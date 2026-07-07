package app

import (
	"context"
	"testing"
	"time"
)

// TestOutOfSyncAutoHeal verifies the automatic recovery path: a member flagged
// out-of-sync gets re-added by an online authorized committer (the owner) with
// no manual leave/rejoin, the flag clears, and the member can still exchange
// end-to-end-encrypted messages afterward.
func TestOutOfSyncAutoHeal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	member := startService(t, ctx)

	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("member join: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, owner, member)

	ra, rm := &recorder{}, &recorder{}
	owner.OnMessage(ra.add)
	member.OnMessage(rm.add)

	// Force the stranded state; setOutOfSync kicks off an immediate heal attempt
	// against the online owner (an authorized committer).
	member.setOutOfSync(g.ID, true)
	if !member.OutOfSync(g.ID) {
		t.Fatal("member should be flagged out-of-sync")
	}

	// The re-add should clear the flag automatically.
	waitUntil(t, 25*time.Second, func() bool {
		return !member.OutOfSync(g.ID)
	}, "out-of-sync never auto-healed")

	// Both are still members and the ratchet works: owner sends, member reads.
	waitMembers(t, 20*time.Second, 2, owner, member)
	sendUntilReceived(t, owner, channel, "welcome back", rm)
}
