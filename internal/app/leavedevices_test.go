package app

import (
	"context"
	"testing"
	"time"
)

// Leaving a guild is the account's decision. A person who left on one device
// used to find the other still in the guild, still posting as them.
//
// Live: the phone leaves and the desk follows. Offline: the desk leaves while
// the phone is away, and the phone follows on its next hello. And a deliberate
// rejoin on one device brings the other back instead of being vetoed by the
// leave it followed.
func TestLeavingAGuildLeavesItOnEveryDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	phoneDir := t.TempDir()
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), phoneDir, boot)

	cy := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := cy.SetDisplayName("Cy"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	join := func(name string) (string, string) {
		g, err := cy.CreateGuild(name)
		if err != nil {
			t.Fatalf("CreateGuild: %v", err)
		}
		code, err := cy.InviteCode(g.ID)
		if err != nil {
			t.Fatalf("InviteCode: %v", err)
		}
		if _, err := desk.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
		waitUntil(t, 60*time.Second, func() bool { return holdsGuild(phone, g.ID) },
			"the phone never got "+name)
		return g.ID, code
	}

	live, _ := join("Live")
	if err := phone.LeaveGuild(live); err != nil {
		t.Fatalf("LeaveGuild: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool { return !holdsGuild(desk, live) },
		"the desk stayed in a guild the account left on the phone")

	away, code := join("Away")
	_ = phone.Close()
	// Leaves are stamped in whole seconds, and an arrival in the same second
	// as a leave cannot be told from a rejoin; nobody leaves that fast.
	time.Sleep(1100 * time.Millisecond)
	if err := desk.LeaveGuild(away); err != nil {
		t.Fatalf("LeaveGuild: %v", err)
	}
	phone = startServiceOn(t, ctx, phoneDir, boot)
	waitUntil(t, 60*time.Second, func() bool { return !holdsGuild(phone, away) },
		"the phone kept a guild the account left while it was offline")

	// Past the second the phone's own leave was stamped in, for the same reason.
	time.Sleep(2100 * time.Millisecond)
	if _, err := desk.JoinViaInvite(code); err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool { return holdsGuild(phone, away) },
		"the phone stayed out of a guild the account deliberately rejoined")
}
