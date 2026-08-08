package net

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
)

// blockingRelay stands in for the circuit-relay service. Its Close parks until
// released, which is what turns the lock-order question below into a decidable
// one rather than a race we would have to hammer at.
type blockingRelay struct {
	entered  chan struct{}
	release  chan struct{}
	enterOne sync.Once
	freeOne  sync.Once
}

func newBlockingRelay() *blockingRelay {
	return &blockingRelay{entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingRelay) Close() error {
	b.enterOne.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

// free lets a parked Close finish. Idempotent, and deferred in every path: a
// failing assertion must not leave the teardown parked, or the failure report
// would be followed by a hung test binary.
func (b *blockingRelay) free() { b.freeOne.Do(func() { close(b.release) }) }

// TestRelayLifecycleDoesNotHoldTheHostMutex pins the lock order that a deadlock
// broke. Starting or stopping the peer relay registers or unregisters a swarm
// notifiee, and the swarm holds its notifiee lock while it runs our own
// Connected/Disconnected handlers, which take the host mutex. Taking the host
// mutex around the relay call closes the cycle, and then one connection dying
// while reachability flips wedges the host for good: no notification fires
// again, so gossipsub never meshes with a new peer and the app never hears
// anyone connect. Seen from the outside as messages that simply never arrive,
// and as a test binary hung for minutes inside Service.Close.
//
// The relay call can be slow for its own reasons, so the assertion is not that
// it finishes but that the host mutex is free while it runs.
func TestRelayLifecycleDoesNotHoldTheHostMutex(t *testing.T) {
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	h, err := New(context.Background(), Config{
		Identity:    id,
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
	})
	if err != nil {
		t.Fatalf("start host: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })

	// syncRelayService is the live path (reachability flipped away, stop
	// relaying) and Close is the shutdown path. Both tear the relay down, and
	// both used to do it holding the host mutex.
	for _, tc := range []struct {
		name string
		tear func()
	}{
		{"syncRelayService", h.syncRelayService},
		{"Close", func() { _ = h.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relay := newBlockingRelay()
			defer relay.free()
			h.relayMu.Lock()
			h.relaySvc = relay
			h.relayMu.Unlock()

			done := make(chan struct{})
			go func() { defer close(done); tc.tear() }()
			select {
			case <-relay.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("never reached the relay teardown")
			}

			// The relay is mid-Close. A read of host state must still get
			// through; if it blocks, the mutex is held across the relay call.
			free := make(chan struct{})
			go func() { defer close(free); h.connectedCallbacks() }()
			select {
			case <-free:
			case <-time.After(5 * time.Second):
				t.Fatal("the host mutex is held while the relay closes: a connect or disconnect notification arriving now deadlocks the host")
			}

			relay.free()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("teardown never returned after the relay was released")
			}
		})
	}
}
