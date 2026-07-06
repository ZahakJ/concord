package app

import (
	"context"
	"testing"
	"time"
)

// TestNicknamePropagation covers per-guild nicknames: A and B share a guild, A
// sets its own server nickname, and B converges on that override for A while A's
// global profile name is unchanged. Clearing the nickname reverts it everywhere.
func TestNicknamePropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)

	// Give A a global profile name so we can prove the nickname shadows it.
	if err := a.SetProfile(Profile{Name: "euclid"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	// A sets a per-guild nickname for itself.
	if err := a.SetNickname(g.ID, "the_professor"); err != nil {
		t.Fatalf("SetNickname: %v", err)
	}
	if got := a.NickOf(g.ID, a.Fingerprint()); got != "the_professor" {
		t.Fatalf("A local nick = %q, want the_professor", got)
	}

	// B learns the nickname over the guild-meta topic.
	waitUntil(t, 20*time.Second, func() bool {
		return b.NickOf(g.ID, a.Fingerprint()) == "the_professor"
	}, "B never learned A's nickname")

	// The global profile name is untouched — nickname is guild-scoped only.
	if p := b.ProfileOf(a.Fingerprint()); p.Name != "euclid" {
		t.Fatalf("A profile name at B = %q, want euclid (nickname must not overwrite it)", p.Name)
	}

	// Clearing the nickname reverts B back to the profile name.
	if err := a.SetNickname(g.ID, ""); err != nil {
		t.Fatalf("SetNickname clear: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return b.NickOf(g.ID, a.Fingerprint()) == ""
	}, "B never saw A's nickname cleared")
}
