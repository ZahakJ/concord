package net

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/identity"
)

// newTestHost brings up a host with mDNS off for deterministic wiring.
func newTestHost(t *testing.T, id *identity.Identity) *Host {
	t.Helper()
	h, err := New(context.Background(), Config{Identity: id})
	if err != nil {
		t.Fatalf("New host: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func TestPeerIDDerivedFromIdentity(t *testing.T) {
	id, _ := identity.Generate()

	h1 := newTestHost(t, id)
	h2 := newTestHost(t, id) // same identity, different host instance

	if h1.PeerID() != h2.PeerID() {
		t.Fatal("PeerID is not stable for a fixed identity")
	}

	other, _ := identity.Generate()
	h3 := newTestHost(t, other)
	if h1.PeerID() == h3.PeerID() {
		t.Fatal("distinct identities produced the same PeerID")
	}
}

func TestTwoHostsConnectAndReportPresence(t *testing.T) {
	ctx := context.Background()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()

	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	connected := make(chan peer.ID, 1)
	a.OnPeerConnected(func(p peer.ID) { connected <- p })

	if err := a.Connect(ctx, b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case p := <-connected:
		if p != b.PeerID() {
			t.Fatalf("connected callback fired for %s, want %s", p, b.PeerID())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for connect callback")
	}

	if !containsPeer(a.Peers(), b.PeerID()) {
		t.Fatal("A does not list B as a peer")
	}
	if !containsPeer(b.Peers(), a.PeerID()) {
		t.Fatal("B does not list A as a peer")
	}
}

func TestDisconnectFiresCallback(t *testing.T) {
	ctx := context.Background()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()

	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	disconnected := make(chan peer.ID, 1)
	a.OnPeerDisconnected(func(p peer.ID) { disconnected <- p })

	if err := a.Connect(ctx, b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// Wait until the connection is actually established before tearing down.
	waitFor(t, 5*time.Second, func() bool { return containsPeer(a.Peers(), b.PeerID()) })

	_ = b.Close()

	select {
	case p := <-disconnected:
		if p != b.PeerID() {
			t.Fatalf("disconnect callback fired for %s, want %s", p, b.PeerID())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for disconnect callback")
	}
}

func containsPeer(peers []peer.ID, want peer.ID) bool {
	for _, p := range peers {
		if p == want {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

// Two transports to the same peer come up concurrently often enough that both
// notifications used to see a connection count of two and neither reported the
// peer — which silently cost the app layer its contact record.
func TestConcurrentConnectionsAnnouncePeerOnce(t *testing.T) {
	id, _ := identity.Generate()
	h := newTestHost(t, id)
	other, _ := identity.Generate()
	p := newTestHost(t, other).PeerID()

	var announced atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h.markConnected(p) {
				announced.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := announced.Load(); got != 1 {
		t.Fatalf("peer announced %d times, want exactly 1", got)
	}

	if !h.markDisconnected(p) {
		t.Fatal("an announced peer must produce a disconnect")
	}
	if h.markDisconnected(p) {
		t.Fatal("a peer nobody heard connect must not produce a disconnect")
	}
}
