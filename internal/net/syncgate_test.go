package net

import (
	"testing"
	"time"
)

// One peer asking for sync without pause gets its burst, then one a second;
// another peer is not held back by it.
func TestSyncGatePacesOnePeer(t *testing.T) {
	var g syncGate
	now := time.Unix(1000, 0)
	served := 0
	for i := 0; i < 500; i++ {
		if g.allow("hog", now) {
			served++
		}
	}
	if served != syncBurst {
		t.Fatalf("served %d of 500 at once, want the burst of %d", served, syncBurst)
	}
	if !g.allow("other", now) {
		t.Fatal("another peer was refused")
	}
	if g.allow("hog", now.Add(500*time.Millisecond)) {
		t.Fatal("served again before a token came back")
	}
	if !g.allow("hog", now.Add(1500*time.Millisecond)) {
		t.Fatal("not served once a token came back")
	}
}
