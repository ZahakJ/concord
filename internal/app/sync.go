package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// errSyncDeclined marks a peer that answered our catch-up with an empty body:
// it does not believe we belong to the guild. Distinct from a transport error
// because it means "ask someone else", not "retry this one".
var errSyncDeclined = errors.New("app: peer declined to sync")

// History sync (v2): when a peer (re)connects, we ask it — per guild — for
// everything we missed while offline:
//
//  1. MLS COMMITS. Membership changes must be applied gaplessly; a member that
//     misses one is stranded at an old epoch and can decrypt nothing newer. So
//     the request carries our current epoch and the responder replays, from its
//     commit log, the exact commits that bridge us to its epoch. This is the
//     part that makes offline catch-up possible at all.
//  2. A STATE PAYLOAD, MLS-encrypted at the responder's current epoch: the
//     guild snapshot (name, channels created while we were away), the member
//     profile roster, and per-channel message rows — including edits, deletes,
//     pins, and reactions of messages we already hold (state snapshots, not
//     action replay).
//
// Trust note: everything in the payload is *attested by the responding member*
// (their local copies), not re-verified against each original sender — the
// same trust Discord places in its server, but limited to guild members.
// Message saves are idempotent by ID and state adoption is newest-wins, so
// overlapping syncs against several members are harmless.

// syncOverlap is subtracted from the per-channel cursor. Its ONLY job is sender
// clock skew: the cursor is our own newest stamp for the channel, so a message
// we never received from a peer whose clock runs behind ours would otherwise
// fall below it and stay invisible forever. Everything else is already covered —
// a gap in one sender's own stream is above their previous stamp, and an edit
// carries a fresh `updated` that MessagesChangedSince matches on.
//
// It was five minutes, which meant every reconcile re-shipped the last five
// minutes of every active channel — and a chat message can carry an inline
// base64 image, so on a busy channel that was megabytes an hour to re-send rows
// the peer already had. A minute absorbs any clock a phone or laptop keeps
// (NTP-disciplined devices sit inside a second, and a device further out than a
// minute is not saved by five either — that just moves the cliff), while
// costing a fifth of the traffic.
const syncOverlap = time.Minute

// maxSyncPayload caps the marshalled payload well below the transport's 1 MiB
// frame limit (inline base64 images make single messages large). Truncation is
// safe: whatever was saved advances the cursor and the requester's digests, so
// the next round continues where this one stopped — and syncResponse.More asks
// for that round now rather than on the next beat.
const maxSyncPayload = 700 * 1024

// syncMessagesPerChannel bounds one channel's contribution to a single response.
const syncMessagesPerChannel = 200

// maxSyncRounds bounds one peer's catch-up. Two rounds were always needed when
// applied commits moved our epoch (history encrypted beyond our old reach
// becomes readable); the third covers a payload the budget truncated.
const maxSyncRounds = 3

type syncRequest struct {
	GuildID string           `json:"guildId"`
	Epoch   uint64           `json:"epoch"`           // requester's current MLS epoch
	Since   map[string]int64 `json:"since,omitempty"` // channelID -> UnixNano cursor (overlap already applied)
	// Have is what the requester already holds, as content hashes (syncdigest.go).
	// Absent from older peers, which reads as nil: serve the full snapshot, as
	// this protocol always did.
	Have *syncDigest `json:"have,omitempty"`
}

type syncResponse struct {
	// Commits bridge the requester from its epoch to ours, in order. They are
	// carried in the clear — the same bytes travel the public control topic —
	// and MLS itself authenticates and orders them on apply.
	Commits [][]byte `json:"commits,omitempty"`
	// EpochGap is set when our commit log cannot bridge the requester's epoch
	// (e.g. we joined later than they diverged). No payload accompanies it —
	// they couldn't decrypt one.
	EpochGap bool `json:"epochGap,omitempty"`
	// Epoch is the responder's current MLS epoch. It lets the requester tell a
	// payload it cannot read because the RESPONDER is far behind (their
	// problem) from one it cannot read at an epoch it supposedly shares (a
	// fork — the requester's problem, curable only by a re-add). Absent from
	// older peers, which reads as 0: "responder behind", the harmless verdict.
	Epoch uint64 `json:"myEpoch,omitempty"`
	// Payload is an MLS ciphertext (our current epoch) of syncPayload.
	Payload []byte `json:"payload,omitempty"`
	// More says the byte budget cut the payload short and there is more to
	// serve. The requester asks again straight away instead of waiting out the
	// reconcile beat; its own cursor and digests, which advanced over whatever
	// it just ingested, are what make the next answer a continuation rather
	// than a repeat. Absent from older peers, which reads as false — exactly
	// the old behaviour, one round and wait.
	More bool `json:"more,omitempty"`
}

type syncPayload struct {
	Guild      domain.Guild                `json:"guild"`
	Profiles   map[string]Profile          `json:"profiles,omitempty"`
	Categories []domain.Category           `json:"categories,omitempty"`
	Emoji      []domain.CustomEmoji        `json:"emoji,omitempty"`
	Gifs       []GuildGif                  `json:"gifs,omitempty"`     // GIF-pack records (references, not bytes)
	Events     []domain.Event              `json:"events,omitempty"`   // calendar events, RSVPs included
	GovOps     []json.RawMessage           `json:"govOps,omitempty"`   // signed governance log (roles/bans)
	Messages   map[string][]domain.Message `json:"messages,omitempty"` // channelID -> changed rows
	// Stories are AUTHOR-SIGNED records (story.go), so unlike the rest of this
	// payload they are NOT taken on the responder's word: the applier verifies
	// each signature and the author's membership before storing.
	Stories []storyRecord `json:"stories,omitempty"`
	// StoryDels are author-signed retractions, verified exactly like Stories —
	// a peer who slept through a delete hears it from whoever answers.
	StoryDels []storyDelete `json:"storyDels,omitempty"`
}

// handleSyncRequest serves a peer's catch-up request from local state.
func (s *Service) handleSyncRequest(ctx context.Context, from peer.ID, request []byte) ([]byte, error) {
	var req syncRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}
	s.mu.RLock()
	g, ok := s.guilds[req.GuildID]
	var guild domain.Guild
	if ok {
		guild = g.Clone()
	}
	s.mu.RUnlock()
	if !ok {
		return []byte{}, nil // not in that guild; nothing to serve
	}
	// Only serve guild history to a current member. The payload is MLS-encrypted
	// (members-only anyway), but the commit list is plaintext and MLS Add commits
	// embed joiners' key packages (account pubkeys) — serving them to a non-member
	// who merely knows the guild ID (e.g. a removed/banned member) would leak the
	// membership roster. Membership is checked against the authenticated PeerID.
	if !s.guildHasMember(req.GuildID, s.presence(from).Fingerprint) {
		return []byte{}, nil
	}

	var resp syncResponse
	myEpoch, err := s.mls.Epoch(ctx, guild.GroupID)
	if err != nil {
		return []byte{}, nil
	}
	resp.Epoch = myEpoch
	if req.Epoch < myEpoch {
		rows, err := s.store.CommitsAfter(guild.GroupID, req.Epoch)
		if err != nil || !bridges(rows, req.Epoch, myEpoch) {
			resp.EpochGap = true
			return json.Marshal(resp)
		}
		for _, r := range rows {
			resp.Commits = append(resp.Commits, r.Commit)
		}
	}
	// A requester *ahead* of us gets no commits but still gets the payload: MLS
	// tolerates decrypting a few epochs back, and the mirror-image sync running
	// in the other direction lifts us to their epoch.

	// Gather everything this guild could contribute, then let the requester's
	// digests and the byte budget decide what actually travels (syncdigest.go).
	// Gathering is cheap next to serving: the expensive part of the old
	// behaviour was not reading the icon, it was encrypting and shipping it to
	// somebody who already had it.
	src := s.syncSourceFor(guild.ID, guild, time.Now().Unix())
	src.reqFpr = s.presence(from).Fingerprint
	// Whether WE are a trusted history source for this guild, judged by our own
	// governance state. The requester judges the same question on its own state
	// when it applies what we send; if the two ever disagree the cost is a
	// profile refresh withheld (it still reaches them from the owner, a
	// SyncHost, or its author), never a wrong write.
	src.selfTrusted = s.trustedSyncSource(guild.ID, src.selfFpr)
	for _, ch := range guild.Channels {
		msgs, err := s.store.MessagesChangedSince(ch.ID, req.Since[ch.ID], syncMessagesPerChannel)
		if err != nil || len(msgs) == 0 {
			continue
		}
		src.channels = append(src.channels, syncChannelRows{id: ch.ID, rows: msgs})
	}
	payload, truncated := buildSyncPayload(src, req.Have)
	resp.More = truncated
	raw, err := json.Marshal(payload)
	if err != nil {
		return json.Marshal(resp)
	}
	// Encrypt to the group: only members can read the served history.
	if ct, err := s.mls.Encrypt(ctx, guild.GroupID, raw); err == nil {
		resp.Payload = ct
	}
	return json.Marshal(resp)
}

// bridges reports whether rows form a gapless commit chain from afterEpoch+1
// through wantEpoch.
func bridges(rows []store.CommitRow, afterEpoch, wantEpoch uint64) bool {
	next := afterEpoch + 1
	for _, r := range rows {
		if r.Epoch != next {
			return false
		}
		next++
	}
	return next > wantEpoch
}

// syncFromPeer pulls missed state for every shared guild from one peer.
// Best-effort; reports whether every attempted guild sync at least reached the
// peer (so the caller can retry once on transport-level failure).
func (s *Service) syncFromPeer(p peer.ID) bool {
	fpr := s.presence(p).Fingerprint
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	reachedAll := true
	for _, id := range ids {
		if !s.guildHasMember(id, fpr) {
			continue // not a member (e.g. a rendezvous node): nothing to ask
		}
		if err := s.syncGuildFromPeer(id, p); err != nil {
			reachedAll = false
		}
	}
	return reachedAll
}

// syncGuildFromAnyPeer tries each connected member of a guild until one sync
// completes. Used when a live commit fails to apply — we detected our own
// epoch gap and need backfill right now. Members holding the SyncHost permission
// (designated always-on hosts) are tried first.
func (s *Service) syncGuildFromAnyPeer(guildID string) {
	peers := s.memberPeers(guildID)
	declined := 0
	for _, p := range peers {
		err := s.syncGuildFromPeer(guildID, p)
		if err == nil {
			return
		}
		if errors.Is(err, errSyncDeclined) {
			declined++
		}
	}
	// Everyone we could reach says we are not a member. That is either true —
	// we were removed — or their rosters are stale in a way we cannot fix by
	// asking again, and either way it is worth one line rather than a silent
	// retry loop that outlives the problem.
	if declined > 0 && declined == len(peers) {
		log.Printf("concord/app: guild %s: every peer declined to sync with us (%d asked)", guildID, declined)
	}
}

// trustedSyncSource reports whether a backfill served by this member may perform
// DESTRUCTIVE reconciliation (tombstone/rewrite existing messages, refresh cached
// profiles). The guild owner is always trusted; so is any member holding the
// SyncHost permission — the designated always-on history hosts. An ordinary
// member can still serve gap-fill inserts, just not mutate what we already hold.
func (s *Service) trustedSyncSource(guildID, fpr string) bool {
	if fpr == "" {
		return false
	}
	// The EFFECTIVE owner (not the founding key): destructive-reconciliation
	// trust must follow a transferred crown like every other authority.
	if s.effectiveOwner(guildID) == fpr {
		return true
	}
	return s.memberHasPerm(guildID, fpr, PermSyncHost)
}

// hasProfile reports whether we already hold a cached profile for a fingerprint.
func (s *Service) hasProfile(fpr string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.profiles[fpr]
	return ok
}

// guildHasMember reports whether the fingerprint belongs to a current member
// of the guild's MLS group.
func (s *Service) guildHasMember(guildID, fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return false
	}
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil {
		return false
	}
	for _, c := range creds {
		if accountFingerprintOf(c) == fingerprint {
			return true
		}
	}
	return false
}

// guildMemberFingerprints lists the account fingerprints in a guild's MLS group.
// guildHasMember answers the same question for one person; this is for callers
// that need the whole set and would otherwise walk the roster once per name.
func (s *Service) guildMemberFingerprints(guildID string) []string {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(creds))
	for _, c := range creds {
		if fpr := accountFingerprintOf(c); fpr != "" {
			out = append(out, fpr)
		}
	}
	return out
}

// sharesGuild reports whether the fingerprint is a member of any guild we are
// in — "is this peer a friend", for decisions that aren't about one guild.
func (s *Service) sharesGuild(fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		if s.guildHasMember(id, fingerprint) {
			return true
		}
	}
	return false
}

// syncGuildFromPeer runs one guild's catch-up against one peer: request, apply
// commits, apply payload; when commits moved our epoch, one more round picks up
// history that was encrypted beyond our old reach. The returned error reflects
// transport failure only (no response) — content problems are best-effort.
func (s *Service) syncGuildFromPeer(guildID string, p peer.ID) error {
	for round := 0; round < maxSyncRounds; round++ {
		s.mu.RLock()
		g, ok := s.guilds[guildID]
		var guild domain.Guild
		if ok {
			guild = g.Clone()
		}
		s.mu.RUnlock()
		if !ok {
			return nil
		}

		epoch, err := s.mls.Epoch(s.ctx, guild.GroupID)
		if err != nil {
			return nil
		}
		since := map[string]int64{}
		for _, ch := range guild.Channels {
			latest, err := s.store.LatestTimestamp(ch.ID)
			if err != nil {
				continue
			}
			if cursor := latest - syncOverlap.Nanoseconds(); cursor > 0 {
				since[ch.ID] = cursor
			}
		}

		// Say what we already hold, so the responder can answer with the
		// difference instead of rebuilding the whole guild (syncdigest.go).
		// Recomputed each round: the last round's answer is exactly what must
		// not come back again.
		reqBytes, _ := json.Marshal(syncRequest{
			GuildID: guildID, Epoch: epoch, Since: since,
			Have: s.syncDigestFor(guildID, guild, time.Now().Unix()),
		})
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		respBytes, err := s.host.RequestSync(ctx, p, reqBytes)
		cancel()
		if err != nil {
			return err
		}
		if len(respBytes) == 0 {
			// An empty body means the peer declined — it does not think we are a
			// member (see handleSyncRequest). Reporting that as success made a
			// single peer with a stale view of the roster stand in for every
			// other member: syncGuildFromAnyPeer stops at the first nil, so we
			// asked one node, were refused, called it done, and repeated that
			// every twenty seconds with nothing in the logs. Say it failed, so
			// the caller moves on to somebody who might answer.
			return errSyncDeclined
		}
		var resp syncResponse
		if json.Unmarshal(respBytes, &resp) != nil {
			return nil
		}

		applied := 0
		for _, c := range resp.Commits {
			// Same governance gate as the live control topic: a peer serving us
			// backfill cannot slip in an unauthorized membership change. The
			// author is resolved against our current (pre-apply) member list, so
			// it must be checked before each ApplyCommit as the epoch advances.
			if !s.commitAuthorized(guildID, guild.GroupID, c) {
				break
			}
			if err := s.mls.ApplyCommit(s.ctx, guild.GroupID, c); err != nil {
				break
			}
			s.logCommit(guild.GroupID, c)
			applied++
		}
		if applied > 0 {
			// Catch-up commits change the roster exactly like live ones do, so
			// re-read which leaves are linked devices — otherwise a device that
			// joined while we were away stays a stranger until the next restart.
			// See the same call on the control topic.
			s.relearnDevices(guild.GroupID)
			s.emitGuildUpdate()
			// The epoch moved: messages that arrived too early for it may be
			// readable now. This is the delivery path for a message that raced
			// its own commit and lost.
			s.retryPendingCiphertexts(guild.GroupID)
		}
		if resp.EpochGap {
			// Nobody reachable can bridge us; surface it rather than dropping
			// messages silently. A later successful sync clears the flag.
			s.setOutOfSync(guildID, true)
			return nil
		}
		payloadOK := true
		if len(resp.Payload) > 0 {
			payloadOK = s.applySyncPayload(guildID, guild.GroupID, resp.Payload, s.presence(p).Fingerprint)
		}
		// Only a payload we could actually READ proves the ratchet is whole
		// again. Clearing the flag on the epoch numbers alone hid a forked
		// group: both sides at epoch N, different trees, nothing decryptable,
		// banner gone.
		if payloadOK {
			s.setOutOfSync(guildID, false)
		} else if cur, err := s.mls.Epoch(s.ctx, guild.GroupID); err == nil && resp.Epoch >= cur {
			// We stand at (or past) the responder's epoch and still cannot read
			// what they encrypt: no amount of commit bridging fixes that. Forked
			// or corrupted local state — flag it, which is what routes us to the
			// re-add heal.
			s.setOutOfSync(guildID, true)
		}
		if applied == 0 && !resp.More {
			// The epoch didn't move and the responder served everything it had
			// for us: another round would repeat this one.
			return nil
		}
	}
	return nil
}

// applySyncPayload decrypts a served payload and folds it into local state:
// guild snapshot, profile roster, and message rows/state. It reports whether
// the payload could be read at all — false means our group state cannot
// decrypt what a current member encrypted at their epoch, the signature of a
// gap or fork that still needs repair.
func (s *Service) applySyncPayload(guildID string, groupID, ciphertext []byte, srcFpr string) bool {
	dec, err := s.mls.Decrypt(s.ctx, groupID, ciphertext)
	if err != nil {
		return false
	}
	var payload syncPayload
	if json.Unmarshal(dec.Plaintext, &payload) != nil {
		return true // readable but malformed: the ratchet itself is fine
	}
	// A backfill is only as trustworthy as its server for anything that MUTATES
	// state we already hold (tombstoning/rewriting messages, overwriting cached
	// profiles). Gap-fill inserts stay open to any member so ordinary catch-up
	// works; destructive reconcile is limited to the guild owner and designated
	// SyncHost members. (Forging a brand-new message attributed to another member
	// is the residual gap here — closing it fully needs per-message author
	// signatures; tracked as a follow-up.)
	trusted := s.trustedSyncSource(guildID, srcFpr)

	// Channels created while we were away (addChannel is idempotent and
	// subscribes topics); adopt a rename the same way receiveGuildMeta would.
	for _, ch := range payload.Guild.Channels {
		if ch.ID == "" {
			continue
		}
		ch.GuildID = guildID
		s.addChannel(guildID, ch)
	}
	// Adopt the guild profile (name/icon/banner/description) the peer served —
	// the catch-up path for a member that was offline when the owner changed the
	// logo, so the gossip'd guild_profile update never reached it. Only non-empty
	// values are taken (like the rename above), so a peer that simply hasn't
	// learned the new image yet can't clobber ours back to blank. Images are
	// validated defensively, same as receiveGuildMeta.
	validImg := func(v string) bool {
		return strings.HasPrefix(v, "data:image/") && len(v) <= maxGuildImageBytes
	}
	s.mu.Lock()
	if g, ok := s.guilds[guildID]; ok {
		changed := false
		if payload.Guild.Name != "" && g.Name != payload.Guild.Name {
			g.Name = payload.Guild.Name
			changed = true
		}
		if payload.Guild.Icon != "" && g.Icon != payload.Guild.Icon && validImg(payload.Guild.Icon) {
			g.Icon = payload.Guild.Icon
			changed = true
		}
		if payload.Guild.Banner != "" && g.Banner != payload.Guild.Banner && validImg(payload.Guild.Banner) {
			g.Banner = payload.Guild.Banner
			changed = true
		}
		if payload.Guild.Description != "" && g.Description != payload.Guild.Description {
			g.Description = payload.Guild.Description
			changed = true
		}
		if changed {
			gc := g.Clone()
			s.mu.Unlock()
			_ = s.store.SaveGuild(gc)
			s.emitGuildUpdate()
		} else {
			s.mu.Unlock()
		}
	} else {
		s.mu.Unlock()
	}

	selfFpr := s.id.Fingerprint()
	for fpr, p := range payload.Profiles {
		if fpr == selfFpr {
			// Our own account's profile, served back to us. Only another device
			// of THIS account may move it (srcFpr is certificate-authenticated);
			// any member can put our fingerprint in a roster, and adopting that
			// would let a neighbor rewrite our identity on our own screen. This
			// is the offline catch-up lane for linked devices — the device hello
			// covers the same ground, sync covers it again for good measure.
			if srcFpr == selfFpr {
				s.AdoptLinkedProfile(p)
			}
			continue
		}
		// An untrusted backfill may fill in profiles we don't know yet, but must not
		// overwrite a member's cached identity — in particular their MailboxPub,
		// which routes offline mail. Trusted sources (owner/SyncHost) may refresh —
		// and so may the member itself for its OWN row (fpr == srcFpr): the same
		// author-binding the gossip announce enforces, so a returning peer can
		// hand us the fresh status it set while we were apart.
		if !trusted && fpr != srcFpr && s.hasProfile(fpr) {
			continue
		}
		s.learnProfile(fpr, p)
	}
	for _, c := range payload.Categories {
		if c.ID != "" {
			c.GuildID = guildID
			_ = s.store.SaveCategory(c)
		}
	}
	for _, e := range payload.Emoji {
		s.applyCustomEmoji(guildID, e)
	}
	// GIF-pack records are metadata only (the blobs are fetched on demand), so a
	// member who joins after a GIF was added still learns the pack here — the
	// gossiped announcement they missed is not replayed.
	//
	// Note the trust boundary, which is the same one custom emoji sit behind
	// just above: the GOSSIP path checks that the announcing member holds
	// PermManageGuild, but this one cannot. Catch-up is served by whichever
	// member answered, not by the admin who created the record, so requiring the
	// server to be an admin would stop an ordinary member handing over a pack
	// that is legitimately theirs to relay. The consequence is real and worth
	// naming: a member without Manage Guild can inject a pack record by serving
	// a doctored snapshot. Closing it needs the record to carry the creating
	// admin's signature, the way governance ops already do (ingestGovOpsRaw
	// below) — a bigger change than moving this line, and one that would want to
	// cover emoji at the same time.
	for _, g := range payload.Gifs {
		s.applyGuildGif(guildID, g)
	}
	// Calendar events ride the snapshot so a member who joins AFTER an event
	// was created still converges on it — the event_upserted gossip they never
	// received is not replayed. Trust and merge rules live in applySyncedEvent.
	for _, ev := range payload.Events {
		s.applySyncedEvent(guildID, ev)
	}
	s.ingestGovOpsRaw(guildID, payload.GovOps)

	// Stories: the responder attests NOTHING for these — this payload is just
	// whatever the serving member's disk says, and "trusted" above only covers
	// mutating state we already hold. Each record must prove itself: the
	// author's signature is verified against our roster's key for them, and
	// the author's membership is re-checked, in applySyncedStory. This is the
	// per-record closing of the forgery gap the message comment above still
	// names as open for message rows.
	storyChanged := false
	for _, rec := range payload.Stories {
		if s.applySyncedStory(guildID, rec) {
			storyChanged = true
		}
	}
	// Retractions AFTER inserts, so a payload carrying both a story and its
	// own delete nets out to deleted, whatever order the responder built it in.
	for _, d := range payload.StoryDels {
		if s.applyStoryDel(guildID, "", d, 0) {
			storyChanged = true
		}
	}
	if storyChanged {
		s.emitStory(guildID)
	}

	self := s.id.Fingerprint()
	anyNew := false
	for chID, msgs := range payload.Messages {
		s.mu.RLock()
		_, tracked := s.channelToGuild[chID]
		s.mu.RUnlock()
		if !tracked {
			continue
		}
		for _, m := range msgs {
			// Never accept action kinds through sync (state already snapshotted)
			// or rows claiming a different channel than the one they came under.
			if m.ChannelID != chID || (m.Kind != "" && m.Kind != "system") {
				continue
			}
			changed, err := s.store.UpsertSyncedMessage(m, self, trusted)
			if err != nil || !changed {
				continue
			}
			anyNew = true
			if full, ok, err := s.store.MessageByID(m.ID); err == nil && ok {
				s.emitMessage(full)
			}
		}
	}
	// Activity that arrived while we were offline must reopen a closed DM, same
	// as a live message would.
	if anyNew {
		s.unhideDM(guildID)
	}
	return true
}
