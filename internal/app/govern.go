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

// effectiveOwnerLocked is the guild's CURRENT owner fingerprint: the head of
// the replayed transfer_owner chain, or the founding owner when no transfer
// ever happened. Every authority decision must root HERE — rooting at
// guild.OwnerID would let a dethroned founder keep ruling. The founding key
// keeps only its non-authority roles (invite dialing, the Notes self-DM
// identity check). Caller must hold s.mu.
func (s *Service) effectiveOwnerLocked(guildID string) string {
	if st, ok := s.govState[guildID]; ok && st.owner != "" {
		return st.owner
	}
	// No folded state yet (no ops ingested / guild just tracked): the founder.
	if g, ok := s.guilds[guildID]; ok {
		return identity.FingerprintOf(g.OwnerID)
	}
	return ""
}

// effectiveOwner is effectiveOwnerLocked for callers that don't hold s.mu.
// Empty for an unknown guild — callers compare against non-empty fingerprints.
func (s *Service) effectiveOwner(guildID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.effectiveOwnerLocked(guildID)
}

// govStateCopy returns a deep copy of a guild's state so callers can't mutate
// the live maps. Caller must hold s.mu (read).
func (st GuildState) copy() GuildState {
	out := newGuildState()
	out.owner = st.owner
	out.heir = st.heir
	for k, v := range st.Roles {
		out.Roles[k] = v
	}
	for k, v := range st.MemberRoles {
		out.MemberRoles[k] = append([]string(nil), v...)
	}
	for k, v := range st.Banned {
		out.Banned[k] = v
	}
	for k, v := range st.Muted {
		out.Muted[k] = v
	}
	for k, v := range st.SlowMode {
		out.SlowMode[k] = v
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
	// transfer_owner receive-side gate: the named heir must be a CURRENT member
	// of the guild's MLS group. This lives at ingest rather than replay because
	// replay must stay a deterministic function of the recorded log, while
	// membership is a moving fact — every honest peer runs this same gate
	// independently before the op ever enters its log. A dropped op is not
	// remembered as seen, so if it merely outran the target's join commit,
	// the next sync re-delivery lands it.
	if o.Type == "transfer_owner" && !s.guildHasMember(guildID, o.Target) {
		return false
	}
	// set_heir names a future owner, so it runs the same gate on its Target
	// (an empty Target is a revocation and always passes). claim_heir's future
	// owner is its SIGNER, so the gate keys on that — an heir who has since
	// left the guild cannot claim it from outside.
	if o.Type == "set_heir" && o.Target != "" && !s.guildHasMember(guildID, o.Target) {
		return false
	}
	if o.Type == "claim_heir" && !s.guildHasMember(guildID, o.signerFpr()) {
		return false
	}
	hash := o.hash()
	s.mu.Lock()
	if s.govHashes[guildID] == nil {
		s.govHashes[guildID] = map[string]bool{}
	}
	if s.govHashes[guildID][hash] { // O(1) dedup instead of re-hashing the whole log
		s.mu.Unlock()
		return false
	}
	s.govHashes[guildID][hash] = true
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
	if _, ok := s.guilds[guildID]; !ok {
		return false
	}
	return s.govState[guildID].Can(s.effectiveOwnerLocked(guildID), self, need)
}

func (s *Service) canManageMembers(guildID string) bool {
	return s.hasPerm(guildID, PermManageMembers)
}

// memberHasPerm reports whether an arbitrary member (by fingerprint) holds a
// permission in the guild — used to authorize an inbound moderation action
// whose actor is the MLS-authenticated sender, not us.
func (s *Service) memberHasPerm(guildID, fpr string, need Permission) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.guilds[guildID]; !ok {
		return false
	}
	return s.govState[guildID].Can(s.effectiveOwnerLocked(guildID), fpr, need)
}

// ---- exported accessors for the bridge ----

// HasPermission reports whether this peer holds a permission bit in the guild.
func (s *Service) HasPermission(guildID string, perm Permission) bool {
	return s.hasPerm(guildID, perm)
}

// CanManageMembers reports whether this peer may invite/kick/ban in the guild.
func (s *Service) CanManageMembers(guildID string) bool { return s.canManageMembers(guildID) }

// MemberPermission returns a member's EFFECTIVE permission bitmask (union of its
// roles; the owner holds everything).
func (s *Service) MemberPermission(guildID, fingerprint string) Permission {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.guilds[guildID]; !ok {
		return 0
	}
	return s.govState[guildID].permsOf(s.effectiveOwnerLocked(guildID), fingerprint)
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

// IsGuildOwner reports whether a fingerprint is the guild's CURRENT owner
// (the founding owner unless a transfer_owner chain moved it).
func (s *Service) IsGuildOwner(guildID, fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.effectiveOwnerLocked(guildID) == fingerprint
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

// TransferOwnership hands the guild to another member — what Discord does.
// It is ONE signed governance op: the MLS group is untouched (nobody joins,
// leaves, or re-keys), so messages and calls flow straight through it. The
// issue-side checks here are a courtesy; ingest re-checks membership and
// replay re-checks the owner chain on EVERY peer, so a client that skips
// this function convinces nobody (receive-side doctrine).
func (s *Service) TransferOwnership(guildID, targetFpr string) error {
	self := s.id.Fingerprint()
	if targetFpr == "" {
		return fmt.Errorf("app: no member chosen")
	}
	if !s.IsGuildOwner(guildID, self) {
		return fmt.Errorf("app: only the owner can transfer ownership")
	}
	if targetFpr == self {
		return fmt.Errorf("app: you already own this server")
	}
	if !s.guildHasMember(guildID, targetFpr) {
		return fmt.Errorf("app: ownership can only be handed to a current member")
	}
	return s.issueGovOp(guildID, govOp{Type: "transfer_owner", Target: targetFpr})
}

// GuildHeir returns the fingerprint the current owner named as heir
// ("" = none). Derived from replay, so every peer reads the same answer.
func (s *Service) GuildHeir(guildID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.govState[guildID]; ok {
		return st.heir
	}
	return ""
}

// SetHeir pre-authorizes a member to claim ownership of the guild (owner
// only). BE CLEAR ABOUT WHAT THIS IS: the heir can cash it AT ANY TIME, not
// just "if the owner disappears" — a liveness gate is unimplementable in a
// partitioned P2P network without risking two crowned owners, so we don't
// pretend to have one. The owner can revoke (ClearHeir) any time before it's
// used. Issue-side checks are a courtesy; ingest re-checks membership and
// replay re-checks the owner's signature on EVERY peer.
func (s *Service) SetHeir(guildID, targetFpr string) error {
	self := s.id.Fingerprint()
	if targetFpr == "" {
		return fmt.Errorf("app: no member chosen")
	}
	if !s.IsGuildOwner(guildID, self) {
		return fmt.Errorf("app: only the owner can name an heir")
	}
	if targetFpr == self {
		return fmt.Errorf("app: you already own this server")
	}
	if !s.guildHasMember(guildID, targetFpr) {
		return fmt.Errorf("app: the heir must be a current member")
	}
	return s.issueGovOp(guildID, govOp{Type: "set_heir", Target: targetFpr})
}

// ClearHeir revokes the standing heir designation (owner only). An empty
// Target on a set_heir IS the revocation — one op type, one replay rule.
func (s *Service) ClearHeir(guildID string) error {
	if !s.IsGuildOwner(guildID, s.id.Fingerprint()) {
		return fmt.Errorf("app: only the owner can revoke an heir")
	}
	return s.issueGovOp(guildID, govOp{Type: "set_heir"})
}

// ClaimOwnership is the heir cashing the owner's pre-authorization: one signed
// governance op, MLS untouched, exactly like TransferOwnership but initiated
// by the successor. Replay only honors it when the signer matches the live
// designation, so a non-heir issuing this convinces nobody.
func (s *Service) ClaimOwnership(guildID string) error {
	self := s.id.Fingerprint()
	if s.IsGuildOwner(guildID, self) {
		return fmt.Errorf("app: you already own this server")
	}
	if s.GuildHeir(guildID) != self {
		return fmt.Errorf("app: the owner has not named you their heir")
	}
	return s.issueGovOp(guildID, govOp{Type: "claim_heir"})
}

// UnbanMember lifts a ban. Requires manage-members.
func (s *Service) UnbanMember(guildID, targetFpr string) error {
	if !s.canManageMembers(guildID) {
		return fmt.Errorf("app: you don't have permission to unban members")
	}
	return s.issueGovOp(guildID, govOp{Type: "unban", Target: targetFpr})
}

// MuteMember times a member out for the given number of minutes (advisory —
// honest clients drop a muted member's messages until it lifts). Requires
// mute-members.
func (s *Service) MuteMember(guildID, targetFpr string, minutes int) error {
	if !s.hasPerm(guildID, PermMuteMembers) {
		return fmt.Errorf("app: you don't have permission to mute members")
	}
	if minutes <= 0 {
		minutes = 10
	}
	until := time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
	return s.issueGovOp(guildID, govOp{Type: "mute", Target: targetFpr, Until: until})
}

// SetSlowMode sets a channel's posting interval (0 turns it off). A channel
// setting, so it rides manage-channels; enforcement is the same advisory
// two-legs mutes use — the sender's composer refuses, honest receivers drop.
func (s *Service) SetSlowMode(guildID, channelID string, seconds int64) error {
	if !s.hasPerm(guildID, PermManageChannels) {
		return fmt.Errorf("app: you don't have permission to manage channels")
	}
	if seconds < 0 {
		seconds = 0
	}
	if seconds > 21600 {
		seconds = 21600
	}
	return s.issueGovOp(guildID, govOp{Type: "slow_mode", ChannelID: channelID, Seconds: seconds})
}

// SlowModeSeconds is the channel's current interval (0 = off).
func (s *Service) SlowModeSeconds(guildID, channelID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.govState[guildID]; ok {
		return st.SlowMode[channelID]
	}
	return 0
}

// slowModeWait: how much longer fpr must wait before posting in channelID
// (0 = free to post). The owner and channel/message managers are exempt — the
// interval paces the room, not the people running it.
func (s *Service) slowModeWait(guildID, channelID, fpr string, at time.Time) time.Duration {
	interval := s.SlowModeSeconds(guildID, channelID)
	if interval <= 0 {
		return 0
	}
	if s.memberHasPerm(guildID, fpr, PermManageMessages) ||
		s.memberHasPerm(guildID, fpr, PermManageChannels) {
		return 0
	}
	s.mu.RLock()
	last := s.slowSeen[channelID+"|"+fpr]
	s.mu.RUnlock()
	if last == 0 {
		return 0
	}
	if remaining := interval - (at.Unix() - last); remaining > 0 {
		return time.Duration(remaining) * time.Second
	}
	return 0
}

func (s *Service) noteSlowSend(channelID, fpr string, at time.Time) {
	s.mu.Lock()
	if at.Unix() > s.slowSeen[channelID+"|"+fpr] {
		s.slowSeen[channelID+"|"+fpr] = at.Unix()
	}
	s.mu.Unlock()
}

// UnmuteMember lifts a mute. Requires mute-members.
func (s *Service) UnmuteMember(guildID, targetFpr string) error {
	if !s.hasPerm(guildID, PermMuteMembers) {
		return fmt.Errorf("app: you don't have permission to unmute members")
	}
	return s.issueGovOp(guildID, govOp{Type: "unmute", Target: targetFpr})
}

// mutedUntil returns the unix time a member is muted until (0 if not muted).
func (s *Service) mutedUntil(guildID, fpr string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.govState[guildID]; ok {
		return st.Muted[fpr]
	}
	return 0
}

// isMuted reports whether a member is currently muted in the guild.
func (s *Service) isMuted(guildID, fpr string) bool {
	return s.mutedUntil(guildID, fpr) > time.Now().Unix()
}

// MutedUntil is the exported accessor (0 = not muted).
func (s *Service) MutedUntil(guildID, fpr string) int64 { return s.mutedUntil(guildID, fpr) }

// removeMemberByFingerprint issues the MLS Remove for a present member. It
// matches on the ACCOUNT fingerprint (accountFingerprintOf), not the raw leaf
// hash, and removes EVERY matching leaf — a member with linked devices has one
// leaf per device (each a device cert), and a ban that removed only the first
// would leave the banned account still decrypting and posting from another
// device. Mirrors the account-scoped removal in the bridge.
func (s *Service) removeMemberByFingerprint(guildID, targetFpr string) error {
	creds, err := s.GuildMembers(guildID)
	if err != nil {
		return err
	}
	var firstErr error
	removed := 0
	for _, cred := range creds {
		if accountFingerprintOf(cred) == targetFpr {
			if err := s.RemoveMember(guildID, cred); err != nil && firstErr == nil {
				firstErr = err
			} else if err == nil {
				removed++
			}
		}
	}
	if firstErr != nil && removed == 0 {
		return firstErr
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
