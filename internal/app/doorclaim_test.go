package app

import "testing"

// A peer's unproven word that we are behind (an EpochGap, or an empty answer
// naming a later epoch) raises the flag so the guild heals, but it does not
// shut this device's door: any member could otherwise shut every moderator's
// door by answering their syncs with a made-up epoch. A flag raised on this
// device's own evidence still shuts it.
func TestAClaimedGapDoesNotShutTheDoor(t *testing.T) {
	s := &Service{
		outOfSync: map[string]bool{},
		evicted:   map[string]string{},
		// Recovery is "already running", so raising the flag starts nothing.
		recovering: map[string]bool{"g": true},
	}
	s.setOutOfSyncClaim("g")
	if !s.OutOfSync("g") {
		t.Fatal("a claimed gap did not raise the flag; the guild would not heal")
	}
	if s.doorShut("g") {
		t.Fatal("a claimed gap shut the door")
	}
	// The device's own evidence (a fork it saw, a commit it could not
	// apply) shuts it, and a later claim does not reopen it.
	s.setOutOfSync("g", true)
	s.setOutOfSyncClaim("g")
	if !s.doorShut("g") {
		t.Fatal("a flag on our own evidence left the door open")
	}
	// Level again: open.
	s.setOutOfSync("g", false)
	if s.doorShut("g") {
		t.Fatal("the door stayed shut after the flag dropped")
	}
}
