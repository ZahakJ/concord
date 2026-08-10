package app

import (
	"context"
	"testing"
	"time"
)

// TestChannelRenamePropagates: a rename by someone holding ManageChannels
// reaches every member; a rename pushed by someone WITHOUT it is refused on the
// receive side, because the receive half must gate exactly as the local half
// does or the permission is decorative.
func TestChannelRenamePropagates(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	member := startServiceOn(t, ctx, t.TempDir(), boot)

	g, err := owner.CreateGuild("Rename")
	if err != nil {
		t.Fatal(err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("join: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 2
	}, "the member never joined")

	ch := g.Channels[0]
	if err := owner.RenameChannel(g.ID, ch.ID, "renamed-channel"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		for _, mg := range member.Guilds() {
			if mg.ID != g.ID {
				continue
			}
			for _, c := range mg.Channels {
				if c.ID == ch.ID && c.Name == "renamed-channel" {
					return true
				}
			}
		}
		return false
	}, "the rename never reached the member")

	// The member holds no ManageChannels: the local call must refuse…
	if err := member.RenameChannel(g.ID, ch.ID, "hijacked"); err == nil {
		t.Fatal("a member without ManageChannels renamed a channel locally")
	}
	// …and a forged meta application must be refused by the receive gate.
	owner.applyChannelRename(g.ID, member.id.Fingerprint(), ch.ID, "forged")
	for _, og := range owner.Guilds() {
		if og.ID != g.ID {
			continue
		}
		for _, c := range og.Channels {
			if c.ID == ch.ID && c.Name != "renamed-channel" {
				t.Fatalf("a forged rename from a non-privileged member was applied: %q", c.Name)
			}
		}
	}
}
