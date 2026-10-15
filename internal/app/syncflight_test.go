package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TestOneCatchUpPerGuildAtATime: the beat, the connect tail, a nudge and a
// reciprocal pull all start a guild's catch-up without asking whether one is
// already running. A member that takes its full timeout to answer used to
// collect a stack of identical requests, one per trigger. Eight triggers
// against a request that never returns must put exactly one on the wire, and
// once it returns, what queued behind it is answered by one more pass per
// queue, not one per trigger: the three single-peer callers share a second
// request, and the five guild-wide callers share one more guild-wide pass,
// which makes the third. They asked after the first question went out, so it
// cannot answer them.
func TestOneCatchUpPerGuildAtATime(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("flight")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)
	// Let whatever the join itself started run out first.
	waitUntil(t, 30*time.Second, func() bool {
		b.syncFlightMu.Lock()
		defer b.syncFlightMu.Unlock()
		return len(b.syncFlight) == 0
	}, "the join's own catch-ups never finished")

	var asked atomic.Int32
	release := make(chan struct{})
	b.requestSync = func(ctx context.Context, to peer.ID, req []byte) ([]byte, error) {
		asked.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, errors.New("held")
	}
	done := make(chan struct{}, 8)
	for i := 0; i < 5; i++ {
		go func() { b.syncGuildFromAnyPeer(g.ID); done <- struct{}{} }()
	}
	for i := 0; i < 3; i++ {
		go func() { _ = b.syncGuildFromPeer(g.ID, a.host.PeerID()); done <- struct{}{} }()
	}
	waitUntil(t, 10*time.Second, func() bool { return asked.Load() >= 1 }, "no catch-up reached the wire")
	time.Sleep(300 * time.Millisecond)
	got := asked.Load()
	close(release)
	for i := 0; i < 8; i++ {
		<-done
	}
	if got != 1 {
		t.Fatalf("%d catch-ups of one guild from one peer were in flight at once, want 1", got)
	}
	if total := asked.Load(); total != 3 {
		t.Fatalf("the queued triggers were answered by %d requests in all, want 3", total)
	}
}
