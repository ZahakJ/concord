package app

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TestStalledCatchUpDoesNotFreezeThePair: one catch-up to a member that
// accepted the request and never answered used to hang for good — the
// request's deadline stopped applying once the stream was open — and because
// catch-ups between a guild and a peer run one at a time, every later one
// between the two queued behind it forever. That pair never synced again, and
// anything only that member held never arrived. Once the member answers again,
// the next catch-up has to go through.
//
// What the stalled call itself returns is coalesceSync's rule, not its own
// request's: a caller that finds a pass queued behind its own runs that pass
// too and reports it, the last pass it ran, which began after it asked. Here
// that pass runs after the member recovered, so "success" from the stalled call
// is true — the pair is level — and this test used to fail on it whenever the
// stalled call happened to lead (it passed only when a catch-up from the
// setup was still leading). It now pins the stalled call in the lead.
func TestStalledCatchUpDoesNotFreezeThePair(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, channel := guildOf(t, ctx, 2)
	a, b := svcs[0], svcs[1]

	// Let the setup's own catch-ups between the pair finish, so the stalled
	// call below is the one running and the next one queues behind it.
	key := guildID + "|" + a.host.PeerID().String()
	waitUntil(t, 30*time.Second, func() bool {
		b.syncFlightMu.Lock()
		defer b.syncFlightMu.Unlock()
		_, busy := b.syncFlight[key]
		return !busy
	}, "the setup's catch-ups never finished")

	stall := make(chan struct{})
	defer close(stall)
	a.host.HandleSync(func(context.Context, peer.ID, []byte) ([]byte, error) {
		<-stall
		return nil, nil
	})
	stuck := make(chan error, 1)
	go func() { stuck <- b.syncGuildFromPeer(guildID, a.host.PeerID()) }()
	time.Sleep(500 * time.Millisecond) // the stalled request is on the wire

	// A recovers; B's next catch-up must reach it.
	a.host.HandleSync(a.handleSyncRequest)
	m := plantRow(t, a, channel, "", "only A holds this", time.Now())
	next := make(chan error, 1)
	go func() { next <- b.syncGuildFromPeer(guildID, a.host.PeerID()) }()
	select {
	case err := <-next:
		if err != nil {
			t.Fatalf("the catch-up after the stall failed: %v", err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("a catch-up queued behind a stalled one never ran")
	}
	if _, ok, _ := b.store.MessageByID(m.ID); !ok {
		t.Fatal("the catch-up after the stall did not deliver what A holds")
	}
	// The stalled call ran the queued pass itself, so it returns with that
	// pass's answer: success, with A's row in hand.
	select {
	case err := <-stuck:
		if err != nil {
			t.Fatalf("the stalled call reported %v, though the pass it ran after A recovered succeeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stalled call never returned")
	}
}
