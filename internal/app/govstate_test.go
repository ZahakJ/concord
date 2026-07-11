package app

import (
	"testing"

	"github.com/zahak/concord/internal/identity"
)

// upsertRole builds a signed role_upsert op.
func upsertRole(id *identity.Identity, seq uint64, roleID, name string, perms Permission, pos int) govOp {
	o := govOp{
		Seq: seq, Signer: id.PublicKey(), Type: "role_upsert",
		RoleID: roleID, Name: name, Perms: uint32(perms), Position: pos, Time: int64(seq),
	}
	o.Sig = id.Sign(o.signingBytes())
	return o
}

func assignRole(id *identity.Identity, seq uint64, target, roleID string, add bool) govOp {
	o := govOp{
		Seq: seq, Signer: id.PublicKey(), Type: "role_assign",
		RoleID: roleID, Target: target, Add: add, Time: int64(seq),
	}
	o.Sig = id.Sign(o.signingBytes())
	return o
}

func banOp(id *identity.Identity, seq uint64, typ, target string) govOp {
	o := govOp{Seq: seq, Signer: id.PublicKey(), Type: typ, Target: target, Time: int64(seq)}
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

func TestRolesOwnerGrantsAndMemberActs(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	member := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())
	memberFpr := identity.FingerprintOf(member.PublicKey())

	ops := []govOp{
		// Owner defines a "Mod" role with manage-members and assigns it.
		upsertRole(owner, 1, "r_mod", "Mod", PermManageMembers, 10),
		assignRole(owner, 2, modFpr, "r_mod", true),
		// The moderator (now authorized) bans a member.
		banOp(mod, 3, "ban", memberFpr),
	}
	st := replayGuildOps(owner.PublicKey(), ops)

	if !st.Can(ownerFpr, modFpr, PermManageMembers) {
		t.Fatal("moderator should hold manage-members from its role")
	}
	if !st.Banned[memberFpr] {
		t.Fatal("member should be banned by the authorized moderator")
	}
	if !st.Can(ownerFpr, ownerFpr, permAll) {
		t.Fatal("owner must implicitly hold every permission")
	}
}

func TestRolesCannotEscalatePastSelf(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	modFpr := identity.FingerprintOf(mod.PublicKey())

	ops := []govOp{
		// A role that can manage roles but nothing else, at position 10.
		upsertRole(owner, 1, "r_roles", "RoleMgr", PermManageRoles, 10),
		assignRole(owner, 2, modFpr, "r_roles", true),
		// The role-manager tries to mint a role MORE powerful than itself
		// (adds ManageMembers, which it doesn't have) — must be refused.
		upsertRole(mod, 3, "r_evil", "Evil", PermManageRoles|PermManageMembers, 5),
		// ...and tries to create a role at/above its own rank (position 10) —
		// must be refused.
		upsertRole(mod, 4, "r_high", "High", PermManageRoles, 10),
	}
	st := replayGuildOps(owner.PublicKey(), ops)

	if _, ok := st.Roles["r_evil"]; ok {
		t.Fatal("a role-manager must not mint a role with perms it lacks")
	}
	if _, ok := st.Roles["r_high"]; ok {
		t.Fatal("a role-manager must not create a role at/above its own rank")
	}
}

func TestRolesCannotAssignAboveRank(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	crony := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())
	cronyFpr := identity.FingerprintOf(crony.PublicKey())

	ops := []govOp{
		upsertRole(owner, 1, "r_admin", "Admin", PermManageRoles|PermManageMembers, 20), // senior
		upsertRole(owner, 2, "r_roles", "RoleMgr", PermManageRoles, 10),                 // junior
		assignRole(owner, 3, modFpr, "r_roles", true),
		// The junior role-manager tries to grant its crony the SENIOR admin role
		// (position 20 >= its own rank 10) — must be refused.
		assignRole(mod, 4, cronyFpr, "r_admin", true),
	}
	st := replayGuildOps(owner.PublicKey(), ops)

	if st.Can(ownerFpr, cronyFpr, PermManageMembers) {
		t.Fatal("a junior must not assign a role that outranks it (privilege escalation)")
	}
}

func TestRolesRejectForgedSignature(t *testing.T) {
	owner := mustID(t)
	attacker := mustID(t)

	forged := govOp{
		Seq: 1, Signer: owner.PublicKey(), Type: "role_upsert",
		RoleID: "r", Name: "x", Perms: uint32(PermManageMembers), Position: 5, Time: 1,
	}
	forged.Sig = attacker.Sign(forged.signingBytes()) // signed by the wrong key

	st := replayGuildOps(owner.PublicKey(), []govOp{forged})
	if len(st.Roles) != 0 {
		t.Fatal("op whose signature doesn't match its signer key must be rejected")
	}
}

func TestRolesOwnerImmune(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())

	ops := []govOp{
		upsertRole(owner, 1, "r_mod", "Mod", PermManageMembers|PermManageRoles, 10),
		assignRole(owner, 2, modFpr, "r_mod", true),
		banOp(mod, 3, "ban", ownerFpr),              // can't ban the owner
		assignRole(mod, 4, ownerFpr, "r_mod", true), // can't assign roles to the owner
	}
	st := replayGuildOps(owner.PublicKey(), ops)
	if st.Banned[ownerFpr] {
		t.Fatal("the owner must never be bannable")
	}
	if len(st.MemberRoles[ownerFpr]) != 0 {
		t.Fatal("roles must not be assignable to the owner")
	}
	if !st.Can(ownerFpr, ownerFpr, permAll) {
		t.Fatal("owner authority is intrinsic")
	}
}

func TestRolesBannedMemberForfeitsRoles(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())

	ops := []govOp{
		upsertRole(owner, 1, "r_mod", "Mod", PermManageMembers, 10),
		assignRole(owner, 2, modFpr, "r_mod", true),
		banOp(owner, 3, "ban", modFpr),
	}
	st := replayGuildOps(owner.PublicKey(), ops)
	if st.Can(ownerFpr, modFpr, PermManageMembers) {
		t.Fatal("a banned member must forfeit its roles/permissions")
	}
}

func TestRolesMute(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	member := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())
	memberFpr := identity.FingerprintOf(member.PublicKey())
	rando := mustID(t)

	muteOp := func(id *identity.Identity, seq uint64, typ, target string, until int64) govOp {
		o := govOp{Seq: seq, Signer: id.PublicKey(), Type: typ, Target: target, Until: until, Time: int64(seq)}
		o.Sig = id.Sign(o.signingBytes())
		return o
	}

	ops := []govOp{
		upsertRole(owner, 1, "r_mute", "Muter", PermMuteMembers, 10),
		assignRole(owner, 2, modFpr, "r_mute", true),
		muteOp(mod, 3, "mute", memberFpr, 9999999999), // authorized mute
		muteOp(rando, 4, "mute", modFpr, 9999999999),  // unauthorized — no perms
		muteOp(mod, 5, "mute", ownerFpr, 9999999999),  // can't mute the owner
	}
	st := replayGuildOps(owner.PublicKey(), ops)

	if st.Muted[memberFpr] == 0 {
		t.Fatal("authorized moderator should be able to mute a member")
	}
	if st.Muted[modFpr] != 0 {
		t.Fatal("a member without mute-members must not be able to mute")
	}
	if st.Muted[ownerFpr] != 0 {
		t.Fatal("the owner must not be mutable")
	}
	// Unmute lifts it.
	ops = append(ops, muteOp(mod, 6, "unmute", memberFpr, 0))
	st = replayGuildOps(owner.PublicKey(), ops)
	if st.Muted[memberFpr] != 0 {
		t.Fatal("unmute should lift the mute")
	}
}

func TestRolesReplayOrderIndependent(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	memberFpr := identity.FingerprintOf(mustID(t).PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())

	def := upsertRole(owner, 1, "r_mod", "Mod", PermManageMembers, 10)
	assign := assignRole(owner, 2, modFpr, "r_mod", true)
	ban := banOp(mod, 3, "ban", memberFpr)

	a := replayGuildOps(owner.PublicKey(), []govOp{def, assign, ban})
	b := replayGuildOps(owner.PublicKey(), []govOp{ban, assign, def})
	if a.Banned[memberFpr] != b.Banned[memberFpr] || !a.Banned[memberFpr] {
		t.Fatal("replay must be independent of arrival order")
	}
}
