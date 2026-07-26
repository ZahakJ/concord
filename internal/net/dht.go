package net

import (
	"context"
	"fmt"
	"log"
	"sync"
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

	disc := drouting.NewRoutingDiscovery(kdht)
	dutil.Advertise(n.ctx, disc, rendezvous)
	go n.keepBootstrapped(kdht, cfg.BootstrapPeers)
	go n.discoverLoop(disc, rendezvous)
	return nil
}

// keepBootstrapped keeps at least one bootstrap node connected, for as long as
// the host lives.
//
// This has to be a loop, not a one-shot dial at startup. The first attempt
// routinely fails through no fault of ours — the app launches before Windows
// has finished bringing the network up, a laptop resumes from sleep, a VPN
// flaps, the rendezvous is briefly restarting. And a failure there is total,
// not partial: the DHT can only refresh a routing table that already has
// someone in it, so a node that never reached a bootstrap peer has no way back
// in. It looks exactly like "the internet works, but this app can't see
// anyone", and the only cure used to be restarting the app.
//
// So: retry with backoff while disconnected, re-check on a slow beat while
// connected, and kick the DHT after a reconnection so discovery restarts
// immediately instead of waiting out the next refresh cycle.
func (n *Host) keepBootstrapped(kdht *dht.IpfsDHT, peers []peer.AddrInfo) {
	if len(peers) == 0 {
		return // LAN-only node (no rendezvous configured); mDNS is its discovery
	}
	const (
		minBackoff   = 2 * time.Second
		maxBackoff   = 2 * time.Minute
		whileHealthy = 30 * time.Second
	)
	backoff := minBackoff
	up, failed := false, false
	for {
		now := n.dialBootstrap(peers)
		if now && !up {
			// We just (re)gained a way into the network. The routing table may be
			// empty or stale, so refresh it rather than waiting for the DHT's own
			// timer — that's the difference between "connects in seconds" and
			// "connects in an hour".
			if kdht != nil {
				_ = kdht.Bootstrap(n.ctx)
			}
			if failed {
				log.Printf("concord/net: reconnected to a bootstrap node, discovery resumed")
			}
		}
		up = now
		wait := whileHealthy
		if now {
			backoff = minBackoff
		} else {
			if !failed {
				log.Printf("concord/net: no bootstrap node reachable, retrying in the background")
			}
			failed = true
			wait = backoff
			if backoff < maxBackoff {
				backoff *= 2
			}
		}
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// dialBootstrap dials every bootstrap node we aren't already connected to and
// reports whether at least one connection is up afterwards.
func (n *Host) dialBootstrap(peers []peer.AddrInfo) bool {
	var wg sync.WaitGroup
	for _, p := range peers {
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			continue
		}
		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
			defer cancel()
			_ = n.h.Connect(ctx, pi)
		}(p)
	}
	wg.Wait()
	for _, p := range peers {
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			return true
		}
	}
	return false
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
