package app

import (
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

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

// Leaving is also the account's decision, not one device's: a person who
// leaves a guild on their desktop has not chosen to stay in it on their phone,
// and the phone carrying on (posting as them, receiving everything) is the
// opposite of what they asked for. The account's other devices follow a leave
// two ways: live, by hearing the leaving device's own member_left frame on the
// guild's meta lane, and on the next hello, which lists the guilds a device
// has left and when, for a device that was offline at the time.

// leftGuildsFor lists the guilds this device left, with when, for a device of
// our own account; nil for anyone else.
func (s *Service) leftGuildsFor(p peer.ID) map[string]int64 {
	if !s.ownDevice(p) {
		return nil
	}
	left, err := s.store.LeftGuildsSince()
	if err != nil || len(left) == 0 {
		return nil
	}
	return left
}

// arrivalsFor stamps each guild we offer a device of ours with when it
// arrived here, so a device that left it can tell a rejoin from a leftover.
func (s *Service) arrivalsFor(p peer.ID) map[string]int64 {
	if !s.ownDevice(p) {
		return nil
	}
	out := map[string]int64{}
	for _, g := range s.Guilds() {
		if at := s.guildArrivedNano(g.ID); at > 0 {
			out[g.ID] = at
		}
	}
	return out
}

// followSiblingLeaves leaves every guild another device of ours left after
// this device had it. A guild that arrived here after that leave is a rejoin
// on this device and stays.
func (s *Service) followSiblingLeaves(from peer.ID, left map[string]int64) {
	if len(left) == 0 || !s.ownDevice(from) {
		return
	}
	for guildID, at := range left {
		if at <= 0 {
			continue
		}
		if s.guildArrivedNano(guildID) > leftEnd(at) {
			continue
		}
		s.leaveWithSibling(guildID)
	}
}

// leftEnd is the latest moment a leave stamped at (whole unix seconds) can
// have happened, in unix nanoseconds: an arrival is only "after the leave"
// when it is after the whole of that second.
func leftEnd(at int64) int64 { return (at + 1) * int64(time.Second) }

// leaveWithSibling leaves a guild because another device of this account did.
// No "left the guild" line: the account already said so once, from the device
// the person actually used. The member_left frame does go out, because it is
// what has a committer take THIS device's leaf out.
func (s *Service) leaveWithSibling(guildID string) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	isGuild := ok && g.Kind == ""
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !isGuild {
		return
	}
	if s.EvictedFrom(guildID) == "" {
		s.announceLeave(guildID, groupID, "")
	}
	_ = s.deleteGuildLocal(guildID)
}
