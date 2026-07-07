package app

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"

	"github.com/zahak/concord/internal/identity"
)

// govstate.go is the pure (I/O-free) core of guild governance: the permission
// model, named roles, the signed-operation format, and the deterministic replay
// that folds a log of ops into a GuildState. Keeping it pure makes the
// security-critical rules — who may define/assign roles, who may ban, and the
// anti-escalation guarantees — exhaustively unit-testable without networks.
//
// Trust anchor: the guild owner (guild.OwnerID) implicitly holds every
// permission and outranks every role. Everyone else's power comes from named
// roles the owner (or a delegate holding ManageRoles) defines and assigns.

// Permission is a capability bit. Each maps to a concrete action in the P2P
// system, so a role grants exactly the powers it names — not a blanket "mod".
type Permission uint32

const (
	// PermManageMembers: invite, kick, ban. This is the bit the MLS committer
	// gate consults — a holder's membership commits are accepted by honest peers.
	PermManageMembers Permission = 1 << iota
	// PermManageMessages: delete anyone's messages and pin/unpin (moderation).
	PermManageMessages
	// PermManageChannels: create, rename, move, delete channels and categories.
	PermManageChannels
	// PermManageGuild: rename the guild and manage custom emoji.
	PermManageGuild
	// PermManageRoles: define, edit, delete, and assign roles (bounded by rank).
	PermManageRoles
	// PermMuteMembers: mute/time-out a member (advisory — honest clients drop a
	// muted member's messages until it lifts).
	PermMuteMembers
	// PermSyncHost: a designated always-on history-sync/relay host for the guild,
	// preferred as a re-add and backfill source.
	PermSyncHost
)

const permAll = PermManageMembers | PermManageMessages | PermManageChannels |
	PermManageGuild | PermManageRoles | PermMuteMembers | PermSyncHost

// Has reports whether the bitmask includes every bit of p.
func (perm Permission) Has(p Permission) bool { return perm&p == p }

// Role is a named, colored bundle of permissions with a hierarchy position
// (higher = more senior). A member may hold several; effective permission is
// the union.
type Role struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Color    string     `json:"color"`
	Perms    Permission `json:"perms"`
	Position int        `json:"position"`
}

// govOp is one signed governance mutation. Ops form a per-guild log; replaying
// them in canonical order yields the GuildState. The signature covers every
// field except Sig, binding the op to its author's account key.
type govOp struct {
	Seq    uint64 `json:"seq"`
	Signer []byte `json:"signer"` // author's Ed25519 account public key
	Type   string `json:"type"`   // role_upsert | role_delete | role_assign | ban | unban

	// role_upsert
	RoleID   string `json:"roleId,omitempty"`
	Name     string `json:"name,omitempty"`
	Color    string `json:"color,omitempty"`
	Perms    uint32 `json:"perms,omitempty"`
	Position int    `json:"position,omitempty"`

	// role_assign (Add=false removes) — Target is a member fingerprint;
	// ban/unban also use Target.
	Target string `json:"target,omitempty"`
	Add    bool   `json:"add,omitempty"`

	Time int64  `json:"t"`   // author wall-clock (unix nanos), ordering tiebreak
	Sig  []byte `json:"sig"` // signature over the op with Sig zeroed
}

func (o govOp) signingBytes() []byte {
	o.Sig = nil
	b, _ := json.Marshal(o)
	return b
}

func (o govOp) hash() string {
	sum := sha256.Sum256(o.signingBytes())
	return hex.EncodeToString(sum[:])
}

func (o govOp) verifySig() bool {
	if len(o.Signer) != ed25519.PublicKeySize || len(o.Sig) != ed25519.SignatureSize {
		return false
	}
	return identity.Verify(o.Signer, o.signingBytes(), o.Sig)
}

func (o govOp) signerFpr() string { return identity.FingerprintOf(o.Signer) }

// GuildState is the folded result of replaying a guild's governance log.
type GuildState struct {
	Roles       map[string]Role     // roleID -> role definition
	MemberRoles map[string][]string // fingerprint -> assigned role IDs
	Banned      map[string]bool     // barred fingerprints
}

func newGuildState() GuildState {
	return GuildState{
		Roles:       map[string]Role{},
		MemberRoles: map[string][]string{},
		Banned:      map[string]bool{},
	}
}

// permsOf returns a member's effective permission bitmask (union of its roles).
// The owner implicitly holds everything.
func (st GuildState) permsOf(ownerFpr, fpr string) Permission {
	if fpr == ownerFpr {
		return permAll
	}
	var p Permission
	for _, rid := range st.MemberRoles[fpr] {
		if r, ok := st.Roles[rid]; ok {
			p |= r.Perms
		}
	}
	return p
}

// Can reports whether the member holds every bit of need. Owner always passes.
func (st GuildState) Can(ownerFpr, fpr string, need Permission) bool {
	return st.permsOf(ownerFpr, fpr).Has(need)
}

// topPosition is the highest role position a member holds (its rank in the
// hierarchy). The owner outranks everyone; a member with no roles is below all.
func (st GuildState) topPosition(ownerFpr, fpr string) int {
	if fpr == ownerFpr {
		return math.MaxInt
	}
	top := -1
	for _, rid := range st.MemberRoles[fpr] {
		if r, ok := st.Roles[rid]; ok && r.Position > top {
			top = r.Position
		}
	}
	return top
}

// canonicalOps sorts a log into the deterministic replay order every peer agrees
// on: by Seq, then Time, then op hash. Sorting (not trusting arrival order)
// means peers that received ops differently still fold to the same state.
func canonicalOps(ops []govOp) []govOp {
	out := append([]govOp(nil), ops...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Seq != b.Seq {
			return a.Seq < b.Seq
		}
		if a.Time != b.Time {
			return a.Time < b.Time
		}
		return a.hash() < b.hash()
	})
	return out
}

// replayGuildOps folds a governance log into a GuildState, enforcing at each step
// that the op's signer is authorized AND cannot escalate beyond its own rank:
//   - role_upsert: signer needs ManageRoles (or is owner); the new role's perms
//     must be a subset of the signer's own, and its position strictly below the
//     signer's rank (owner exempt). Editing a role above your rank is refused.
//   - role_delete: ManageRoles (or owner); the role must be below your rank.
//   - role_assign: ManageRoles (or owner); the role must be below your rank; you
//     cannot assign roles to the owner.
//   - ban/unban: ManageMembers (or owner); the owner cannot be banned; a ban
//     strips the member's roles.
//
// Invalidly-signed or unauthorized ops are skipped, not fatal (the log is
// gossiped; a peer may see a stray or malicious op).
func replayGuildOps(owner []byte, ops []govOp) GuildState {
	ownerFpr := identity.FingerprintOf(owner)
	st := newGuildState()
	for _, o := range canonicalOps(ops) {
		if !o.verifySig() {
			continue
		}
		signer := o.signerFpr()
		isOwner := signer == ownerFpr

		switch o.Type {
		case "role_upsert":
			if o.RoleID == "" {
				continue
			}
			if !isOwner && !st.Can(ownerFpr, signer, PermManageRoles) {
				continue
			}
			newPerms := Permission(o.Perms) & permAll
			if !isOwner {
				// Can't mint a role more powerful than yourself.
				if !st.permsOf(ownerFpr, signer).Has(newPerms) {
					continue
				}
				// Can't create/move a role at or above your own rank.
				if o.Position >= st.topPosition(ownerFpr, signer) {
					continue
				}
				// Can't edit an existing role that outranks you.
				if existing, ok := st.Roles[o.RoleID]; ok && existing.Position >= st.topPosition(ownerFpr, signer) {
					continue
				}
			}
			st.Roles[o.RoleID] = Role{
				ID: o.RoleID, Name: o.Name, Color: o.Color, Perms: newPerms, Position: o.Position,
			}
		case "role_delete":
			r, ok := st.Roles[o.RoleID]
			if !ok {
				continue
			}
			if !isOwner {
				if !st.Can(ownerFpr, signer, PermManageRoles) || r.Position >= st.topPosition(ownerFpr, signer) {
					continue
				}
			}
			delete(st.Roles, o.RoleID)
			for fpr := range st.MemberRoles {
				st.MemberRoles[fpr] = removeStr(st.MemberRoles[fpr], o.RoleID)
			}
		case "role_assign":
			r, ok := st.Roles[o.RoleID]
			if !ok || o.Target == "" || o.Target == ownerFpr {
				continue
			}
			if !isOwner {
				// Need ManageRoles, and the role must be below your own rank.
				if !st.Can(ownerFpr, signer, PermManageRoles) || r.Position >= st.topPosition(ownerFpr, signer) {
					continue
				}
			}
			if o.Add {
				if !containsStr(st.MemberRoles[o.Target], o.RoleID) {
					st.MemberRoles[o.Target] = append(st.MemberRoles[o.Target], o.RoleID)
				}
			} else {
				st.MemberRoles[o.Target] = removeStr(st.MemberRoles[o.Target], o.RoleID)
			}
		case "ban":
			if !isOwner && !st.Can(ownerFpr, signer, PermManageMembers) {
				continue
			}
			if o.Target == ownerFpr || o.Target == "" {
				continue
			}
			st.Banned[o.Target] = true
			delete(st.MemberRoles, o.Target) // a banned member forfeits its roles
		case "unban":
			if !isOwner && !st.Can(ownerFpr, signer, PermManageMembers) {
				continue
			}
			delete(st.Banned, o.Target)
		}
	}
	return st
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func removeStr(s []string, v string) []string {
	out := s[:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
