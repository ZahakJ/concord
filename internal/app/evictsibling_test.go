package app

import (
	"context"
	"testing"
	"time"
)

// A commit that removes ANOTHER device of this account is not this device
// being removed. Bob owns a guild and unlinks his phone, which commits the
// removal of the phone's leaf; his tablet must apply that commit and stay a
// member. It used to read the commit as its own removal, refuse it, and tell
// Bob he was no longer in his own guild.
//
// And the other direction still holds: when the account is kicked, each of
// its devices learns it is out.
func TestRemovingOneDeviceLeafDoesNotEvictItsSiblings(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	code, err := w.bob.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	tabletDir := t.TempDir()
	if _, err := RedeemLink(ctx, tabletDir, code, "test-pass", ""); err != nil {
		t.Fatalf("link Bob's tablet: %v", err)
	}
	tablet := startServiceOn(t, ctx, tabletDir, w.boot)
	rt := &recorder{}
	tablet.OnMessage(rt.add)

	own := w.guildOf(t, w.bob, w.phone, tablet, w.alice)
	if !delivered(w.alice, own.Channels[0].ID, "before", 60*time.Second, rt, w.rb) {
		t.Fatal("setup: Alice's message did not reach Bob's devices")
	}

	_ = w.phone.Close()
	if err := w.bob.UnlinkDevice(w.phoneKey); err != nil {
		t.Fatalf("UnlinkDevice in a guild Bob owns: %v", err)
	}
	waitMemberCount(t, 60*time.Second, own.ID, 3, w.bob, w.alice)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && memberCount(tablet, own.ID) != 3 && tablet.EvictedFrom(own.ID) == "" {
		time.Sleep(200 * time.Millisecond)
	}
	if why, n := tablet.EvictedFrom(own.ID), memberCount(tablet, own.ID); why != "" || n != 3 {
		t.Fatalf("Bob's tablet after his phone's leaf was removed: evicted=%q leaves=%d; want still a member with 3", why, n)
	}
	if !delivered(w.alice, own.Channels[0].ID, "after", 60*time.Second, rt, w.rb) {
		t.Error("after the phone's leaf was removed, Alice's message did not reach Bob's tablet and desktop")
	}

	// Alice kicks Bob from her guild: both of his devices are out, and know.
	hers := w.guildOf(t, w.alice, w.bob, tablet)
	if err := w.alice.KickMember(hers.ID, w.bob.Fingerprint(), ""); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return w.bob.EvictedFrom(hers.ID) == evictedKicked && tablet.EvictedFrom(hers.ID) == evictedKicked
	}, "a kicked account's devices did not both learn they were removed")
}
