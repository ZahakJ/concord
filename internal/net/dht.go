package net

import (
	"context"
	"fmt"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// DefaultRendezvous namespaces Concord peers in the shared DHT so they find each
// other and not unrelated applications.
const DefaultRendezvous = "concord/dht/v1"

// startDHT brings up the Kademlia DHT for internet-wide discovery: it
// bootstraps off the configured nodes, advertises this peer under the
// rendezvous key, and continuously connects to other peers advertising it.
func (n *Host) startDHT(cfg Config) error {
	rendezvous := cfg.Rendezvous
	if rendezvous == "" {
		rendezvous = DefaultRendezvous
	}

	kdht, err := dht.New(n.ctx, n.h,
		dht.Mode(dht.ModeAuto),
		dht.BootstrapPeers(cfg.BootstrapPeers...),
	)
	if err != nil {
		return fmt.Errorf("net: create dht: %w", err)
	}
	if err := kdht.Bootstrap(n.ctx); err != nil {
		return fmt.Errorf("net: bootstrap dht: %w", err)
	}
	n.kdht = kdht

	// Seed the routing table by connecting to the bootstrap nodes.
	for _, p := range cfg.BootstrapPeers {
		p := p
		go func() { _ = n.h.Connect(n.ctx, p) }()
	}

	disc := drouting.NewRoutingDiscovery(kdht)
	dutil.Advertise(n.ctx, disc, rendezvous)
	go n.discoverLoop(disc, rendezvous)
	return nil
}

func (n *Host) discoverLoop(disc *drouting.RoutingDiscovery, rendezvous string) {
	// Poll fairly often at first (mesh is small and warming), then steadily.
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		n.findAndConnect(disc, rendezvous)
		select {
		case <-n.ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (n *Host) findAndConnect(disc *drouting.RoutingDiscovery, rendezvous string) {
	ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
	defer cancel()
	peers, err := disc.FindPeers(ctx, rendezvous)
	if err != nil {
		return
	}
	for p := range peers {
		if p.ID == n.h.ID() || len(p.Addrs) == 0 {
			continue
		}
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			continue
		}
		go func(pi peer.AddrInfo) { _ = n.h.Connect(n.ctx, pi) }(p)
	}
}
