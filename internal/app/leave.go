package app

import "sort"

// leave.go tells the rest of a guild that somebody walked out.
//
// Leaving used to be purely local: the leaver's device dropped its copy and
// went quiet, and the leaf stayed in the tree on the theory that a silent leaf
// harms nobody. It harmed the roster. Everyone else kept listing the leaver as a
// member, online by presence, forever; group keys kept being encrypted to a
// device that had said it wanted out; and nobody was told. So the leaver now
// says so on the guild's meta lane before going quiet, posts the line everyone
// sees, and one member who may commit removals takes the leaf out.

// announceLeave is the leaver's half. Called with the guild still tracked.
func (s *Service) announceLeave(guildID string, groupID []byte, firstChannel string) {
	if firstChannel != "" {
		s.sendSystem(firstChannel, "left the guild")
	}
	s.publishMeta(groupID, guildMeta{Type: "member_left", Fingerprint: s.id.Fingerprint()})
}

// handleMemberLeft is everyone else's half. MLS authenticated the sender, so the
// leaf to remove is exactly the one that spoke — one device, not the account: a
// person leaving on their laptop has not left on their phone.
//
// Exactly one committer should act, because two commits at one epoch fork the
// group (DESIGN §7.5). The guild's owner does it if they are here; otherwise
// the online committer whose fingerprint sorts first, a choice every peer that
// sees the same peers makes the same way.
func (s *Service) handleMemberLeft(guildID string, senderCred []byte) {
	actor := accountFingerprintOf(senderCred)
	if actor == "" || actor == s.id.Fingerprint() || !s.canManageMembers(guildID) {
		return
	}
	if !s.leaveRemoverIsMe(guildID) {
		return
	}
	go func() {
		_ = s.RemoveMember(guildID, senderCred)
		s.emitGuildUpdate()
	}()
}

// leaveRemoverIsMe decides whether this device is the one that removes a leaver.
func (s *Service) leaveRemoverIsMe(guildID string) bool {
	me := s.id.Fingerprint()
	owner := s.effectiveOwner(guildID)
	if owner == me {
		return true
	}
	online := s.authorizedCommittersOnline(guildID)
	fprs := []string{me}
	for _, p := range online {
		fpr := s.presence(p).Fingerprint
		if fpr == owner {
			return false // the owner is here and will do it
		}
		fprs = append(fprs, fpr)
	}
	sort.Strings(fprs)
	return fprs[0] == me
}
