package app

import (
	"context"
	"testing"
)

// Setting a rendezvous in the app used to save it, dial it, and then leave the
// rest of the session believing none was configured: s.bootstrap was never
// reassigned, so NetworkStatus reported HasBootstrap false, the mailbox never
// registered with the new node, and Nudge wouldn't re-dial it. From the user's
// seat that reads as "the rendezvous is broken" while a live connection to it
// sits in the peer list.
func TestSetBootstrapLiveAppliesToThisSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)

	if s.NetworkStatus().HasBootstrap {
		t.Fatal("a fresh test service should have no rendezvous configured")
	}

	// A syntactically valid multiaddr for a node that need not exist: this is
	// about adopting the address, not reaching it.
	addr := "/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWCzonwxmETSLwgDY9JAe9WczUHssnYTrSpCqkp6UXZg1q"
	if err := s.SetBootstrapLive([]string{addr}); err != nil {
		t.Fatalf("SetBootstrapLive: %v", err)
	}

	if !s.NetworkStatus().HasBootstrap {
		t.Error("NetworkStatus still reports no rendezvous after setting one")
	}
	if got := len(s.bootstrapPeers()); got != 1 {
		t.Errorf("bootstrapPeers() = %d, want 1 — the session never adopted the new address", got)
	}

	// Clearing it has to work the same way round, or removing a rendezvous
	// leaves the old one live until restart.
	if err := s.SetBootstrapLive(nil); err != nil {
		t.Fatalf("SetBootstrapLive(nil): %v", err)
	}
	if len(s.bootstrapPeers()) != 0 {
		t.Error("clearing the rendezvous left the old one in this session")
	}
}
