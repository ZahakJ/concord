package app

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/zahak/concord/internal/identity"
)

// govstate.go is the pure (I/O-free) core of guild governance: the permission
// model, the signed-operation format, and the deterministic replay that folds a
// log of ops into a GuildState. Keeping it pure makes the security-critical
// rules — who may grant permissions, who may ban — exhaustively unit-testable
// without spinning up networks or MLS groups.
//
// Trust anchor: the guild owner (guild.OwnerID). The owner implicitly holds
// every permission and is the only identity that may grant permissions to
// others (so a moderator cannot escalate itself or a colluder). Everything else
// derives from owner-signed ops, replayed in a canonical order.

// Permission is a capability bitmask assigned to a member.
type Permission uint32

const (
	// PermManageMembers lets a member invite, kick, ban, and unban others. This
	// is the capability the MLS committer gate consults: a holder's membership
	// commits are accepted by honest peers (see authorizedCommitter).
	PermManageMembers Permission = 1 << iota
	// PermManageChannels lets a member create/edit/delete channels & categories.
	PermManageChannels
	// PermManageGuild lets a member rename the guild and manage custom emoji.
	PermManageGuild
)

// Has reports whether the bitmask includes every bit of p.
func (perm Permission) Has(p Permission) bool { return perm&p == p }

// govOp is one signed governance mutation. Ops form a per-guild log; replaying
// them in canonical order yields the GuildState. The signature covers every
// field except Sig itself, binding the op to its author's account key.
type govOp struct {
	Seq    uint64 `json:"seq"`    // monotonic order hint (owner/moderators bump it)
	Signer []byte `json:"signer"` // author's Ed25519 account public key
	Type   string `json:"type"`   // "set_perms" | "ban" | "unban"
	Target string `json:"target"` // fingerprint the op acts on
	Perms  uint32 `json:"perms"`  // new bitmask for "set_perms"
	Time   int64  `json:"t"`      // author's wall-clock (unix nanos), tiebreaker
	Sig    []byte `json:"sig"`    // signature over the op with Sig zeroed
}

// signingBytes is the canonical serialization the signature covers: the op with
// its Sig field cleared, JSON-encoded (Go sorts struct fields by declaration, so
// this is deterministic for a fixed struct).
func (o govOp) signingBytes() []byte {
	o.Sig = nil
	b, _ := json.Marshal(o)
	return b
}

func (o govOp) hash() string {
	sum := sha256.Sum256(o.signingBytes())
	return hex.EncodeToString(sum[:])
}

// verifySig checks the op's signature against its embedded signer key.
func (o govOp) verifySig() bool {
	if len(o.Signer) != ed25519.PublicKeySize || len(o.Sig) != ed25519.SignatureSize {
		return false
	}
	return identity.Verify(o.Signer, o.signingBytes(), o.Sig)
}

func (o govOp) signerFpr() string { return identity.FingerprintOf(o.Signer) }

// GuildState is the folded result of replaying a guild's governance log.
type GuildState struct {
	// Perms maps a member fingerprint to its granted bitmask. The owner is not
	// listed (it implicitly has everything); see Can.
	Perms map[string]Permission
	// Banned holds fingerprints barred from the guild; they are refused at the
	// invite gate and removed if present.
	Banned map[string]bool
}

func newGuildState() GuildState {
	return GuildState{Perms: map[string]Permission{}, Banned: map[string]bool{}}
}

// Can reports whether the member with fingerprint fpr holds permission p in this
// state. ownerFpr always passes (the owner holds every capability).
func (st GuildState) Can(ownerFpr, fpr string, p Permission) bool {
	if fpr == ownerFpr {
		return true
	}
	return st.Perms[fpr].Has(p)
}

// canonicalOps sorts a log into the deterministic replay order every peer must
// agree on: by Seq, then Time, then op hash. Sorting (rather than trusting
// arrival order) means two peers that received ops in different orders still
// fold to the identical GuildState.
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
// that the op's signer is currently authorized to make that change:
//   - set_perms: owner only (prevents privilege escalation by moderators).
//   - ban / unban: owner or a current PermManageMembers holder.
//
// Authorization is evaluated against the state accumulated from all earlier ops
// in canonical order, so a moderator granted power by an early owner op can act
// in later ops. Invalidly-signed or unauthorized ops are skipped, not fatal —
// the log is gossiped and a peer may see a stray or malicious op.
func replayGuildOps(owner []byte, ops []govOp) GuildState {
	ownerFpr := identity.FingerprintOf(owner)
	st := newGuildState()
	for _, o := range canonicalOps(ops) {
		if !o.verifySig() {
			continue
		}
		signer := o.signerFpr()
		switch o.Type {
		case "set_perms":
			if signer != ownerFpr {
				continue // only the owner assigns permissions
			}
			if o.Target == ownerFpr {
				continue // the owner's implicit full authority isn't editable
			}
			if o.Perms == 0 {
				delete(st.Perms, o.Target)
			} else {
				st.Perms[o.Target] = Permission(o.Perms)
			}
		case "ban":
			if !st.Can(ownerFpr, signer, PermManageMembers) {
				continue
			}
			if o.Target == ownerFpr {
				continue // the owner cannot be banned
			}
			st.Banned[o.Target] = true
			delete(st.Perms, o.Target) // a banned member forfeits any permissions
		case "unban":
			if !st.Can(ownerFpr, signer, PermManageMembers) {
				continue
			}
			delete(st.Banned, o.Target)
		}
	}
	return st
}
