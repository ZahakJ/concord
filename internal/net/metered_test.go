package net

import (
	"testing"
	"time"
)

// The metered floor is the data-plan regression. Discovery on a foregrounded
// phone runs a Kademlia walk every fifteen seconds whenever rounds keep turning
// up somebody new, and on cellular that is the owner's money — so the floor
// applies with the app on screen, which is precisely where background mode does
// not.
func TestMeteredHoldsAFloorEvenInTheForeground(t *testing.T) {
	if got := paceWait(discoverMin, false, true); got != meteredFloor {
		t.Errorf("metered and on screen, a %v wait ran at %v, want the %v floor",
			discoverMin, got, meteredFloor)
	}
	if got := paceWait(discoverMin, false, false); got != discoverMin {
		t.Errorf("unmetered, a %v wait was stretched to %v — the floor is leaking",
			discoverMin, got)
	}
}

// The two floors compose. The bug this guards is the obvious refactor where one
// flag returns early and quietly cancels the other: a backgrounded phone on
// cellular must get the SLOWER of the two, not whichever branch ran last.
func TestBackgroundAndMeteredComposeToTheSlowerFloor(t *testing.T) {
	if backgroundBeat <= meteredFloor {
		t.Fatal("this test assumes the background beat is the slower of the two floors")
	}
	cases := []struct {
		background, metered bool
		want                time.Duration
	}{
		{false, false, discoverMin},
		{false, true, meteredFloor},
		{true, false, backgroundBeat},
		{true, true, backgroundBeat},
	}
	for _, c := range cases {
		if got := paceWait(discoverMin, c.background, c.metered); got != c.want {
			t.Errorf("background=%v metered=%v: %v, want %v",
				c.background, c.metered, got, c.want)
		}
	}
}

// A wait already slower than every floor is left alone. advertiseLoop parks on
// 7/8 of a DHT TTL after a successful announce; a floor that "paced" that would
// be re-announcing hours early, which is more traffic on the metered link, not
// less.
func TestAFloorNeverDragsASlowWaitFaster(t *testing.T) {
	long := 2 * time.Hour
	for _, bg := range []bool{false, true} {
		for _, m := range []bool{false, true} {
			if got := paceWait(long, bg, m); got != long {
				t.Errorf("background=%v metered=%v: a %v wait became %v", bg, m, long, got)
			}
		}
	}
}

// Zero is the value the loops carry immediately after a kick, and it must stay
// zero on an unmetered foreground link so a join still runs its round at once.
// Metered it is held to the floor like any other short wait — the round the
// kick itself forces has already happened by then (discoverLoop runs
// findAndConnect before consulting pace), so joins stay instant on cellular.
func TestKickedWaitIsOnlyHeldByARealFloor(t *testing.T) {
	if got := paceWait(0, false, false); got != 0 {
		t.Errorf("a kicked loop waited %v before its next round, want none", got)
	}
	if got := paceWait(0, false, true); got != meteredFloor {
		t.Errorf("metered, the round after a kick was scheduled at %v, want %v", got, meteredFloor)
	}
}

// SetMetered's unmetered edge has to wake the loops: whatever they backed off to
// was earned on a link that was costing money, and walking into Wi-Fi is exactly
// when the round being held back became free. The backgrounded edge in
// SetBackground has the same shape and the same reason.
func TestLeavingAMeteredNetworkKicksTheLoops(t *testing.T) {
	h := &Host{}
	kick := h.netKick()
	h.SetMetered(true)
	select {
	case <-kick:
		t.Fatal("going ON to a metered network woke the loops; only coming off it should")
	default:
	}
	h.SetMetered(false)
	select {
	case <-kick:
	default:
		t.Fatal("coming off a metered network left the loops parked on their backed-off timer")
	}
}

func TestSetMeteredIsIdempotent(t *testing.T) {
	h := &Host{}
	h.SetMetered(false)
	kick := h.netKick()
	h.SetMetered(false)
	select {
	case <-kick:
		t.Fatal("a repeated SetMetered(false) kicked the loops; only the edge should")
	default:
	}
	if h.meteredNet() {
		t.Error("the host thinks it is metered after being told it is not")
	}
}
