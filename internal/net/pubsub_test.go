package net

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/identity"
)

// TestUnsubscribeStopsDeliveryAndAllowsResubscribe: leaving a guild unwinds
// its topic subscriptions, and re-joining re-subscribes the same topic names.
// Both halves matter — Unsubscribe must actually silence the handler, and it
// must not poison the topic for a later Subscribe (gossipsub refuses a second
// Join of the same name, so the handle bookkeeping has to survive the cycle).
func TestUnsubscribeStopsDeliveryAndAllowsResubscribe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)
	if err := a.Connect(ctx, b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	psA, err := a.NewPubSub(ctx)
	if err != nil {
		t.Fatalf("NewPubSub A: %v", err)
	}
	psB, err := b.NewPubSub(ctx)
	if err != nil {
		t.Fatalf("NewPubSub B: %v", err)
	}

	const topic = "test-unsub-topic"
	var got atomic.Int64
	handler := func(_ peer.ID, _ []byte) { got.Add(1) }
	if err := psA.Subscribe(ctx, topic, handler); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// publishUntil publishes from B until A's counter passes want (mesh
	// formation is asynchronous, so single publishes can be dropped early on).
	publishUntil := func(want int64) bool {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			_ = psB.Publish(ctx, topic, []byte(fmt.Sprintf("m-%d", time.Now().UnixNano())))
			if got.Load() >= want {
				return true
			}
			time.Sleep(200 * time.Millisecond)
		}
		return got.Load() >= want
	}
	if !publishUntil(1) {
		t.Fatal("no delivery before unsubscribe — mesh never formed")
	}

	psA.Unsubscribe(topic)
	// Let in-flight frames drain, then measure silence over a window that is
	// long relative to gossipsub's heartbeat.
	time.Sleep(1 * time.Second)
	base := got.Load()
	for i := 0; i < 10; i++ {
		_ = psB.Publish(ctx, topic, []byte(fmt.Sprintf("after-%d", i)))
		time.Sleep(200 * time.Millisecond)
	}
	if n := got.Load(); n != base {
		t.Fatalf("handler ran %d times after Unsubscribe", n-base)
	}

	// Re-subscribe (the explicit re-join case) — same topic name must work.
	if err := psA.Subscribe(ctx, topic, handler); err != nil {
		t.Fatalf("re-Subscribe after Unsubscribe: %v", err)
	}
	before := got.Load()
	if !publishUntil(before + 1) {
		t.Fatal("no delivery after re-subscribe — Unsubscribe poisoned the topic")
	}
}
