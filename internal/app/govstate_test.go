package app

import (
	"testing"

	"github.com/zahak/concord/internal/identity"
)

// signOp builds a govOp of the given shape signed by id.
func signOp(id *identity.Identity, seq uint64, typ, target string, perms Permission) govOp {
	o := govOp{
		Seq:    seq,
		Signer: id.PublicKey(),
		Type:   typ,
		Target: target,
		Perms:  uint32(perms),
		Time:   int64(seq), // deterministic for tests
	}
	o.Sig = id.Sign(o.signingBytes())
	return o
}

func mustID(t *testing.T) *identity.Identity {
	t.Helper()
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	return id
}

func TestReplayOwnerGrantsAndModeratorBans(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	member := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())
	memberFpr := identity.FingerprintOf(member.PublicKey())

	ops := []govOp{
		// Owner grants the moderator manage-members.
		signOp(owner, 1, "set_perms", modFpr, PermManageMembers),
		// The moderator (now authorized) bans a member.
		signOp(mod, 2, "ban", memberFpr, 0),
	}
	st := replayGuildOps(owner.PublicKey(), ops)

	if !st.Can(ownerFpr, modFpr, PermManageMembers) {
		t.Fatal("moderator should hold manage-members after owner grant")
	}
	if !st.Banned[memberFpr] {
		t.Fatal("member should be banned by the authorized moderator")
	}
	if !st.Can(ownerFpr, ownerFpr, PermManageMembers|PermManageChannels|PermManageGuild) {
		t.Fatal("owner must implicitly hold every permission")
	}
}

func TestReplayModeratorCannotEscalate(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	crony := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())
	cronyFpr := identity.FingerprintOf(crony.PublicKey())

	ops := []govOp{
		signOp(owner, 1, "set_perms", modFpr, PermManageMembers),
		// The moderator tries to grant its crony permissions — must be ignored,
		// since only the owner may assign permissions.
		signOp(mod, 2, "set_perms", cronyFpr, PermManageMembers),
	}
	st := replayGuildOps(owner.PublicKey(), ops)

	if st.Can(ownerFpr, cronyFpr, PermManageMembers) {
		t.Fatal("a moderator must not be able to grant permissions (privilege escalation)")
	}
}

func TestReplayRejectsForgedSignature(t *testing.T) {
	owner := mustID(t)
	attacker := mustID(t)
	victimFpr := identity.FingerprintOf(mustID(t).PublicKey())

	// An op that CLAIMS to be from the owner but is signed by the attacker.
	forged := govOp{
		Seq:    1,
		Signer: owner.PublicKey(), // lies about the author
		Type:   "set_perms",
		Target: victimFpr,
		Perms:  uint32(PermManageMembers),
		Time:   1,
	}
	forged.Sig = attacker.Sign(forged.signingBytes()) // signed by the wrong key

	st := replayGuildOps(owner.PublicKey(), []govOp{forged})
	if len(st.Perms) != 0 {
		t.Fatal("op with a signature that doesn't match its signer key must be rejected")
	}
}

func TestReplayOwnerCannotBeBannedOrDemoted(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())

	ops := []govOp{
		signOp(owner, 1, "set_perms", modFpr, PermManageMembers),
		// The moderator tries to ban the owner.
		signOp(mod, 2, "ban", ownerFpr, 0),
	}
	st := replayGuildOps(owner.PublicKey(), ops)
	if st.Banned[ownerFpr] {
		t.Fatal("the owner must never be bannable")
	}
	if !st.Can(ownerFpr, ownerFpr, PermManageMembers) {
		t.Fatal("owner authority is intrinsic and cannot be revoked")
	}
}

func TestReplayBannedMemberForfeitsPerms(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())

	ops := []govOp{
		signOp(owner, 1, "set_perms", modFpr, PermManageMembers),
		signOp(owner, 2, "ban", modFpr, 0),
	}
	st := replayGuildOps(owner.PublicKey(), ops)
	if st.Can(ownerFpr, modFpr, PermManageMembers) {
		t.Fatal("a banned member must forfeit its permissions")
	}
	if !st.Banned[modFpr] {
		t.Fatal("member should be banned")
	}
}

func TestReplayIsOrderIndependent(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	memberFpr := identity.FingerprintOf(mustID(t).PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())

	grant := signOp(owner, 1, "set_perms", modFpr, PermManageMembers)
	ban := signOp(mod, 2, "ban", memberFpr, 0)

	// Same ops, opposite arrival order → identical folded state (canonical sort
	// by Seq means the grant is always evaluated before the moderator's ban).
	a := replayGuildOps(owner.PublicKey(), []govOp{grant, ban})
	b := replayGuildOps(owner.PublicKey(), []govOp{ban, grant})

	if a.Banned[memberFpr] != b.Banned[memberFpr] || !a.Banned[memberFpr] {
		t.Fatal("replay must be independent of arrival order")
	}
}
