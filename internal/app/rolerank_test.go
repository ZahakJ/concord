package app

import (
	"context"
	"testing"
	"time"
)

// TestModerationRespectsRankAndKickStripsRoles pins two issuer-side rules. A
// moderator cannot mute, kick or ban someone whose top role is equal or above
// theirs (the menus offered it, and the guild carried it out). And a kick takes
// the member's roles with it, so a kicked admin who comes back through an
// invite is a member, not an admin again on arrival.
func TestModerationRespectsRankAndKickStripsRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	admin := startService(t, ctx)
	mod := startService(t, ctx)

	g, err := owner.CreateGuild("Ranked Hall")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	for _, s := range []*Service{admin, mod} {
		if _, err := s.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitMembers(t, 30*time.Second, 3, owner, admin, mod)
	adminFpr, modFpr := admin.id.Fingerprint(), mod.id.Fingerprint()
	adminRole, _ := owner.UpsertRole(g.ID, "", "Admin", "#e91e63", PermManageMembers|PermManageRoles|PermMuteMembers, 5)
	modRole, _ := owner.UpsertRole(g.ID, "", "Moderator", "#5865f2", PermManageMembers|PermMuteMembers, 1)
	_ = owner.AssignRole(g.ID, adminFpr, adminRole, true)
	_ = owner.AssignRole(g.ID, modFpr, modRole, true)
	for _, s := range []*Service{admin, mod} {
		waitUntil(t, 20*time.Second, func() bool {
			return len(s.MemberRoleIDs(g.ID, adminFpr)) == 1 && len(s.MemberRoleIDs(g.ID, modFpr)) == 1
		}, "the roles never reached every member")
	}

	if err := mod.MuteMember(g.ID, adminFpr, 5, ""); err == nil {
		t.Fatal("a moderator muted an admin")
	}
	if err := mod.KickMember(g.ID, adminFpr, ""); err == nil {
		t.Fatal("a moderator kicked an admin")
	}
	if err := mod.BanMember(g.ID, adminFpr, ""); err == nil {
		t.Fatal("a moderator banned an admin")
	}
	if mod.Outranks(g.ID, modFpr, adminFpr) || !admin.Outranks(g.ID, adminFpr, modFpr) {
		t.Fatal("Outranks disagrees with the role positions")
	}

	if err := admin.KickMember(g.ID, modFpr, ""); err != nil {
		t.Fatalf("an admin could not kick a moderator: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool { return len(owner.MemberRoleIDs(g.ID, modFpr)) == 0 },
		"the kicked moderator still holds their role in the owner's log")
}
