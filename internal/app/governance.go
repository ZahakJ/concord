package app

import (
	"bytes"

	"github.com/zahak/concord/internal/identity"
)

// governance.go is the guild authorization layer: it decides whose membership
// commits (adds/removes) honest peers will accept. This is the cryptographic
// enforcement point of the guild's power structure — "who can kick/ban/invite"
// is exactly "whose commits everyone applies." Every commit is MLS-signed by its
// author's leaf, so an author identity extracted from the commit (via
// mls.CommitSender) is unforgeable; gating application on that identity is what
// makes a role or ban actually binding rather than advisory.
//
// The model is intentionally extensible from one function:
//   - foundational (now): the guild owner is the sole authorized committer,
//     which reproduces the old "owner is sole committer" behavior but *enforces*
//     it — an unauthorized member can no longer kick/add by crafting a commit,
//     because honest peers refuse to apply it.
//   - Phase 3 will accept commits from members holding a "manage members" role.
//   - Phase 4b will accept a member's self-device commits (adding/removing a leaf
//     whose device cert chains to that member's own account key).

// authorizedCommitter reports whether the member identified by senderCred (their
// MLS credential == Concord account public key) may publish membership commits
// for the given guild. It fails closed: an unknown guild, an empty credential,
// or any ambiguity yields false, so a commit that cannot be positively
// authorized is dropped rather than applied.
func (s *Service) authorizedCommitter(guildID string, senderCred []byte) bool {
	if len(senderCred) == 0 {
		return false
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var ownerID []byte
	var st GuildState
	if ok {
		ownerID = g.OwnerID
		st = s.govState[guildID]
	}
	s.mu.RUnlock()
	if !ok {
		return false
	}
	// The owner is always authorized.
	if bytes.Equal(ownerID, senderCred) {
		return true
	}
	// A member the owner granted "manage members" may also invite/kick. This is
	// what lets moderation happen without the owner being online — the crux of
	// not being load-bearing. (Phase 4b self-device commits slot in here too.)
	return st.Can(identity.FingerprintOf(ownerID), identity.FingerprintOf(senderCred), PermManageMembers)
}

// commitAuthorized extracts a commit's author from its MLS framing and runs it
// through authorizedCommitter. It is the guard both commit-application paths
// (the live control topic and history-sync backfill) call before advancing the
// group, so the authorization rule holds no matter how a commit reaches us. A
// commit whose author cannot be determined (e.g. it targets an epoch we have not
// reached, so the sender leaf is unresolvable) is treated as unauthorized; the
// caller then falls back to history sync, which re-validates from a peer.
func (s *Service) commitAuthorized(guildID string, groupID, commit []byte) bool {
	sender, err := s.mls.CommitSender(s.ctx, groupID, commit)
	if err != nil {
		return false
	}
	return s.authorizedCommitter(guildID, sender)
}
