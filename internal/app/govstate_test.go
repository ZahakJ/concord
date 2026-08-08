package app

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
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

// TestOwnerSelfAssignsRole: the owner may give a role to THEMSELVES (that's
// how they take the Admin badge — it grants nothing they don't already hold),
// while a moderator still cannot decorate the owner. Regression: the blanket
// "no roles on the owner" rule silently dropped the owner's own op, so
// "Make admin" appeared to do nothing.
func TestOwnerSelfAssignsRole(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())

	ops := []govOp{
		upsertRole(owner, 1, "r_admin", "Admin", permAll, 100),
		upsertRole(owner, 2, "r_mod", "Mod", PermManageRoles, 10),
		assignRole(owner, 3, modFpr, "r_mod", true),
		assignRole(owner, 4, ownerFpr, "r_admin", true), // owner → self: allowed
		assignRole(mod, 5, ownerFpr, "r_mod", true),     // mod → owner: refused
	}
	st := replayGuildOps(owner.PublicKey(), ops)
	if !containsStr(st.MemberRoles[ownerFpr], "r_admin") {
		t.Fatal("the owner must be able to assign a role to themselves")
	}
	if containsStr(st.MemberRoles[ownerFpr], "r_mod") {
		t.Fatal("a moderator must not be able to assign roles to the owner")
	}
}

// TestModeratorCannotSelfPromote is the escalation gate, stated as a test: a
// member holding ManageRoles cannot make themselves an admin — not by minting
// an all-powerful role (capped at their own permissions), and not by granting
// themselves an existing role that outranks them.
func TestModeratorCannotSelfPromote(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	modFpr := identity.FingerprintOf(mod.PublicKey())

	ops := []govOp{
		// An admin role the owner created, ranked above the moderator.
		upsertRole(owner, 1, "r_admin", "Admin", permAll, 100),
		upsertRole(owner, 2, "r_mod", "Mod", PermManageRoles, 10),
		assignRole(owner, 3, modFpr, "r_mod", true),

		// The moderator tries every escalation path: mint an all-powerful role…
		upsertRole(mod, 4, "r_evil", "Evil", permAll, 5),
		assignRole(mod, 5, modFpr, "r_evil", true),
		// …and grab the existing admin role that outranks them.
		assignRole(mod, 6, modFpr, "r_admin", true),
	}
	st := replayGuildOps(owner.PublicKey(), ops)

	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	if st.Can(ownerFpr, modFpr, PermManageGuild) {
		t.Fatal("a moderator escalated to permissions they never held")
	}
	if containsStr(st.MemberRoles[modFpr], "r_admin") {
		t.Fatal("a moderator assigned themselves a role above their rank")
	}
	if _, minted := st.Roles["r_evil"]; minted {
		t.Fatal("a moderator minted a role more powerful than themselves")
	}
}

// TestNicknameAuthority pins who may rename whom. The rule matters because the
// nickname payload NAMES ITS TARGET: without a check on the (MLS-authenticated)
// sender, any member could rename anyone on everyone else's screen. Renaming
// someone else needs MANAGE_MEMBERS and outranking them; the owner is
// untouchable; renaming yourself is always fine.
func TestNicknameAuthority(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	member := mustID(t)
	ownerFpr := identity.FingerprintOf(owner.PublicKey())
	modFpr := identity.FingerprintOf(mod.PublicKey())
	memberFpr := identity.FingerprintOf(member.PublicKey())

	st := replayGuildOps(owner.PublicKey(), []govOp{
		upsertRole(owner, 1, "r_mod", "Mod", PermManageMembers, 10),
		assignRole(owner, 2, modFpr, "r_mod", true),
	})

	// allowed reproduces Service.nickAllowed against a replayed state.
	allowed := func(actor, target string) bool {
		if actor == target {
			return true
		}
		if !st.Can(ownerFpr, actor, PermManageMembers) {
			return false
		}
		if target == ownerFpr {
			return false
		}
		return actor == ownerFpr || st.topPosition(ownerFpr, actor) > st.topPosition(ownerFpr, target)
	}

	cases := []struct {
		name          string
		actor, target string
		want          bool
	}{
		{"a member renames themselves", memberFpr, memberFpr, true},
		{"a mod renames a member", modFpr, memberFpr, true},
		{"the owner renames a mod", ownerFpr, modFpr, true},
		{"a plain member renames someone else", memberFpr, modFpr, false},
		{"a mod renames the owner", modFpr, ownerFpr, false},
		{"a mod renames another mod (equal rank)", modFpr, modFpr, true}, // self
	}
	for _, c := range cases {
		if got := allowed(c.actor, c.target); got != c.want {
			t.Errorf("%s: allowed=%v, want %v", c.name, got, c.want)
		}
	}

	// Two mods of equal rank: neither may rename the other.
	mod2 := mustID(t)
	mod2Fpr := identity.FingerprintOf(mod2.PublicKey())
	st = replayGuildOps(owner.PublicKey(), []govOp{
		upsertRole(owner, 1, "r_mod", "Mod", PermManageMembers, 10),
		assignRole(owner, 2, modFpr, "r_mod", true),
		assignRole(owner, 3, mod2Fpr, "r_mod", true),
	})
	if allowed(modFpr, mod2Fpr) {
		t.Error("a moderator must not rename a moderator of equal rank")
	}
}

func TestSlowModeReplay(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	rando := mustID(t)
	modFpr := identity.FingerprintOf(mod.PublicKey())

	slowOp := func(id *identity.Identity, seq uint64, ch string, secs int64) govOp {
		o := govOp{Seq: seq, Signer: id.PublicKey(), Type: "slow_mode", ChannelID: ch, Seconds: secs, Time: int64(seq)}
		o.Sig = id.Sign(o.signingBytes())
		return o
	}

	ops := []govOp{
		upsertRole(owner, 1, "r_chan", "Channeler", PermManageChannels, 10),
		assignRole(owner, 2, modFpr, "r_chan", true),
		slowOp(mod, 3, "ch1", 30),      // authorized
		slowOp(rando, 4, "ch2", 30),    // unauthorized — no perms
		slowOp(owner, 5, "ch3", 99999), // over the ceiling: clamped in replay
		slowOp(owner, 6, "", 30),       // no channel: inert
	}
	st := replayGuildOps(owner.PublicKey(), ops)

	if st.SlowMode["ch1"] != 30 {
		t.Fatal("authorized manage-channels holder should set slow mode")
	}
	if st.SlowMode["ch2"] != 0 {
		t.Fatal("a member without manage-channels must not set slow mode")
	}
	if st.SlowMode["ch3"] != 21600 {
		t.Fatalf("over-ceiling seconds must clamp in replay, got %d", st.SlowMode["ch3"])
	}
	// Zero turns it off.
	ops = append(ops, slowOp(mod, 7, "ch1", 0))
	st = replayGuildOps(owner.PublicKey(), ops)
	if st.SlowMode["ch1"] != 0 {
		t.Fatal("seconds<=0 should clear slow mode")
	}
}

// A dethroned owner must not be able to take a guild back by BACKDATING an op:
// Seq is a signed-but-self-chosen field, so a forged low Seq would otherwise
// fold into replay at a position where the signer still held the crown.
// Two shapes were exploitable; both are covered here at the replay level.
func TestDethronedOwnerCannotRetakeGuild(t *testing.T) {
	alice := mustID(t) // founding owner, later transfers away
	bob := mustID(t)   // the new owner
	bobFpr := identity.FingerprintOf(bob.PublicKey())

	op := func(id *identity.Identity, seq uint64, typ, target string) govOp {
		o := govOp{Seq: seq, Signer: id.PublicKey(), Type: typ, Target: target, Time: int64(seq)}
		o.Sig = id.Sign(o.signingBytes())
		return o
	}

	// The honest history: Alice hands the guild to Bob at Seq 5.
	honest := []govOp{op(alice, 5, "transfer_owner", bobFpr)}
	if got := replayGuildOps(alice.PublicKey(), honest).Owner(); got != bobFpr {
		t.Fatalf("setup: transfer should make Bob owner, got %q", got)
	}

	// SHAPE 1 — backdated ban. Alice signs "ban Bob" at Seq 1, which sorts
	// before her own transfer. A ban must not void the handover.
	withBan := append(append([]govOp(nil), honest...), op(alice, 1, "ban", bobFpr))
	st := replayGuildOps(alice.PublicKey(), withBan)
	if st.Owner() != bobFpr {
		t.Fatalf("a backdated ban must not undo the transfer: owner is %q, want Bob", st.Owner())
	}
	if st.Banned[bobFpr] {
		t.Fatal("being made owner must clear a ban on the new owner")
	}

	// SHAPE 2 — backdated succession (set_heir + claim_heir before the
	// transfer) is NOT stoppable at replay: a set_heir at Seq 1 is
	// indistinguishable from one Alice genuinely issued while she was owner.
	// Its defence is the ingest guard, asserted in the next test. Recording
	// the limit here so nobody "fixes" replay and assumes it covers this.
}

// The ingest guard is what stops a backdated op reaching the log at all: a
// signer's own ops only move forward, so one arriving LIVE at or below a Seq
// we already hold from that signer is forged by construction. This is the
// defence that covers the succession shape replay cannot see.
func TestBackdatedGovOpRefusedOnLivePath(t *testing.T) {
	alice := mustID(t)
	victim := mustID(t)
	victimFpr := identity.FingerprintOf(victim.PublicKey())

	st, err := store.Open(filepath.Join(t.TempDir(), "concord.db"), bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := &Service{
		store:     st,
		govOps:    map[string][]govOp{},
		govState:  map[string]GuildState{},
		govHashes: map[string]map[string]bool{},
		guilds:    map[string]*domain.Guild{},
	}

	sign := func(seq uint64, typ, target string) govOp {
		o := govOp{Seq: seq, Signer: alice.PublicKey(), Type: typ, Target: target, Time: int64(seq)}
		o.Sig = alice.Sign(o.signingBytes())
		return o
	}

	// Alice's genuine op lands at Seq 5.
	if !svc.ingestGovOp("g1", sign(5, "ban", victimFpr), true) {
		t.Fatal("a fresh op from Alice should be accepted")
	}
	// Now she tries to slip one in behind it.
	if svc.ingestGovOp("g1", sign(1, "unban", victimFpr), true) {
		t.Fatal("a backdated op arriving live must be refused")
	}
	// The same op is still accepted off the SYNC path, where a peer
	// legitimately replays a signer's older ops out of order.
	if !svc.ingestGovOp("g1", sign(1, "unban", victimFpr), false) {
		t.Fatal("sync backfill must still accept older ops, or logs fork")
	}
}
