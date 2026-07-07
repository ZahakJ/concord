package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
	"github.com/zahak/concord/internal/store"
)

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

// syncOverlap is subtracted from the per-channel cursor so sender clock skew
// up to this much can't hide a message; idempotent saves make overlap free.
const syncOverlap = 5 * time.Minute

// maxSyncPayload caps the marshalled payload well below the transport's 1 MiB
// frame limit (inline base64 images make single messages large). Truncation is
// safe: whatever was saved advances the cursor, and the next sync continues.
const maxSyncPayload = 700 * 1024

// syncMessagesPerChannel bounds one channel's contribution to a single response.
const syncMessagesPerChannel = 200

type syncRequest struct {
	GuildID string           `json:"guildId"`
	Epoch   uint64           `json:"epoch"`           // requester's current MLS epoch
	Since   map[string]int64 `json:"since,omitempty"` // channelID -> UnixNano cursor (overlap already applied)
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
	// Payload is an MLS ciphertext (our current epoch) of syncPayload.
	Payload []byte `json:"payload,omitempty"`
}

type syncPayload struct {
	Guild      domain.Guild                `json:"guild"`
	Profiles   map[string]Profile          `json:"profiles,omitempty"`
	Categories []domain.Category           `json:"categories,omitempty"`
	Emoji      []domain.CustomEmoji        `json:"emoji,omitempty"`
	GovOps     []json.RawMessage           `json:"govOps,omitempty"` // signed governance log (roles/bans)
	Messages   map[string][]domain.Message `json:"messages,omitempty"` // channelID -> changed rows
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
		guild = *g
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
	if !s.guildHasMember(req.GuildID, presenceFor(from).Fingerprint) {
		return []byte{}, nil
	}

	var resp syncResponse
	myEpoch, err := s.mls.Epoch(ctx, guild.GroupID)
	if err != nil {
		return []byte{}, nil
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

	payload := syncPayload{
		Guild:    guild,
		Profiles: s.profileRoster(),
		Messages: map[string][]domain.Message{},
	}
	if cats, err := s.store.Categories(guild.ID); err == nil {
		for _, c := range cats {
			payload.Categories = append(payload.Categories, c)
		}
	}
	if emoji, err := s.CustomEmoji(guild.ID); err == nil {
		payload.Emoji = emoji
	}
	payload.GovOps = s.govOpsFor(guild.ID)
	budget := maxSyncPayload
	for _, ch := range guild.Channels {
		msgs, err := s.store.MessagesChangedSince(ch.ID, req.Since[ch.ID], syncMessagesPerChannel)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			cost := len(m.Content) + 256 // rough per-row JSON overhead
			if budget < cost {
				break
			}
			budget -= cost
			payload.Messages[ch.ID] = append(payload.Messages[ch.ID], m)
		}
	}
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
	fpr := presenceFor(p).Fingerprint
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
	var hosts, others []peer.ID
	for _, p := range s.host.Peers() {
		fpr := presenceFor(p).Fingerprint
		if !s.guildHasMember(guildID, fpr) {
			continue
		}
		if s.memberHasPerm(guildID, fpr, PermSyncHost) {
			hosts = append(hosts, p)
		} else {
			others = append(others, p)
		}
	}
	for _, p := range append(hosts, others...) {
		if err := s.syncGuildFromPeer(guildID, p); err == nil {
			return
		}
	}
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
		if identity.FingerprintOf(c) == fingerprint {
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
	for round := 0; round < 2; round++ {
		s.mu.RLock()
		g, ok := s.guilds[guildID]
		var guild domain.Guild
		if ok {
			guild = *g
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

		reqBytes, _ := json.Marshal(syncRequest{GuildID: guildID, Epoch: epoch, Since: since})
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		respBytes, err := s.host.RequestSync(ctx, p, reqBytes)
		cancel()
		if err != nil {
			return err
		}
		if len(respBytes) == 0 {
			return nil // peer isn't in this guild
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
			s.emitGuildUpdate()
		}
		if resp.EpochGap {
			// Nobody reachable can bridge us; surface it rather than dropping
			// messages silently. A later successful sync clears the flag.
			s.setOutOfSync(guildID, true)
			return nil
		}
		s.setOutOfSync(guildID, false)
		if len(resp.Payload) > 0 {
			s.applySyncPayload(guildID, guild.GroupID, resp.Payload)
		}
		if applied == 0 {
			return nil // epoch didn't move; a second round would repeat the first
		}
	}
	return nil
}

// applySyncPayload decrypts a served payload and folds it into local state:
// guild snapshot, profile roster, and message rows/state.
func (s *Service) applySyncPayload(guildID string, groupID, ciphertext []byte) {
	dec, err := s.mls.Decrypt(s.ctx, groupID, ciphertext)
	if err != nil {
		return
	}
	var payload syncPayload
	if json.Unmarshal(dec.Plaintext, &payload) != nil {
		return
	}

	// Channels created while we were away (addChannel is idempotent and
	// subscribes topics); adopt a rename the same way receiveGuildMeta would.
	for _, ch := range payload.Guild.Channels {
		if ch.ID == "" {
			continue
		}
		ch.GuildID = guildID
		s.addChannel(guildID, ch)
	}
	if payload.Guild.Name != "" {
		s.mu.Lock()
		if g, ok := s.guilds[guildID]; ok && g.Name != payload.Guild.Name {
			g.Name = payload.Guild.Name
			gc := *g
			s.mu.Unlock()
			_ = s.store.SaveGuild(gc)
			s.emitGuildUpdate()
		} else {
			s.mu.Unlock()
		}
	}

	for fpr, p := range payload.Profiles {
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
	s.ingestGovOpsRaw(guildID, payload.GovOps)

	self := s.id.Fingerprint()
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
			changed, err := s.store.UpsertSyncedMessage(m, self)
			if err != nil || !changed {
				continue
			}
			if full, ok, err := s.store.MessageByID(m.ID); err == nil && ok {
				s.emitMessage(full)
			}
		}
	}
}
