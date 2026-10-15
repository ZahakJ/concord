package net

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/ZahakJ/concord/internal/identity"
)

// TestRememberedPeerRedialledWithoutDHT: a node with no rendezvous whose
// remembered peers all sit at private addresses runs no DHT — and the startup
// dial of remembered peers lived inside the DHT's bootstrap loop, so it never
// happened. With mDNS off or unable to cross the network, a member that
// restarted came back connected to nobody, and every guild it was in went
// silent. The remembered peer must be dialled whether or not there is a DHT.
func TestRememberedPeerRedialledWithoutDHT(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	aID, _ := identity.Generate()
	a, err := New(ctx, Config{Identity: aID})
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	defer a.Close()

	var private []multiaddr.Multiaddr
	for _, m := range a.AddrInfo().Addrs {
		if manet.IsPrivateAddr(m) || manet.IsIPLoopback(m) {
			private = append(private, m)
		}
	}
	if len(private) == 0 {
		t.Skip("no private address to remember")
	}
	bID, _ := identity.Generate()
	b, err := New(ctx, Config{
		Identity:        bID,
		EnableDHT:       false, // what wantDHT decides for a LAN-only install
		RememberedPeers: []peer.AddrInfo{{ID: a.PeerID(), Addrs: private}},
	})
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	defer b.Close()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if b.h.Network().Connectedness(a.PeerID()) == network.Connected {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("a remembered peer at a private address was never dialled")
}
