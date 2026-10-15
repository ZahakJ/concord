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
func TestStalledCatchUpDoesNotFreezeThePair(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, channel := guildOf(t, ctx, 2)
	a, b := svcs[0], svcs[1]

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
	case <-next:
	case <-time.After(45 * time.Second):
		t.Fatal("a catch-up queued behind a stalled one never ran")
	}
	if _, ok, _ := b.store.MessageByID(m.ID); !ok {
		t.Fatal("the catch-up after the stall did not deliver what A holds")
	}
	if err := <-stuck; err == nil {
		t.Fatal("the stalled catch-up reported success")
	}
}
