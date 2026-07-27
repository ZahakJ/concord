package net

import (
	"context"
	"sync"
	"testing"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/identity"
)

// TestRememberedPeerReconnectsWithoutBootstrap is the whole point of the peer
// cache: with no rendezvous configured and no mDNS, a node that was told where
// it last found a peer gets back to it on its own.
func TestRememberedPeerReconnectsWithoutBootstrap(t *testing.T) {
	loopback := []string{"/ip4/127.0.0.1/tcp/0"}

	idA, _ := identity.Generate()
	a, err := New(context.Background(), Config{Identity: idA, ListenAddrs: loopback})
	if err != nil {
		t.Fatalf("start peer A: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	idB, _ := identity.Generate()
	b, err := New(context.Background(), Config{
		Identity:        idB,
		ListenAddrs:     loopback,
		EnableDHT:       true,
		RememberedPeers: []peer.AddrInfo{a.AddrInfo()},
	})
	if err != nil {
		t.Fatalf("start peer B: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if b.Libp2p().Network().Connectedness(a.PeerID()) == network.Connected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("B never re-dialled the peer it was told to remember")
}

// TestRedialFailureIsReportedOncePerOutage drives the pruning signal the app
// layer needs: a remembered address that nobody is listening on has to come
// back as a failure, or dead entries would live in the cache forever — but
// exactly once, because the app spends a small budget of failures before it
// deletes the peer. The retry loop below runs on a 2s/4s/8s/16s backoff, so
// reporting per attempt would exhaust that budget inside a minute and erase a
// friend who is merely offline tonight.
func TestRedialFailureIsReportedOncePerOutage(t *testing.T) {
	gone, _ := identity.Generate()
	dead, err := New(context.Background(), Config{Identity: gone, ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}})
	if err != nil {
		t.Fatalf("start throwaway peer: %v", err)
	}
	stale := dead.AddrInfo()
	_ = dead.Close()

	idB, _ := identity.Generate()
	b, err := New(context.Background(), Config{
		Identity:        idB,
		ListenAddrs:     []string{"/ip4/127.0.0.1/tcp/0"},
		EnableDHT:       true,
		RememberedPeers: []peer.AddrInfo{stale},
	})
	if err != nil {
		t.Fatalf("start peer B: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	var mu sync.Mutex
	var reports []peer.ID
	b.OnRedialFailed(func(p peer.ID) {
		mu.Lock()
		reports = append(reports, p)
		mu.Unlock()
	})

	// Long enough for the loop to have retried several times over.
	time.Sleep(20 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 1 {
		t.Fatalf("one offline peer produced %d failure reports, want exactly 1", len(reports))
	}
	if reports[0] != stale.ID {
		t.Fatalf("reported the wrong peer: %s", reports[0])
	}
}

// TestRedialFailuresAreArmedAgainByASuccess keeps the dedupe from becoming
// "report once, ever": a peer that comes back and later goes away again is a
// second outage, and has to count as one.
func TestRedialFailuresAreArmedAgainByASuccess(t *testing.T) {
	n := &Host{}
	var reports []peer.ID
	n.OnRedialFailed(func(p peer.ID) { reports = append(reports, p) })

	friend := peer.ID("friend-asleep-tonight")
	for i := 0; i < 8; i++ {
		n.redialFailed(friend)
	}
	if len(reports) != 1 {
		t.Fatalf("eight failed attempts in one outage reported %d failures, want 1", len(reports))
	}

	n.redialReached(friend)
	n.redialFailed(friend)
	if len(reports) != 2 {
		t.Fatalf("a fresh outage after a reconnection reported %d failures in total, want 2", len(reports))
	}
}

// TestRedialFailuresBeforeAnyoneListensAreNotSwallowed guards the ordering: the
// app registers its callback a moment after the host starts, and if the first
// attempt consumed the peer's single report there, a truly dead address would
// never be retired at all.
func TestRedialFailuresBeforeAnyoneListensAreNotSwallowed(t *testing.T) {
	n := &Host{}
	dead := peer.ID("gone-for-good")
	n.redialFailed(dead)

	var reports []peer.ID
	n.OnRedialFailed(func(p peer.ID) { reports = append(reports, p) })
	n.redialFailed(dead)
	if len(reports) != 1 {
		t.Fatalf("want the first report after registration to arrive, got %d", len(reports))
	}
}

func TestBootstrapSetOptsIntoPublicNodes(t *testing.T) {
	rendezvous := []peer.AddrInfo{{ID: "seed"}}

	if got := bootstrapSet(Config{BootstrapPeers: rendezvous}); len(got) != 1 {
		t.Fatalf("public DHT must stay off by default, got %d bootstrappers", len(got))
	}

	got := bootstrapSet(Config{BootstrapPeers: rendezvous, PublicBootstrap: true})
	want := 1 + len(dht.GetDefaultBootstrapPeerAddrInfos())
	if len(got) != want {
		t.Fatalf("want %d bootstrappers with the opt-in on, got %d", want, len(got))
	}
	if got[0].ID != rendezvous[0].ID {
		t.Fatal("the user's own rendezvous must still be tried first")
	}
}
