package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/zahak/concord/internal/identity"
)

// govern.go is the service-level wiring for guild governance: it turns the pure
// GuildState replay (govstate.go) into live, persisted, gossiped state and
// exposes the moderation actions (grant/revoke permissions, ban, unban, kick).
// The signed ops propagate over the guild-meta topic and ride along in history
// sync, so every member converges on the same roles and banlist.

// rebuildGovStateLocked recomputes a guild's folded governance state from its op
// log. Caller must hold s.mu.
func (s *Service) rebuildGovStateLocked(guildID string) {
	g, ok := s.guilds[guildID]
	if !ok {
		return
	}
	s.govState[guildID] = replayGuildOps(g.OwnerID, s.govOps[guildID])
}

// GovState returns a snapshot of a guild's governance state (roles + banlist).
func (s *Service) GovState(guildID string) GuildState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.govState[guildID]
	if !ok {
		return newGuildState()
	}
	// Return copies so callers can't mutate the live maps.
	perms := make(map[string]Permission, len(st.Perms))
	for k, v := range st.Perms {
		perms[k] = v
	}
	banned := make(map[string]bool, len(st.Banned))
	for k, v := range st.Banned {
		banned[k] = v
	}
	return GuildState{Perms: perms, Banned: banned}
}

// isBanned reports whether a fingerprint is barred from a guild.
func (s *Service) isBanned(guildID, fingerprint string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.govState[guildID]; ok {
		return st.Banned[fingerprint]
	}
	return false
}

// ingestGovOp validates and records a governance op (from any source: local
// action, gossip, or sync). It dedupes by content hash, persists, appends to the
// log, refolds the state, and refreshes the UI. Returns true if the op was new.
func (s *Service) ingestGovOp(guildID string, o govOp) bool {
	if !o.verifySig() {
		return false
	}
	hash := o.hash()
	s.mu.Lock()
	for _, existing := range s.govOps[guildID] {
		if existing.hash() == hash {
			s.mu.Unlock()
			return false // already have it
		}
	}
	s.govOps[guildID] = append(s.govOps[guildID], o)
	s.rebuildGovStateLocked(guildID)
	s.mu.Unlock()

	raw, _ := json.Marshal(o)
	_ = s.store.SaveGuildOp(guildID, hash, raw)
	s.emitGuildUpdate()
	return true
}

// nextGovSeq returns a sequence number one past the highest op currently known
// for the guild, so a new op sorts after everything we've seen.
func (s *Service) nextGovSeq(guildID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var max uint64
	for _, o := range s.govOps[guildID] {
		if o.Seq > max {
			max = o.Seq
		}
	}
	return max + 1
}

// issueGovOp builds, signs, records, and broadcasts a governance op. It does not
// itself check the caller's authority — callers (SetMemberPermissions, BanMember,
// …) enforce that first; replay re-validates on every peer regardless.
func (s *Service) issueGovOp(guildID, typ, target string, perms Permission) error {
	o := govOp{
		Seq:    s.nextGovSeq(guildID),
		Signer: s.id.PublicKey(),
		Type:   typ,
		Target: target,
		Perms:  uint32(perms),
		Time:   time.Now().UnixNano(),
	}
	o.Sig = s.id.Sign(o.signingBytes())

	if !s.ingestGovOp(guildID, o) {
		return fmt.Errorf("app: governance op rejected locally")
	}

	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if ok {
		raw, _ := json.Marshal(o)
		s.publishMeta(groupID, guildMeta{Type: "gov_op", GovOp: raw})
	}
	return nil
}

// canManageMembers reports whether this peer may invite/kick/ban in the guild.
func (s *Service) canManageMembers(guildID string) bool {
	self := s.id.Fingerprint()
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return false
	}
	ownerFpr := identity.FingerprintOf(g.OwnerID)
	st := s.govState[guildID]
	return st.Can(ownerFpr, self, PermManageMembers)
}

// SetMemberPermissions grants (or, with perms==0, clears) a member's permission
// bitmask. Owner-only, matching the replay rule that prevents privilege
// escalation by moderators.
func (s *Service) SetMemberPermissions(guildID, targetFpr string, perms Permission) error {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %q", guildID)
	}
	if !isOwnerCred(g.OwnerID, s.PublicKey()) {
		return fmt.Errorf("app: only the owner can assign permissions")
	}
	return s.issueGovOp(guildID, "set_perms", targetFpr, perms)
}

// BanMember bars a fingerprint from the guild (recorded in governance state) and,
// if that member is currently present, removes them from the MLS group. Requires
// manage-members.
func (s *Service) BanMember(guildID, targetFpr string) error {
	if !s.canManageMembers(guildID) {
		return fmt.Errorf("app: you don't have permission to ban members")
	}
	if err := s.issueGovOp(guildID, "ban", targetFpr, 0); err != nil {
		return err
	}
	// If present, evict them now (the ban keeps them out on any rejoin attempt).
	return s.removeMemberByFingerprint(guildID, targetFpr)
}

// UnbanMember lifts a ban. Requires manage-members.
func (s *Service) UnbanMember(guildID, targetFpr string) error {
	if !s.canManageMembers(guildID) {
		return fmt.Errorf("app: you don't have permission to unban members")
	}
	return s.issueGovOp(guildID, "unban", targetFpr, 0)
}

// removeMemberByFingerprint issues the MLS Remove for the member whose credential
// yields targetFpr, if they are in the group. A no-op if they aren't present.
func (s *Service) removeMemberByFingerprint(guildID, targetFpr string) error {
	creds, err := s.GuildMembers(guildID)
	if err != nil {
		return err
	}
	for _, cred := range creds {
		if identity.FingerprintOf(cred) == targetFpr {
			return s.RemoveMember(guildID, cred)
		}
	}
	return nil // not currently a member; the banlist still bars rejoin
}

// govOpsFor returns the raw op log for a guild, for including in a sync payload.
func (s *Service) govOpsFor(guildID string) []json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]json.RawMessage, 0, len(s.govOps[guildID]))
	for _, o := range s.govOps[guildID] {
		raw, _ := json.Marshal(o)
		out = append(out, raw)
	}
	return out
}

// ingestGovOpsRaw folds a batch of raw ops (from a sync payload) into state.
func (s *Service) ingestGovOpsRaw(guildID string, raws []json.RawMessage) {
	for _, raw := range raws {
		var o govOp
		if json.Unmarshal(raw, &o) == nil {
			s.ingestGovOp(guildID, o)
		}
	}
}

func isOwnerCred(ownerID, cred []byte) bool {
	return len(ownerID) > 0 && string(ownerID) == string(cred)
}
