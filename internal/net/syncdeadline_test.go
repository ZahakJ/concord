package net

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
)

// TestSyncRequestHonoursItsContext: a catch-up asks over a stream, and the
// context it passes was only ever applied to OPENING that stream. A member
// that accepted the stream and never answered — a stalled process, a
// half-dead connection — held the request forever. Once catch-ups were made
// one per guild and peer at a time, that one stuck request also held every
// later catch-up between the two for good. The request must give up when its
// context does.
func TestSyncRequestHonoursItsContext(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	aID, _ := identity.Generate()
	bID, _ := identity.Generate()
	a, err := New(ctx, Config{Identity: aID})
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	defer a.Close()
	b, err := New(ctx, Config{Identity: bID})
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	defer b.Close()

	release := make(chan struct{})
	defer close(release)
	a.HandleSync(func(context.Context, peer.ID, []byte) ([]byte, error) {
		<-release // never answers while the test runs
		return nil, nil
	})
	if err := b.Connect(ctx, a.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		rctx, rcancel := context.WithTimeout(ctx, 2*time.Second)
		defer rcancel()
		_, err := b.RequestSync(rctx, a.PeerID(), []byte("{}"))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a request nobody answered reported success")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("a sync request outlived its two-second context by more than ten seconds")
	}
}
