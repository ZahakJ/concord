package app

import (
	"bytes"
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
// Trust note: the payload is the responding member's local copies, and the
// responder attests nothing about who produced any of it. What used to follow
// from that — "not re-verified against each original sender" — is no longer true
// of the parts that carry an author's own signature. Messages, stories, pack
// records and archive manifests each prove themselves on arrival against the key
// that signed them, so the member serving the payload is a courier rather than a
// witness (recordsig.go). What is still taken on their word is the guild
// snapshot, the profile roster, categories and events, and the destructive
// reconcile of rows we already hold — which is why that last one is gated on the
// owner and designated sync hosts rather than on any member.
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
	// Chronicles are OWNER-signed history-archive manifests (chronicle.go),
	// carried raw. Raw is load-bearing twice over: the signature covers these
	// exact bytes, and this is the field most likely to grow one — decoding and
	// re-encoding on the way past would silently strip whatever a newer peer
	// added and leave a manifest nobody could verify again.
	Chronicles []json.RawMessage `json:"chronicles,omitempty"`
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
	// A member that stands AHEAD of us just asked for catch-up. That request is
	// the one moment we are told, on the record, that our own ratchet is behind,
	// and nothing used to act on it: the stale side of a split kept serving
	// happily and re-examined itself only on the 60-second anti-entropy beat,
	// against whichever single member answered first — which on a fork is very
	// often somebody on our own dead branch, who answers readably and teaches us
	// nothing. Pull from the peer that just told us. In the ordinary case it is
	// the missed-commit backfill we needed anyway; on a fork it is how the
	// stranded half finally gets a verdict. Only the behind side reciprocates,
	// so this cannot ping-pong.
	if req.Epoch > myEpoch && s.claimReciprocalSync(req.GuildID, from) {
		go s.syncGuildFromPeer(req.GuildID, from)
	}
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

// noteForkedPeer records that p served a payload we could not read while
// standing strictly ahead of our epoch, and flags the guild. See the call site
// for why "strictly ahead" is the whole of the evidence.
func (s *Service) noteForkedPeer(guildID string, p peer.ID) {
	if s.healedRecently(guildID) {
		// A re-add landed moments ago, so this verdict cannot route another one:
		// flag the guild the way an unproven suspicion always has, and let the
		// ordinary sync-everyone recovery run. See forkEvidenceCooldown.
		s.setOutOfSync(guildID, true)
		return
	}
	s.mu.Lock()
	if s.forkedPeers[guildID] == nil {
		s.forkedPeers[guildID] = map[peer.ID]bool{}
	}
	s.forkedPeers[guildID][p] = true
	s.mu.Unlock()
	s.setOutOfSync(guildID, true)
}

// clearForkedPeer withdraws the fork evidence against one peer — its payload
// just decrypted, so whatever divergence we suspected between the two of us is
// over. It reports whether any evidence still stands for the guild.
func (s *Service) clearForkedPeer(guildID string, p peer.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.forkedPeers[guildID]
	if m == nil {
		return false
	}
	delete(m, p)
	if len(m) == 0 {
		delete(s.forkedPeers, guildID)
		return false
	}
	return true
}

// clearForkEvidence drops every fork verdict for a guild. Used after a re-add
// heal, whose Join replaces our group state outright: nothing we believed about
// our old tree survives it.
func (s *Service) clearForkEvidence(guildID string) {
	s.mu.Lock()
	delete(s.forkedPeers, guildID)
	s.mu.Unlock()
}

// forkedWith lists the peers a guild's fork verdict currently rests on.
func (s *Service) forkedWith(guildID string) []peer.ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]peer.ID, 0, len(s.forkedPeers[guildID]))
	for p := range s.forkedPeers[guildID] {
		out = append(out, p)
	}
	return out
}

// pruneForkEvidence forgets fork verdicts against peers that are no longer
// connected members. The verdict's only use is routing a heal at the branch we
// cannot read, and a peer that has gone offline can neither serve one nor be
// re-examined — leaving its verdict standing would keep the "catching up"
// banner lit with nothing able to clear it, and keep this device refusing to
// admit members for a split that may no longer exist.
func (s *Service) pruneForkEvidence(guildID string) {
	live := map[peer.ID]bool{}
	for _, p := range s.memberPeers(guildID) {
		live[p] = true
	}
	s.mu.Lock()
	m := s.forkedPeers[guildID]
	for p := range m {
		if !live[p] {
			delete(m, p)
		}
	}
	if len(m) == 0 {
		delete(s.forkedPeers, guildID)
	}
	s.mu.Unlock()
}

// reciprocalSyncWindow bounds how often one member's request may make us sync
// back to it. A guild in mid-repair asks constantly; without a floor we would
// answer each of those asks with a catch-up of our own.
const reciprocalSyncWindow = healRetryInterval

// claimReciprocalSync reports whether enough time has passed to pull from this
// peer again on the strength of its own request.
func (s *Service) claimReciprocalSync(guildID string, p peer.ID) bool {
	key := guildID + "|" + p.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastReciprocal == nil {
		s.lastReciprocal = map[string]time.Time{}
	}
	if t, ok := s.lastReciprocal[key]; ok && time.Since(t) < reciprocalSyncWindow {
		return false
	}
	s.lastReciprocal[key] = time.Now()
	return true
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

// memberSetTTL bounds how long a cached roster answer may be reused.
//
// It is a backstop, not the mechanism: every path that changes a group's
// membership goes through logCommit or a welcome, and both drop the entry
// outright, so a kick is refused on the very next request. The TTL is what
// makes a membership check that we FORGOT to invalidate self-correct in
// seconds instead of never — the cached answers gate serving guild history, and
// "stale forever" is not a failure mode worth risking to save a hash.
const memberSetTTL = 2 * time.Second

// memberSet is one guild's roster, resolved once and reused.
type memberSet struct {
	fprs map[string]bool
	list []string
	at   time.Time
}

// guildMemberSet resolves a guild's account fingerprints, from cache when it
// can. Reading the roster means asking the MLS engine, which reloads and
// unmarshals the whole group state (~370µs for a fifty-member group on a fast
// desktop) and then hashing every credential — and this was being done ONCE PER
// PEER PER GUILD in the connect and heal paths, so a dozen guilds and twenty
// connected peers cost a quarter of a second of pure re-derivation on every
// beat. Membership only changes when the epoch does, so the answer is worth
// keeping.
func (s *Service) guildMemberSet(guildID string) *memberSet {
	s.memberSetMu.Lock()
	if c, ok := s.memberSets[guildID]; ok && time.Since(c.at) < memberSetTTL {
		s.memberSetMu.Unlock()
		return c
	}
	s.memberSetMu.Unlock()

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
	c := &memberSet{
		fprs: make(map[string]bool, len(creds)),
		list: make([]string, 0, len(creds)),
		at:   time.Now(),
	}
	for _, cred := range creds {
		fpr := accountFingerprintOf(cred)
		if fpr == "" || c.fprs[fpr] {
			continue // an account's second device is the same member
		}
		c.fprs[fpr] = true
		c.list = append(c.list, fpr)
	}
	s.memberSetMu.Lock()
	if s.memberSets == nil {
		s.memberSets = map[string]*memberSet{}
	}
	s.memberSets[guildID] = c
	s.memberSetMu.Unlock()
	return c
}

// forgetMemberSet drops a guild's cached roster. Called wherever the group's
// membership can have moved — every commit we mint or apply, and every welcome
// we accept.
func (s *Service) forgetMemberSet(guildID string) {
	s.memberSetMu.Lock()
	delete(s.memberSets, guildID)
	s.memberSetMu.Unlock()
}

// forgetMemberSetForGroup is forgetMemberSet for the paths that hold a group ID
// rather than a guild ID (commit handling works in group IDs).
func (s *Service) forgetMemberSetForGroup(groupID []byte) {
	s.mu.RLock()
	var id string
	for gid, g := range s.guilds {
		if bytes.Equal(g.GroupID, groupID) {
			id = gid
			break
		}
	}
	s.mu.RUnlock()
	if id != "" {
		s.forgetMemberSet(id)
	}
}

// guildHasMember reports whether the fingerprint belongs to a current member
// of the guild's MLS group.
func (s *Service) guildHasMember(guildID, fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	c := s.guildMemberSet(guildID)
	return c != nil && c.fprs[fingerprint]
}

// GuildHasMember is the exported read of the same question, for the bridge.
func (s *Service) GuildHasMember(guildID, fingerprint string) bool {
	return s.guildHasMember(guildID, fingerprint)
}

// guildMemberFingerprints lists the account fingerprints in a guild's MLS group.
// guildHasMember answers the same question for one person; this is for callers
// that need the whole set and would otherwise walk the roster once per name.
func (s *Service) guildMemberFingerprints(guildID string) []string {
	c := s.guildMemberSet(guildID)
	if c == nil {
		return nil
	}
	return c.list
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
			// The removal we slept through. A member who was offline for the
			// kick meets it here rather than on the control topic, and it has to
			// be read for what it is before ApplyCommit fails on it — otherwise
			// coming back from a weekend away is indistinguishable from a gap
			// nobody can bridge.
			if s.commitEvictsUs(guild.GroupID, c) {
				s.noteEvicted(guildID, evictedKicked)
				return nil
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
			// It proves it about THIS peer, though, and nothing more. Clearing
			// the guild's verdict on any readable answer is what let a fork live
			// forever: both halves of a split are full of members that answer
			// their own side readably, so a verdict set by the far branch was
			// wiped seconds later by somebody on ours, the flag never latched,
			// and no heal was ever attempted. Withdraw the evidence against this
			// peer, and clear the guild only when none stands.
			if !s.clearForkedPeer(guildID, p) {
				s.setOutOfSync(guildID, false)
			}
		} else if cur, err := s.mls.Epoch(s.ctx, guild.GroupID); err == nil && resp.Epoch >= cur {
			// We stand at (or past) the responder's epoch and still cannot read
			// what they encrypt: no amount of commit bridging fixes that. Forked
			// or corrupted local state — flag it, which is what routes us to the
			// re-add heal.
			if resp.Epoch > cur {
				// Strictly ahead of us, handed us the very commits meant to
				// bridge the gap, and we still cannot read a word: the trees
				// diverged. That is remembered against this peer until its
				// payload decrypts (which is what a repair looks like) or a heal
				// re-joins us, rather than until the next member answers.
				//
				// The strictness matters. At EQUAL epochs both halves of a fork
				// would hold evidence against each other, both would fly the
				// banner, and handleInviteRequest's refuse-while-stranded rule
				// would make each decline the re-add that is the only cure —
				// two committers deadlocked. Only a peer we are demonstrably
				// BEHIND can strand us, so only that verdict is kept; the equal
				// case stays the transient flag it has always been, and the next
				// commit on either branch breaks the tie.
				s.noteForkedPeer(guildID, p)
			} else {
				s.setOutOfSync(guildID, true)
			}
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
	// SyncHost members. Forging a brand-new message attributed to another member
	// used to be the residual gap here; it is now the author's own signature that
	// answers it, not this flag (see the message loop at the bottom).
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
	// Calendar events ride the snapshot so a member who joins AFTER an event
	// was created still converges on it — the event_upserted gossip they never
	// received is not replayed. Trust and merge rules live in applySyncedEvent.
	for _, ev := range payload.Events {
		s.applySyncedEvent(guildID, ev)
	}
	s.ingestGovOpsRaw(guildID, payload.GovOps)

	// Pack records, AFTER the governance ops, for the reason the archives below
	// give: the op that granted somebody Manage Guild may be arriving in this very
	// response, and judging their emoji against a state that predates it would
	// refuse a record that is about to be perfectly legitimate.
	// Custom emoji and GIF-pack records are metadata only (a GIF's bytes are
	// fetched on demand), so a member who joins after one was added still learns
	// it here — the gossiped announcement they missed is not replayed.
	//
	// THE OLD TRUST BOUNDARY, and why it is gone. The gossip path checks that the
	// announcing member holds PermManageGuild; this path could not, because
	// catch-up is served by whichever member answered rather than by the admin
	// who created the record, and requiring the responder to be an admin would
	// stop an ordinary member handing over a pack that is legitimately theirs to
	// relay. The consequence was real: a member without Manage Guild could inject
	// a pack record, or replace an existing emoji's image, by serving a doctored
	// snapshot.
	//
	// The record now carries the creating admin's own signature, so the question
	// is asked of the right person. Both lanes ask the identical thing of the
	// identical member — does the AUTHOR hold Manage Guild — and neither has to
	// trust the messenger. Unlike a message, this one FAILS CLOSED on an absent
	// signature too: a pack record is a claim of authority, not of authorship,
	// and an unsigned claim of authority is precisely the injection that was
	// being blocked. Records added before signatures existed keep working on the
	// devices that already hold them; they simply stop spreading on nobody's
	// word, and an admin re-adding one takes a few seconds.
	packPerm := map[string]bool{}
	packRefused := 0
	for _, e := range payload.Emoji {
		if !s.authorizedPackRecord(guildID, e.Author, e.Sig, emojiSigningBytes(guildID, e), packPerm) {
			packRefused++
			continue
		}
		s.applyCustomEmoji(guildID, e)
	}
	for _, g := range payload.Gifs {
		if !s.authorizedPackRecord(guildID, g.Author, g.Sig, gifSigningBytes(guildID, g), packPerm) {
			packRefused++
			continue
		}
		s.applyGuildGif(guildID, g)
	}
	refusedPacks.note(packRefused, "backfilled pack records", srcFpr)

	// History archives, AFTER the governance ops in the same payload. The order
	// is the whole of the interlock: a manifest is only accepted from the guild's
	// EFFECTIVE owner, and the op that moved the crown may be arriving in this
	// very response. Applying the ops first means a member who slept through a
	// transfer learns of it and of the new owner's archive in one round instead
	// of dropping the archive and waiting a beat to be offered it again.
	//
	// Nothing here is taken on the responder's word — see applySyncedChronicles.
	s.applySyncedChronicles(guildID, payload.Chronicles)

	// Stories: the responder attests NOTHING for these — this payload is just
	// whatever the serving member's disk says, and "trusted" above only covers
	// mutating state we already hold. Each record must prove itself: the
	// author's signature is verified against our roster's key for them, and
	// the author's membership is re-checked, in applySyncedStory — the same
	// per-record proof the message loop below now applies to chat rows.
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
	refused := 0
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
			// THIS is the row §13 named. The responder attests nothing about who
			// wrote these words — the payload is their disk, and their disk is
			// whatever they chose to put in it. A signature that does not verify
			// is refused outright; one that is absent is kept and marked, because
			// refusing it would delete every guild's pre-signature history rather
			// than protect anyone (recordsig.go states the reasoning in full).
			if !messageAttestation(&m) {
				refused++
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
	refusedMessages.note(refused, "backfilled messages", srcFpr)
	// Activity that arrived while we were offline must reopen a closed DM, same
	// as a live message would.
	if anyNew {
		s.unhideDM(guildID)
	}
	return true
}
