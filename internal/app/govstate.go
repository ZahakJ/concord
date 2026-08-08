package app

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"

	"github.com/ZahakJ/concord/internal/identity"
)

// govstate.go is the pure (I/O-free) core of guild governance: the permission
// model, named roles, the signed-operation format, and the deterministic replay
// that folds a log of ops into a GuildState. Keeping it pure makes the
// security-critical rules — who may define/assign roles, who may ban, and the
// anti-escalation guarantees — exhaustively unit-testable without networks.
//
// Trust anchor: the guild owner implicitly holds every permission and outranks
// every role. Everyone else's power comes from named roles the owner (or a
// delegate holding ManageRoles) defines and assigns. guild.OwnerID stays the
// immutable FOUNDING key and replay seed; the CURRENT owner is computed by
// replay — a valid transfer_owner chain (each link signed by the then-current
// owner) moves it, Discord-style, without touching MLS membership at all.

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
	Type   string `json:"type"`   // role_upsert | role_delete | role_assign | ban | unban | mute | unmute | transfer_owner | set_heir | claim_heir

	// role_upsert
	RoleID   string `json:"roleId,omitempty"`
	Name     string `json:"name,omitempty"`
	Color    string `json:"color,omitempty"`
	Perms    uint32 `json:"perms,omitempty"`
	Position int    `json:"position,omitempty"`

	// role_assign (Add=false removes) — Target is a member fingerprint;
	// ban/unban/mute/unmute also use Target, transfer_owner names the
	// NEW owner's account fingerprint in it, and set_heir names the
	// designated heir (empty = revoke the designation).
	Target string `json:"target,omitempty"`
	Add    bool   `json:"add,omitempty"`
	Until  int64  `json:"until,omitempty"` // mute: muted-until (unix seconds)

	// slow_mode — per-channel posting interval. Seconds <= 0 turns it off.
	ChannelID string `json:"channelId,omitempty"`
	Seconds   int64  `json:"seconds,omitempty"`

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
	Muted       map[string]int64    // fingerprint -> muted-until (unix seconds)
	SlowMode    map[string]int64    // channelID -> seconds between posts (absent = off)
	// owner is the CURRENT owner's account fingerprint as computed by replay:
	// the founding owner unless a valid transfer_owner chain moved it.
	// Unexported because it is derived — Owner() is the read, and every
	// authority decision must consult it rather than guild.OwnerID.
	owner string
	// heir is the fingerprint the current owner pre-authorized to claim
	// ownership ("" = none). Derived like owner: only a set_heir signed by the
	// then-current owner records it, and it is consumed by a successful
	// claim_heir or cleared by any ownership change.
	heir string
}

// Owner is the guild's EFFECTIVE owner fingerprint after replaying the log
// (empty only for a zero GuildState that never saw a replay).
func (st GuildState) Owner() string { return st.owner }

// Heir is the fingerprint the current owner named as their successor
// ("" = none). The heir may claim ownership AT ANY TIME — see set_heir in
// replayGuildOps for why this is deliberately not a liveness-gated switch.
func (st GuildState) Heir() string { return st.heir }

func newGuildState() GuildState {
	return GuildState{
		Roles:       map[string]Role{},
		MemberRoles: map[string][]string{},
		Banned:      map[string]bool{},
		Muted:       map[string]int64{},
		SlowMode:    map[string]int64{},
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
//   - role_assign: ManageRoles (or owner); the role must be below your rank;
//     only the OWNER may assign a role to the owner. Note what this
//     combination forbids: a member holding ManageRoles cannot escalate — any
//     role they mint is capped at their own permissions (role_upsert) and any
//     role they assign must rank below them, so "make myself admin" is
//     impossible unless you already are one.
//   - ban/unban: ManageMembers (or owner); the owner cannot be banned; a ban
//     strips the member's roles.
//   - transfer_owner: only the THEN-current owner's signature moves ownership,
//     evaluated in canonical order so every peer agrees on the chain A→B→C and
//     a stale op the dethroned founder signs afterwards is dead on arrival.
//   - set_heir / claim_heir: the involuntary-succession pair. Only the
//     then-current owner names (or, with an empty Target, revokes) an heir;
//     only the named heir's own signature converts the designation into
//     ownership, via the same cur machinery as transfer_owner. Any ownership
//     change voids the designation.
//
// "Owner" everywhere below means the CURRENT owner (cur): it starts at the
// founding key and each valid transfer_owner re-anchors every later op's
// authority check — including who is ban/mute-immune — at the new owner.
//
// Invalidly-signed or unauthorized ops are skipped, not fatal (the log is
// gossiped; a peer may see a stray or malicious op).
func replayGuildOps(owner []byte, ops []govOp) GuildState {
	cur := identity.FingerprintOf(owner)
	st := newGuildState()
	for _, o := range canonicalOps(ops) {
		if !o.verifySig() {
			continue
		}
		signer := o.signerFpr()
		isOwner := signer == cur

		switch o.Type {
		case "role_upsert":
			if o.RoleID == "" {
				continue
			}
			if !isOwner && !st.Can(cur, signer, PermManageRoles) {
				continue
			}
			newPerms := Permission(o.Perms) & permAll
			if !isOwner {
				// Can't mint a role more powerful than yourself.
				if !st.permsOf(cur, signer).Has(newPerms) {
					continue
				}
				// Can't create/move a role at or above your own rank.
				if o.Position >= st.topPosition(cur, signer) {
					continue
				}
				// Can't edit an existing role that outranks you.
				if existing, ok := st.Roles[o.RoleID]; ok && existing.Position >= st.topPosition(cur, signer) {
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
				if !st.Can(cur, signer, PermManageRoles) || r.Position >= st.topPosition(cur, signer) {
					continue
				}
			}
			delete(st.Roles, o.RoleID)
			for fpr := range st.MemberRoles {
				st.MemberRoles[fpr] = removeStr(st.MemberRoles[fpr], o.RoleID)
			}
		case "role_assign":
			r, ok := st.Roles[o.RoleID]
			// Nobody but the owner may hand roles TO the owner — that keeps a
			// moderator from decorating (or re-ranking) them. The owner giving
			// THEMSELVES a role is fine and is how they take the Admin badge;
			// it grants nothing they don't already have.
			if !ok || o.Target == "" || (o.Target == cur && !isOwner) {
				continue
			}
			if !isOwner {
				// Need ManageRoles, and the role must be below your own rank.
				if !st.Can(cur, signer, PermManageRoles) || r.Position >= st.topPosition(cur, signer) {
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
			if !isOwner && !st.Can(cur, signer, PermManageMembers) {
				continue
			}
			if o.Target == cur || o.Target == "" {
				continue
			}
			st.Banned[o.Target] = true
			delete(st.MemberRoles, o.Target) // a banned member forfeits its roles
		case "unban":
			if !isOwner && !st.Can(cur, signer, PermManageMembers) {
				continue
			}
			delete(st.Banned, o.Target)
		case "mute":
			if !isOwner && !st.Can(cur, signer, PermMuteMembers) {
				continue
			}
			if o.Target == cur || o.Target == "" {
				continue // the owner can't be muted
			}
			st.Muted[o.Target] = o.Until
		case "unmute":
			if !isOwner && !st.Can(cur, signer, PermMuteMembers) {
				continue
			}
			delete(st.Muted, o.Target)
		case "slow_mode":
			// A channel setting, so it rides manage-channels. The clamp is part
			// of the REPLAY (not just the issuing UI) so every honest client
			// derives the identical state from a hand-crafted op too.
			if !isOwner && !st.Can(cur, signer, PermManageChannels) {
				continue
			}
			if o.ChannelID == "" {
				continue
			}
			if o.Seconds <= 0 {
				delete(st.SlowMode, o.ChannelID)
			} else if o.Seconds > 21600 {
				st.SlowMode[o.ChannelID] = 21600 // 6h, Discord's own ceiling
			} else {
				st.SlowMode[o.ChannelID] = o.Seconds
			}
		case "transfer_owner":
			// Only the reigning owner abdicates — nobody else's signature moves
			// the crown, no matter how the op reached us. Transfer-to-self is a
			// no-op, not a state change. (Membership of the target is enforced
			// receive-side at ingest, because MLS membership is a moving fact
			// while this replay must stay a pure function of the log.)
			//
			// A ban on the target does NOT veto the handover, and that is
			// deliberate: the owner outranks every ban and cannot be removed,
			// so "banned owner" is not a state this system can be in. Letting a
			// ban block a transfer also handed an attacker a lever — a ban
			// folded in ahead of the transfer made the transfer skip, leaving
			// the outgoing owner in place. Naming someone owner readmits them.
			if !isOwner || o.Target == "" || o.Target == cur {
				continue
			}
			delete(st.Banned, o.Target)
			cur = o.Target
			// A handover voids any standing heir designation: it was the OLD
			// owner's trust decision, and the new owner names their own.
			st.heir = ""
		case "set_heir":
			// Only the reigning owner pre-authorizes a successor (or revokes
			// the designation with an empty Target). This is the involuntary-
			// succession primitive: the resulting claim is valid WHENEVER the
			// heir uses it, not merely "if the owner goes quiet" — wall-clock
			// liveness is not a fact partitioned peers can agree on, and a
			// liveness gate could crown two owners on two sides of a partition.
			// So the heir holds a permanent, revocable break-glass, and the UI
			// says exactly that at designation time.
			if !isOwner || o.Target == cur || st.Banned[o.Target] {
				continue // self-heir is meaningless; a banned fpr can't inherit
			}
			st.heir = o.Target
		case "claim_heir":
			// The heir cashes the owner's standing authorization: ownership
			// moves through the SAME cur machinery as transfer_owner, so there
			// is exactly one notion of "current owner" for every later rule.
			// Only a signature from the fingerprint the live designation names
			// counts, and a since-banned heir cannot claim. The designation is
			// consumed — the new owner names their own heir.
			//
			// The ban veto STAYS here, unlike transfer_owner: "named an heir,
			// then banned them" is an in-order revocation by the same owner and
			// blocking the claim is the honest reading of it. The backdating
			// attack that made the veto dangerous on transfer_owner is stopped
			// upstream at ingest (see ingestGovOp), which is where an
			// out-of-order op belongs — not in a rule that would also throw away
			// a legitimate revocation.
			if st.heir == "" || signer != st.heir || st.Banned[signer] {
				continue
			}
			cur = signer
			st.heir = ""
		}
	}
	st.owner = cur
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
