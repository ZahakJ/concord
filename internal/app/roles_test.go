package app

import (
	"context"
	"testing"
	"time"
)

// TestModeratorCanInviteAndKick is the Phase 3 acceptance test for not being
// load-bearing as owner: the owner grants a member the manage-members
// permission, and that member (a moderator) can then invite a new member and
// kick one — with honest peers accepting the moderator's commits because the
// governance gate recognizes the granted authority.
func TestModeratorCanInviteAndKick(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	mod := startService(t, ctx)
	newcomer := startService(t, ctx)

	rMod, rNew := &recorder{}, &recorder{}
	mod.OnMessage(rMod.add)
	newcomer.OnMessage(rNew.add)

	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, _ := owner.InviteCode(g.ID)
	if _, err := mod.JoinViaInvite(code); err != nil {
		t.Fatalf("mod join: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, owner, mod)

	// Owner defines a Mod role with manage-members and assigns it.
	modFpr := mod.Fingerprint()
	roleID, err := owner.UpsertRole(g.ID, "", "Mod", "#5865f2", PermManageMembers, 10)
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := owner.AssignRole(g.ID, modFpr, roleID, true); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	// The grant must reach the moderator over gossip before it acts.
	waitUntil(t, 20*time.Second, func() bool {
		return mod.canManageMembers(g.ID)
	}, "moderator never learned it was granted manage-members")

	// The MODERATOR (not the owner) issues an invite code and adds the newcomer.
	modCode, err := mod.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("mod InviteCode: %v", err)
	}
	if _, err := newcomer.JoinViaInvite(modCode); err != nil {
		t.Fatalf("newcomer join via moderator: %v", err)
	}

	// All three must converge — proving the owner accepted the moderator's Add
	// commit off the control topic (the whole point: no owner action required).
	waitMembers(t, 30*time.Second, 3, owner, mod, newcomer)
	sendUntilReceived(t, mod, channel, "welcome, added by a mod", rNew)

	// The moderator can also kick. It removes the newcomer.
	if err := mod.RemoveMember(g.ID, newcomer.PublicKey()); err != nil {
		t.Fatalf("moderator RemoveMember: %v", err)
	}
	waitUntil(t, 25*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 2
	}, "owner never applied the moderator's removal commit")
}

// TestBanSurvivesRejoin verifies a banned member is evicted and cannot rejoin
// even with a fresh invite code — the ban lives in signed governance state and
// is enforced at the invite gate.
func TestBanSurvivesRejoin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	troublemaker := startService(t, ctx)

	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := troublemaker.JoinViaInvite(code); err != nil {
		t.Fatalf("join: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, owner, troublemaker)

	// Owner bans the troublemaker — they're evicted from the group now...
	if err := owner.BanMember(g.ID, troublemaker.Fingerprint()); err != nil {
		t.Fatalf("BanMember: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 1
	}, "banned member was not evicted from the group")

	// ...and a fresh invite code cannot get them back in.
	code2, _ := owner.InviteCode(g.ID)
	if _, err := troublemaker.JoinViaInvite(code2); err == nil {
		t.Fatal("banned member rejoined with a fresh invite — ban did not survive")
	}
	if n, _ := owner.MemberCount(g.ID); n != 1 {
		t.Fatalf("owner has %d members after rejoin attempt, want 1", n)
	}
}
