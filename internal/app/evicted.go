package app

import (
	"bytes"
	"strings"
)

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
// be told they were thrown out. So there are exactly three ways in, and all are
// evidence rather than inference:
//
//  1. A commit we could read, from an authorized committer, that blanked our own
//     leaf. We cannot APPLY it (see mls.Engine.RemovedBy) but we can read whom
//     it evicts, and MLS signed it.
//  2. An authorized committer answering our admission request with "you were
//     removed" / "you are banned" — a peer that holds manage-members telling us,
//     in response to a question we asked, what the guild's governance log says.
//  3. A signed remove_member or ban op naming our account, folded into our own
//     copy of that log (noteGovernanceRemoval).
//
// Everything else — an undecryptable frame, an epoch gap, a payload we could not
// read — stays what it always was: stranded, recoverable, and shown as catching
// up.

const evictedSettingPrefix = "evicted:"
const evictedReasonPrefix = "evicted-reason:"

// Eviction reasons. They travel to the UI verbatim, so the copy branches on the
// difference between "you can be let back in" and "you cannot".
const (
	evictedKicked = "removed"
	evictedBanned = "banned"
	// evictedUnseated: the welcome this device joined with came from someone
	// who could not admit it, and no real committer has ever seated it. Nobody
	// threw it out; it was never in. The cure is an invite from a member who
	// can admit, which is why the UI says that rather than "you were removed".
	evictedUnseated = "unseated"
)

// EvictedFrom reports why this device is no longer a member of a guild
// ("removed", "banned"), or "" if it still is one.
func (s *Service) EvictedFrom(guildID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.evicted[guildID]
}

// EvictedReason is the optional note from the signed removal that put us
// out. Empty when the moderator wrote nothing, or when we have the commit
// but not yet the op.
func (s *Service) EvictedReason(guildID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.evictedReason[guildID]
}

// reasonWeWereRemovedLocked walks the gov log for the latest remove_member
// or ban targeting us and returns its note. Caller holds mu (read or write).
func (s *Service) reasonWeWereRemovedLocked(guildID string) string {
	if s.id == nil {
		return ""
	}
	me := s.id.Fingerprint()
	var note string
	var seq uint64
	for _, o := range s.govOps[guildID] {
		if (o.Type != "remove_member" && o.Type != "ban") || o.Target != me || o.Name == "" {
			continue
		}
		if o.Seq >= seq {
			seq = o.Seq
			note = o.Name
		}
	}
	return note
}

func (s *Service) rememberEvictedReason(guildID, note string) {
	if guildID == "" || note == "" {
		return
	}
	s.mu.Lock()
	if s.evictedReason == nil {
		s.evictedReason = map[string]string{}
	}
	if s.evictedReason[guildID] == note {
		s.mu.Unlock()
		return
	}
	s.evictedReason[guildID] = note
	s.mu.Unlock()
	_ = s.store.SetSetting(evictedReasonPrefix+guildID, note)
	s.emitGuildUpdate()
}

// noteEvicted records that we are out of a guild and why, and tears down the
// state that only makes sense for a member: the stranded flag, the fork
// evidence, and the stash of ciphertext we were hoping to grow into. A ban
// outranks a removal if both ever arrive.
func (s *Service) noteEvicted(guildID, reason string) {
	if guildID == "" || reason == "" {
		return
	}
	note := ""
	s.mu.Lock()
	// The commit that blanked our leaf cannot say whether a ban came with it,
	// but the log can: a ban is issued before its removal, while we can still
	// read the guild. Without this a banned member was first told an admin
	// could let them back in.
	if reason == evictedKicked && s.id != nil && s.govState[guildID].Banned[s.id.Fingerprint()] {
		reason = evictedBanned
	}
	prev := s.evicted[guildID]
	if prev == reason || (prev == evictedBanned && reason == evictedKicked) {
		s.mu.Unlock()
		return
	}
	s.evicted[guildID] = reason
	note = s.reasonWeWereRemovedLocked(guildID)
	if note != "" {
		if s.evictedReason == nil {
			s.evictedReason = map[string]string{}
		}
		s.evictedReason[guildID] = note
	}
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
	if note != "" {
		_ = s.store.SetSetting(evictedReasonPrefix+guildID, note)
	}
	s.emitGuildUpdate()
}

// clearEvicted takes a guild back out of the terminal state. Called on the
// events that disprove it: a welcome we successfully joined, and a commit we
// applied as a member (clearDisprovedEviction).
func (s *Service) clearEvicted(guildID string) {
	s.mu.Lock()
	if s.evicted[guildID] == "" {
		s.mu.Unlock()
		return
	}
	delete(s.evicted, guildID)
	delete(s.evictedReason, guildID)
	s.mu.Unlock()
	_ = s.store.SetSetting(evictedSettingPrefix+guildID, "")
	_ = s.store.SetSetting(evictedReasonPrefix+guildID, "")
	s.emitGuildUpdate()
}

// clearDisprovedEviction lifts a "removed" verdict that a commit we just
// applied disproves. A device whose leaf was blanked cannot apply the commit
// that blanked it, nor any after it, so applying one from an authorized
// committer and still finding our own leaf in the roster is proof we are in.
//
// This is how a device recovers from an earlier build that read a commit
// removing a sibling device as its own removal: it recorded "removed",
// refused that commit, and kept the verdict through every restart. Catch-up
// now applies the commit it refused, and this takes the verdict away.
//
// A verdict governance stands behind stays. When the guild's log says this
// account was removed or banned, the account is out even if one of its leaves
// survived a kick that failed part-way, and that leaf applying commits proves
// nothing about the decision. Bans and unseated verdicts are never lifted here.
func (s *Service) clearDisprovedEviction(guildID string, groupID []byte) {
	if s.EvictedFrom(guildID) != evictedKicked || s.id == nil {
		return
	}
	me := s.id.Fingerprint()
	s.mu.RLock()
	st := s.govState[guildID]
	decided := st.Removed[me] || st.Banned[me]
	s.mu.RUnlock()
	if decided {
		return
	}
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil {
		return
	}
	for _, c := range creds {
		if bytes.Equal(c, s.myCredential) {
			s.clearEvicted(guildID)
			return
		}
	}
}

// noteGovernanceRemoval records the eviction a governance op just decided for
// this account. A kick removes the account's leaves one commit each, and each
// device used to learn it was out only from the commit for its own leaf. When
// that commit never comes, because the kick failed part-way, the device would
// stay a working member of a guild that removed its account. The log is the
// decision, so it is enough.
func (s *Service) noteGovernanceRemoval(guildID string) {
	if s.id == nil {
		return
	}
	me := s.id.Fingerprint()
	s.mu.RLock()
	st := s.govState[guildID]
	removed := st.Removed[me] || st.Banned[me]
	s.mu.RUnlock()
	if removed {
		// noteEvicted reads a ban from the same state and says so.
		s.noteEvicted(guildID, evictedKicked)
	}
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
		if err != nil || (reason != evictedKicked && reason != evictedBanned && reason != evictedUnseated) {
			continue
		}
		note, _ := s.store.GetSetting(evictedReasonPrefix + id)
		s.mu.Lock()
		s.evicted[id] = reason
		if note != "" {
			s.evictedReason[id] = note
		}
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

// isEvictionRefusal reports whether a refusal is one of the two that
// noteEvictionRefusal records: a verdict about us rather than about the peer
// we asked.
func isEvictionRefusal(refusal string) bool {
	r := strings.TrimSpace(refusal)
	return r == bannedRefusal || r == removedRefusal
}

// commitEvictsUs reports whether a commit we are about to fail to apply is the
// commit that threw us out — read from its own Remove proposals against the
// roster we still hold.
//
// Only THIS device's leaf counts. A commit that removes another device of our
// account is one we can apply and must: it is how an unlinked device's leaf
// goes (unlink.go, removalrequest.go) and how a sibling is re-added by a heal,
// and reading it as our own removal left this device refusing the commit and
// telling its user they were no longer a member of a guild they were still in.
// A kick removes every leaf of the account, one commit each; each device meets
// the commit for its own leaf, and applies the others on the way to it.
//
// A commit's Remove proposals say only what its signature vouches for, so a
// commit that does not verify against its sender's leaf at our epoch evicts
// nobody.
func (s *Service) commitEvictsUs(groupID, commit []byte) bool {
	if s.mls.VerifyCommit(s.ctx, groupID, commit) != nil {
		return false
	}
	removed, err := s.mls.RemovedBy(s.ctx, groupID, commit)
	if err != nil || len(removed) == 0 {
		return false
	}
	for _, cred := range removed {
		if bytes.Equal(cred, s.myCredential) {
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
