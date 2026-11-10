package app

import (
	"context"
	"testing"
	"time"
)

// TestRoleEscalationIsRefusedOutLoud pins UpsertRole telling the truth: a role
// carrying a permission its maker lacks is refused with an error and no id,
// instead of an id for a role the log then silently declines to create.
func TestRoleEscalationIsRefusedOutLoud(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	admin := startService(t, ctx)
	g, err := owner.CreateGuild("Escalators")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := admin.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, admin)
	role, _ := owner.UpsertRole(g.ID, "", "Admin", "#e91e63", PermManageRoles|PermManageMembers|PermManageChannels, 3)
	_ = owner.AssignRole(g.ID, admin.id.Fingerprint(), role, true)
	waitUntil(t, 20*time.Second, func() bool { return admin.HasPermission(g.ID, PermManageRoles) }, "admin never got Manage roles")

	id, err := admin.UpsertRole(g.ID, "", "SuperBob", "#123456", PermManageMembers|PermSyncHost, 1)
	if err == nil || id != "" {
		t.Fatalf("an escalating role came back as id %q, err %v", id, err)
	}
	// A legitimate one still works, and lands below its maker even when asked
	// for the top of the list (the editor's default for a new role).
	id, err = admin.UpsertRole(g.ID, "", "Builder", "#00aa00", PermManageChannels, 4)
	if err != nil || id == "" {
		t.Fatalf("a role within the maker's powers was refused: %v", err)
	}
	waitUntil(t, 10*time.Second, func() bool {
		for _, r := range owner.Roles(g.ID) {
			if r.ID == id {
				return true
			}
		}
		return false
	}, "the allowed role never existed in the owner's log")
}
