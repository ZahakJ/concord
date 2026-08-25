package net

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
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

// TestDesktopKeepsStockGossipsubParams: the desktop path must be the library's
// own configuration, not a copy of it that drifts. Asserted as a whole-struct
// comparison so a field added upstream cannot quietly become a Concord opinion.
func TestDesktopKeepsStockGossipsubParams(t *testing.T) {
	got, tuned := gossipsubParams(false)
	if tuned {
		t.Fatal("the desktop path reported itself as tuned; it must pass no options at all")
	}
	if !reflect.DeepEqual(got, pubsub.DefaultGossipSubParams()) {
		t.Fatal("desktop gossipsub params have drifted from the library defaults")
	}
}

// TestPhonesRunACalmerGossipsub pins the two mobile changes and, just as
// importantly, everything left alone. D and the mesh bounds are the message
// delivery path; the history length is what a peer that missed a message can
// still be sent. Trimming either of those would be trading reliability for
// battery, which is not the trade being made here.
func TestPhonesRunACalmerGossipsub(t *testing.T) {
	stock := pubsub.DefaultGossipSubParams()
	got, tuned := gossipsubParams(true)
	if !tuned {
		t.Fatal("the phone path did not report itself as tuned, so no options would be passed")
	}
	if got.HeartbeatInterval <= stock.HeartbeatInterval {
		t.Fatalf("HeartbeatInterval = %v, want longer than the stock %v", got.HeartbeatInterval, stock.HeartbeatInterval)
	}
	if got.HeartbeatInterval != 2*time.Second {
		t.Fatalf("HeartbeatInterval = %v, want 2s — see gossipsubParams for why not longer", got.HeartbeatInterval)
	}
	if got.Dlazy >= stock.Dlazy {
		t.Fatalf("Dlazy = %d, want fewer than the stock %d", got.Dlazy, stock.Dlazy)
	}
	if got.D != stock.D || got.Dlo != stock.Dlo || got.Dhi != stock.Dhi {
		t.Fatalf("the mesh degree moved (D=%d Dlo=%d Dhi=%d); that is the delivery path, not the idle floor",
			got.D, got.Dlo, got.Dhi)
	}
	if got.HistoryLength != stock.HistoryLength || got.HistoryGossip != stock.HistoryGossip {
		t.Fatal("the message cache was trimmed; that is what serves a peer who missed a message")
	}
	if got.GossipFactor != stock.GossipFactor {
		t.Fatal("GossipFactor moved; it never applies below 24 non-mesh peers on one topic, which a phone never reaches")
	}
}

// TestMobileGossipsubParamsAreAcceptedByTheRouter: GossipSubParams is validated
// on the way in, and a rejected option means NewPubSub returns an error on
// every phone while every desktop test in this package stays green. Building a
// real router with them is the only thing that catches that.
func TestMobileGossipsubParamsAreAcceptedByTheRouter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	id, _ := identity.Generate()
	h := newTestHost(t, id)
	params, _ := gossipsubParams(true)
	if _, err := pubsub.NewGossipSub(ctx, h.h, pubsub.WithGossipSubParams(params)); err != nil {
		t.Fatalf("gossipsub refused the phone parameters: %v", err)
	}
}
