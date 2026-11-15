package app

import (
	"context"
	"testing"
)

// The proven latch rests only on a commit this device's own leaf made. A
// commit another member made at this device's epoch verifies against its tree
// just as well, but it is not proof that this copy is stuck: that one it could
// apply.
func TestOnlyAnOwnCommitProvesBehind(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, gid, _ := guardedPair(t, ctx, boot)
	groupID := rhGroup(u, gid)
	ue, _ := u.mls.Epoch(ctx, groupID)
	pe, _ := p.mls.Epoch(ctx, groupID)
	if ue != pe {
		t.Fatalf("setup: U at %d, P at %d", ue, pe)
	}
	// P's commit at the shared epoch, made in P's engine only: nobody is told.
	c, err := p.mls.Remove(ctx, groupID, u.myCredential)
	if err != nil {
		t.Fatalf("P's commit: %v", err)
	}
	if err := u.mls.VerifyCommit(ctx, groupID, c); err != nil {
		t.Fatalf("setup: P's commit does not verify on U: %v", err)
	}
	if u.ownLostCommit(groupID, c) {
		t.Fatal("another member's commit was taken as proof that this copy is stuck")
	}
}
