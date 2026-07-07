package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zahak/concord/internal/identity"
)

// govern.go is the service-level wiring for guild governance: it turns the pure
// GuildState replay (govstate.go) into live, persisted, gossiped state and
// exposes the role/moderation actions. Signed ops propagate over the guild-meta
// topic and ride along in history sync, so every member converges on the same
// roles, assignments, and banlist.

// rebuildGovStateLocked recomputes a guild's folded governance state from its op
// log. Caller must hold s.mu.
func (s *Service) rebuildGovStateLocked(guildID string) {
	g, ok := s.guilds[guildID]
	if !ok {
		return
	}
	s.govState[guildID] = replayGuildOps(g.OwnerID, s.govOps[guildID])
}

// govStateCopy returns a deep copy of a guild's state so callers can't mutate
// the live maps. Caller must hold s.mu (read).
func (st GuildState) copy() GuildState {
	out := newGuildState()
	for k, v := range st.Roles {
		out.Roles[k] = v
	}
	for k, v := range st.MemberRoles {
		out.MemberRoles[k] = append([]string(nil), v...)
	}
	for k, v := range st.Banned {
		out.Banned[k] = v
	}
	return out
}

// GovState returns a snapshot of a guild's governance state.
func (s *Service) GovState(guildID string) GuildState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.govState[guildID]; ok {
		return st.copy()
	}
	return newGuildState()
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
// action, gossip, or sync). Dedupes by content hash, persists, appends, refolds
// state, refreshes the UI. Returns true if the op was new.
func (s *Service) ingestGovOp(guildID string, o govOp) bool {
	if !o.verifySig() {
		return false
	}
	hash := o.hash()
	s.mu.Lock()
	for _, existing := range s.govOps[guildID] {
		if existing.hash() == hash {
			s.mu.Unlock()
			return false
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

// nextGovSeq returns one past the highest op seq known for the guild.
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

// issueGovOp fills in seq/signer/time, signs, records, and broadcasts an op.
// Callers set Type and the op-specific fields. Replay re-validates authority on
// every peer, so a caller that shouldn't have issued it changes nothing.
func (s *Service) issueGovOp(guildID string, o govOp) error {
	o.Seq = s.nextGovSeq(guildID)
	o.Signer = s.id.PublicKey()
	o.Time = time.Now().UnixNano()
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

// hasPerm reports whether THIS peer holds a permission in the guild.
func (s *Service) hasPerm(guildID string, need Permission) bool {
	self := s.id.Fingerprint()
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return false
	}
	return s.govState[guildID].Can(identity.FingerprintOf(g.OwnerID), self, need)
}

func (s *Service) canManageMembers(guildID string) bool {
	return s.hasPerm(guildID, PermManageMembers)
}

// ---- exported accessors for the bridge ----

// HasPermission reports whether this peer holds a permission bit in the guild.
func (s *Service) HasPermission(guildID string, perm Permission) bool { return s.hasPerm(guildID, perm) }

// CanManageMembers reports whether this peer may invite/kick/ban in the guild.
func (s *Service) CanManageMembers(guildID string) bool { return s.canManageMembers(guildID) }

// MemberPermission returns a member's EFFECTIVE permission bitmask (union of its
// roles; the owner holds everything).
func (s *Service) MemberPermission(guildID, fingerprint string) Permission {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return 0
	}
	return s.govState[guildID].permsOf(identity.FingerprintOf(g.OwnerID), fingerprint)
}

// Roles returns a guild's role definitions.
func (s *Service) Roles(guildID string) []Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.govState[guildID]
	if !ok {
		return nil
	}
	out := make([]Role, 0, len(st.Roles))
	for _, r := range st.Roles {
		out = append(out, r)
	}
	return out
}

// MemberRoleIDs returns the role IDs assigned to a member.
func (s *Service) MemberRoleIDs(guildID, fingerprint string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.govState[guildID]; ok {
		return append([]string(nil), st.MemberRoles[fingerprint]...)
	}
	return nil
}

// IsGuildOwner reports whether a fingerprint is the guild's owner.
func (s *Service) IsGuildOwner(guildID, fingerprint string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return false
	}
	return identity.FingerprintOf(g.OwnerID) == fingerprint
}

// BannedFingerprints returns the guild's banlist.
func (s *Service) BannedFingerprints(guildID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.govState[guildID]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(st.Banned))
	for fpr := range st.Banned {
		out = append(out, fpr)
	}
	return out
}

// ---- role actions ----

func newRoleID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "r_" + hex.EncodeToString(b[:])
}

// UpsertRole creates (roleID == "") or edits a role. Requires ManageRoles (or
// owner); replay enforces that the perms/position don't exceed the caller.
func (s *Service) UpsertRole(guildID, roleID, name, color string, perms Permission, position int) (string, error) {
	if !s.hasPerm(guildID, PermManageRoles) {
		return "", fmt.Errorf("app: you don't have permission to manage roles")
	}
	if roleID == "" {
		roleID = newRoleID()
	}
	err := s.issueGovOp(guildID, govOp{
		Type: "role_upsert", RoleID: roleID, Name: name, Color: color,
		Perms: uint32(perms & permAll), Position: position,
	})
	return roleID, err
}

// DeleteRole removes a role and unassigns it from everyone. Requires ManageRoles.
func (s *Service) DeleteRole(guildID, roleID string) error {
	if !s.hasPerm(guildID, PermManageRoles) {
		return fmt.Errorf("app: you don't have permission to manage roles")
	}
	return s.issueGovOp(guildID, govOp{Type: "role_delete", RoleID: roleID})
}

// AssignRole grants (add) or revokes (add=false) a role to a member. Requires
// ManageRoles; replay enforces the rank rules.
func (s *Service) AssignRole(guildID, targetFpr, roleID string, add bool) error {
	if !s.hasPerm(guildID, PermManageRoles) {
		return fmt.Errorf("app: you don't have permission to assign roles")
	}
	return s.issueGovOp(guildID, govOp{Type: "role_assign", Target: targetFpr, RoleID: roleID, Add: add})
}

// BanMember bars a fingerprint and, if present, evicts it. Requires manage-members.
func (s *Service) BanMember(guildID, targetFpr string) error {
	if !s.canManageMembers(guildID) {
		return fmt.Errorf("app: you don't have permission to ban members")
	}
	if err := s.issueGovOp(guildID, govOp{Type: "ban", Target: targetFpr}); err != nil {
		return err
	}
	return s.removeMemberByFingerprint(guildID, targetFpr)
}

// UnbanMember lifts a ban. Requires manage-members.
func (s *Service) UnbanMember(guildID, targetFpr string) error {
	if !s.canManageMembers(guildID) {
		return fmt.Errorf("app: you don't have permission to unban members")
	}
	return s.issueGovOp(guildID, govOp{Type: "unban", Target: targetFpr})
}

// removeMemberByFingerprint issues the MLS Remove for a present member.
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
	return nil
}

// govOpsFor returns the raw op log for a guild (for a sync payload).
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
