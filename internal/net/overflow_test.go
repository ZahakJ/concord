package net

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
)

// A reader that falls behind loses whatever gossipsub could not buffer for it,
// and that loss used to be silent: an online member missed part of a busy
// channel until the anti-entropy pass a minute or two later. The loss is now
// reported, with the topic and the peer to ask, so the app can catch up at
// once. The buffer is shrunk here so a handful of messages overflow it.
func TestADroppedDeliveryIsReported(t *testing.T) {
	old := subBufferSize
	subBufferSize = 2
	defer func() { subBufferSize = old }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a, b := newTestHost(t, idA), newTestHost(t, idB)
	if err := a.Connect(ctx, b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	psA, err := a.NewPubSub(ctx)
	if err != nil {
		t.Fatal(err)
	}
	psB, err := b.NewPubSub(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const topic = "test-overflow-topic"
	var dropped atomic.Int64
	var via atomic.Value
	psA.OnOverflow(func(tp string, from peer.ID) {
		if tp == topic {
			dropped.Add(1)
			via.Store(from)
		}
	})
	release := make(chan struct{})
	defer close(release)
	var first atomic.Bool
	if err := psA.Subscribe(ctx, topic, func(peer.ID, []byte) {
		if first.CompareAndSwap(false, true) {
			<-release // a reader that has fallen behind
		}
	}); err != nil {
		t.Fatal(err)
	}
	_ = psB.Subscribe(ctx, topic, func(peer.ID, []byte) {})

	deadline := time.Now().Add(20 * time.Second)
	for i := 0; dropped.Load() == 0 && time.Now().Before(deadline); i++ {
		_ = psB.Publish(ctx, topic, []byte(fmt.Sprintf("m-%d", i)))
		time.Sleep(20 * time.Millisecond)
	}
	if dropped.Load() == 0 {
		t.Fatal("messages a stalled reader could not take were dropped without a word")
	}
	if got, _ := via.Load().(peer.ID); got != b.PeerID() {
		t.Fatalf("the drop names %s as the peer to ask, want %s", got, b.PeerID())
	}
}
