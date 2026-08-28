package app

import "strings"

// evicted.go is what a guild looks like from the outside after moderation has
// happened to you.
//
// Before this, being kicked or banned was completely silent on the receiving
// end. The guild stayed in the rail, every channel was listed, the member list
// still showed the people who had just thrown you out, and the composer took
// messages and sent them into a group that would not decrypt them. No error, no
// toast, no greyed-out anything. The worst possible ending for the person on the
// receiving end of a moderation decision is to keep talking to a room that left.
//
// The hard part is not the banner. It is being SURE, because the failure this
// looks like from the inside — "the guild's traffic stopped making sense" — is
// also exactly what a stranded member sees, and a stranded member must heal, not
// be told they were thrown out. So there are exactly two ways in, and both are
// evidence rather than inference:
//
//  1. A commit we could read, from an authorized committer, that blanked our own
//     leaf. We cannot APPLY it (see mls.Engine.RemovedBy) but we can read whom
//     it evicts, and MLS signed it.
//  2. An authorized committer answering our admission request with "you were
//     removed" / "you are banned" — a peer that holds manage-members telling us,
//     in response to a question we asked, what the guild's governance log says.
//
// Everything else — an undecryptable frame, an epoch gap, a payload we could not
// read — stays what it always was: stranded, recoverable, and shown as catching
// up.

const evictedSettingPrefix = "evicted:"

// Eviction reasons. They travel to the UI verbatim, so the copy branches on the
// difference between "you can be let back in" and "you cannot".
const (
	evictedKicked = "removed"
	evictedBanned = "banned"
)

// EvictedFrom reports why this device is no longer a member of a guild
// ("removed", "banned"), or "" if it still is one.
func (s *Service) EvictedFrom(guildID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.evicted[guildID]
}

// noteEvicted records that we are out of a guild and why, and tears down the
// state that only makes sense for a member: the stranded flag, the fork
// evidence, and the stash of ciphertext we were hoping to grow into. A ban
// outranks a removal if both ever arrive.
func (s *Service) noteEvicted(guildID, reason string) {
	if guildID == "" || reason == "" {
		return
	}
	s.mu.Lock()
	prev := s.evicted[guildID]
	if prev == reason || (prev == evictedBanned && reason == evictedKicked) {
		s.mu.Unlock()
		return
	}
	s.evicted[guildID] = reason
	delete(s.outOfSync, guildID)
	var groupID []byte
	if g, ok := s.guilds[guildID]; ok {
		groupID = g.GroupID
	}
	s.mu.Unlock()

	s.clearForkEvidence(guildID)
	if len(groupID) > 0 {
		// Nothing in the stash can ever open now, and leaving it there would let
		// healStrandedGuilds pick the guild back up on its next pass.
		s.pendingCTMu.Lock()
		delete(s.pendingCT, string(groupID))
		s.pendingCTMu.Unlock()
	}
	_ = s.store.SetSetting(evictedSettingPrefix+guildID, reason)
	s.emitGuildUpdate()
}

// clearEvicted takes a guild back out of the terminal state. Called on the one
// event that disproves it: a welcome we successfully joined.
func (s *Service) clearEvicted(guildID string) {
	s.mu.Lock()
	if s.evicted[guildID] == "" {
		s.mu.Unlock()
		return
	}
	delete(s.evicted, guildID)
	s.mu.Unlock()
	_ = s.store.SetSetting(evictedSettingPrefix+guildID, "")
	s.emitGuildUpdate()
}

// loadEvicted restores the terminal state at startup. Without it a restart looks
// like a fresh stranding: the local group state still lists us as a member at
// the epoch we were thrown out of, so the client would flag itself out of sync
// and start asking to be re-added — which is answered correctly, but only when
// a committer happens to be online, and says nothing in the meantime.
func (s *Service) loadEvicted() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		reason, err := s.store.GetSetting(evictedSettingPrefix + id)
		if err != nil || (reason != evictedKicked && reason != evictedBanned) {
			continue
		}
		s.mu.Lock()
		s.evicted[id] = reason
		s.mu.Unlock()
	}
}

// noteEvictionRefusal turns an admission refusal into the terminal state, but
// only for the two answers that mean it. Every other refusal — "inviter is
// catching up", "not authorized to admit", a timeout — is a transient about the
// peer we asked, not a verdict about us, and the heal loop is right to try the
// next committer.
func (s *Service) noteEvictionRefusal(guildID, refusal string) {
	switch strings.TrimSpace(refusal) {
	case bannedRefusal:
		s.noteEvicted(guildID, evictedBanned)
	case removedRefusal:
		s.noteEvicted(guildID, evictedKicked)
	}
}

// commitEvictsUs reports whether a commit we are about to fail to apply is the
// commit that threw us out — read from its own Remove proposals against the
// roster we still hold. Every one of our leaves counts: a linked device is a
// leaf of its own, and a kick removes the account, not one laptop.
func (s *Service) commitEvictsUs(groupID, commit []byte) bool {
	removed, err := s.mls.RemovedBy(s.ctx, groupID, commit)
	if err != nil || len(removed) == 0 {
		return false
	}
	mine := s.id.Fingerprint()
	for _, cred := range removed {
		if accountFingerprintOf(cred) == mine {
			return true
		}
	}
	return false
}

// evictedGuildIDs snapshots the guilds we are out of, so the heal loop can skip
// them without holding the lock across a network call.
func (s *Service) evictedGuildIDs() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.evicted) == 0 {
		return nil
	}
	out := make(map[string]bool, len(s.evicted))
	for id := range s.evicted {
		out[id] = true
	}
	return out
}
