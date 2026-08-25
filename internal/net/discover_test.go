package net

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
)

// TestIdleDiscoveryBacksOff is the battery regression.
//
// discoverLoop ran a flat 15 seconds forever: an idle node — everyone it knows
// already connected, or no network at all — paid a full Kademlia walk, up to
// addrlessLookups more walks and an unbounded fan of dials, four times a
// minute, all night. This asserts the schedule an idle node actually settles
// on. Written against the pure function so the whole schedule is checked in
// microseconds rather than in the twenty minutes of wall clock it describes.
func TestIdleDiscoveryBacksOff(t *testing.T) {
	want := []time.Duration{
		15 * time.Second,
		30 * time.Second,
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		4 * time.Minute, // and stays there
		4 * time.Minute,
	}
	var wait time.Duration
	for i, w := range want {
		wait = discoverPace(wait, false, false)
		if wait != w {
			t.Fatalf("round %d: waited %v, want %v", i+1, wait, w)
		}
	}
	if want[len(want)-1] <= discoverMin {
		t.Fatal("the schedule never actually backs off")
	}
}

func TestDiscoveryStaysFastWhileRoundsFindPeople(t *testing.T) {
	var wait time.Duration
	for i := 0; i < 5; i++ {
		wait = discoverPace(wait, true, false)
		if wait != discoverMin {
			t.Fatalf("round %d found somebody new and still waited %v", i+1, wait)
		}
	}
	// One barren round backs off; the next find snaps straight back to eager
	// rather than crawling down the schedule it climbed.
	wait = discoverPace(wait, false, false)
	if wait != 2*discoverMin {
		t.Fatalf("first barren round waited %v", wait)
	}
	if got := discoverPace(wait, true, false); got != discoverMin {
		t.Fatalf("a find after a barren round waited %v, want %v", got, discoverMin)
	}
}

// TestUnmetDemandCapsTheBackoff: a device of the user's own that is not here
// yet is worth a lookup a minute. It is not worth one every fifteen seconds
// forever, which is why demand caps the backoff instead of pinning it.
func TestUnmetDemandCapsTheBackoff(t *testing.T) {
	var wait time.Duration
	for i := 0; i < 10; i++ {
		wait = discoverPace(wait, false, true)
	}
	if wait != discoverWanted {
		t.Fatalf("with a peer still missing the loop settled at %v, want %v", wait, discoverWanted)
	}
	if discoverWanted <= discoverMin {
		t.Fatal("demand pins the loop at its fastest cadence rather than capping it")
	}
	// Demand satisfied: the backoff is free to run out to its full length again.
	wait = discoverPace(wait, false, false)
	if wait != 2*discoverWanted {
		t.Fatalf("once the device arrived the loop waited %v, want %v", wait, 2*discoverWanted)
	}
}

// TestUnreachablePeerDoesNotHoldTheLoopOpen: "new" is measured against the
// previous round, not against whether we managed to connect. A peer the
// rendezvous keeps offering and we can never reach — a friend behind a firewall
// that eats our dials — would otherwise look like a fresh find every 15s and
// hold the loop at its fastest cadence indefinitely.
func TestUnreachablePeerDoesNotHoldTheLoopOpen(t *testing.T) {
	stubborn := []peer.ID{"unreachable-friend"}

	seen := map[peer.ID]bool{}
	if !newCandidate(seen, stubborn) {
		t.Fatal("the first sighting of a peer is not treated as new")
	}
	seen = map[peer.ID]bool{stubborn[0]: true}
	if newCandidate(seen, stubborn) {
		t.Fatal("the same unreachable peer, offered again, still counts as a find")
	}
	if !newCandidate(seen, []peer.ID{stubborn[0], "somebody-else"}) {
		t.Fatal("a genuinely new peer alongside a familiar one was missed")
	}
	if newCandidate(seen, nil) {
		t.Fatal("an empty round counted as a find")
	}
}

// TestKickResetsTheDiscoveryCadence: coming back on the network, returning to
// the foreground and joining a guild all land here. Whatever backoff was earned
// while idle describes a world that no longer applies.
func TestKickResetsTheDiscoveryCadence(t *testing.T) {
	var wait time.Duration
	for i := 0; i < 6; i++ {
		wait = discoverPace(wait, false, false)
	}
	if wait != discoverMax {
		t.Fatalf("setup: expected the loop parked at %v, got %v", discoverMax, wait)
	}
	// discoverLoop zeroes the wait on a kick; discoverPace then floors it.
	if got := discoverPace(0, false, false); got != discoverMin {
		t.Fatalf("after a kick the next round waited %v, want %v", got, discoverMin)
	}
}

// TestPeerDemandDefaultsToNone: a Host nobody wired a demand callback into must
// back off all the way, not sit at the demand cap forever.
func TestPeerDemandDefaultsToNone(t *testing.T) {
	id, _ := identity.Generate()
	h := newTestHost(t, id)
	if h.peerWanted() {
		t.Fatal("a host with no demand callback reported unmet demand")
	}
	h.SetPeerDemand(func() bool { return true })
	if !h.peerWanted() {
		t.Fatal("the demand callback was not consulted")
	}
	h.SetPeerDemand(nil)
	if h.peerWanted() {
		t.Fatal("clearing the demand callback left demand asserted")
	}
}
