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

	boot := bootstrapSet(cfg)
	kdht, err := dht.New(n.ctx, n.h,
		dht.Mode(dht.ModeAuto),
		dht.BootstrapPeers(boot...),
	)
	if err != nil {
		return fmt.Errorf("net: create dht: %w", err)
	}
	if err := kdht.Bootstrap(n.ctx); err != nil {
		return fmt.Errorf("net: bootstrap dht: %w", err)
	}
	n.kdht = kdht

	disc := drouting.NewRoutingDiscovery(kdht)
	n.disc = kdht
	go n.advertiseLoop(kdht, disc, rendezvous)
	go n.keepBootstrapped(kdht, boot, cfg.RememberedPeers)
	go n.discoverLoop(disc, rendezvous)
	return nil
}

// advertiseLoop publishes this peer under the rendezvous key, forever.
//
// It replaces discovery/util.Advertise, whose failure path costs two minutes of
// invisibility every single launch. That helper retries a failed Advertise after
// a FLAT two-minute sleep, and the first Advertise always fails: startDHT runs it
// the instant the DHT is created, when the routing table is empty and the
// bootstrap dial (keepBootstrapped, a goroutine started on the next line) has not
// even been attempted — "failed to find any peer in table". So a client that had
// just started, or had just come back from a dropped network, was absent from the
// rendezvous key for 120s while believing it had announced itself.
//
// Measured against the symptom the user reported: a phone waking up and a desktop
// that had been running took 120.06s to connect. Everything downstream inherits
// that — voice presence, gossip, history catch-up — because none of it can happen
// before the two peers have a connection. This is the single largest cause of the
// "I join voice on my phone and my desktop sees it a minute later" delay.
//
// So: wait for a routing table before the first attempt, retry on a short
// backoff, and re-announce immediately whenever we regain the network (the kick).
func (n *Host) advertiseLoop(kdht *dht.IpfsDHT, disc *drouting.RoutingDiscovery, rendezvous string) {
	const (
		minRetry = 2 * time.Second
		maxRetry = 30 * time.Second
	)
	backoff := minRetry
	for {
		// Advertising into an empty routing table cannot succeed, and every failed
		// attempt is a wasted DHT query. Wait for a peer first — but not forever:
		// the timeout falls through to an attempt anyway, so a routing table that
		// is technically empty while a bootstrap connection exists still gets tried.
		n.waitRoutingTable(kdht, 30*time.Second)
		ttl, err := disc.Advertise(n.ctx, rendezvous)
		wait := backoff
		if err == nil {
			backoff = minRetry
			// 7/8 of the TTL, as the upstream helper does: re-announce before the
			// record expires rather than after.
			wait = 7 * ttl / 8
			if wait <= 0 {
				wait = maxRetry
			}
		} else if backoff < maxRetry {
			backoff *= 2
		}
		select {
		case <-n.ctx.Done():
			return
		case <-n.netKick():
			// We just (re)gained a way into the network. Whatever we were waiting
			// out is stale — announce again now, which is the difference between a
			// returning phone appearing in seconds and appearing on the next TTL.
		case <-time.After(wait):
		}
	}
}

// waitRoutingTable blocks until the DHT knows at least one peer, or the timeout
// expires. Polling is fine here: it runs once per advertise attempt, not per tick.
func (n *Host) waitRoutingTable(kdht *dht.IpfsDHT, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if kdht.RoutingTable().Size() > 0 {
			return
		}
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// netKick returns the channel closed-and-replaced whenever we regain a way into
// the network, so the advertise and discovery loops can restart at once instead
// of waiting out a timer that was scheduled while we were offline.
func (n *Host) netKick() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.kick == nil {
		n.kick = make(chan struct{})
	}
	return n.kick
}

// kickNetwork wakes every loop parked on netKick.
func (n *Host) kickNetwork() {
	n.mu.Lock()
	if n.kick != nil {
		close(n.kick)
	}
	n.kick = make(chan struct{})
	n.mu.Unlock()
}

// FindPeer asks the DHT where a specific peer is right now.
//
// This is a targeted Kademlia lookup, not a rendezvous-key provider query: it
// answers "where is THIS peer id" without depending on that peer having
// successfully re-advertised under the shared key. That distinction is what lets
// an account reach its own linked devices promptly — we know their peer ids from
// their certificates, so we never have to wait for their advertisement to land.
func (n *Host) FindPeer(ctx context.Context, p peer.ID) (peer.AddrInfo, error) {
	if n.disc == nil {
		return peer.AddrInfo{}, fmt.Errorf("net: no DHT")
	}
	return n.disc.FindPeer(ctx, p)
}

// bootstrapSet is the list of nodes that can let us into a DHT: the user's own
// rendezvous, plus — only when explicitly asked for — the public IPFS
// bootstrappers.
//
// The public list is the one fallback that works between two peers who have
// never met and have no server of their own. Its price is metadata: joining a
// public DHT tells strangers this peer ID exists at these addresses, and the
// rendezvous key we advertise under is guessable, so an observer can enumerate
// Concord nodes. Messages stay sealed; the fact of running Concord does not.
// That trade is the user's to make, so it is off unless they turn it on.
func bootstrapSet(cfg Config) []peer.AddrInfo {
	if !cfg.PublicBootstrap {
		return cfg.BootstrapPeers
	}
	out := append([]peer.AddrInfo{}, cfg.BootstrapPeers...)
	return append(out, dht.GetDefaultBootstrapPeerAddrInfos()...)
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
//
// Remembered peers ride the same loop rather than getting one of their own:
// they answer the same question ("do we have a way into the network?"), and one
// loop means one backoff and no two schedulers fighting over the same dials.
func (n *Host) keepBootstrapped(kdht *dht.IpfsDHT, peers, remembered []peer.AddrInfo) {
	if len(peers) == 0 && len(remembered) == 0 {
		return // LAN-only node (nothing configured, nobody met); mDNS is its discovery
	}
	const (
		minBackoff   = 2 * time.Second
		maxBackoff   = 2 * time.Minute
		whileHealthy = 30 * time.Second
	)
	backoff := minBackoff
	up, failed, first := false, false, true
	for {
		now := n.dialBootstrap(peers)
		// Peers we have actually met are the fallback that survives the loss of
		// the rendezvous entirely. Dial them on the first pass no matter what — a
		// restart should reach yesterday's friends immediately, well before any
		// DHT lookup completes — and after that only while the rendezvous is
		// unreachable, since discovery already covers us when it is up.
		if first || !now {
			reached := n.dialRemembered(remembered)
			if len(peers) == 0 {
				now = reached // no rendezvous configured: a friend IS the way in
			}
		}
		first = false
		if now && !up {
			// We just (re)gained a way into the network. The routing table may be
			// empty or stale, so refresh it rather than waiting for the DHT's own
			// timer — that's the difference between "connects in seconds" and
			// "connects in an hour".
			if kdht != nil {
				_ = kdht.Bootstrap(n.ctx)
			}
			// …and tell the advertise/discover loops, which are otherwise parked on
			// a timer that was scheduled while there was no network to use.
			n.kickNetwork()
			if failed {
				log.Printf("concord/net: back on the network, discovery resumed")
			}
		}
		up = now
		wait := whileHealthy
		if now {
			backoff = minBackoff
		} else {
			if !failed {
				log.Printf("concord/net: no bootstrap node or known peer reachable, retrying in the background")
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

// dialRemembered dials peers we have connected to before and reports whether
// any of them answered. Failures are handed up — once per outage, see
// redialFailed — so the app layer can retire dead addresses; a friend who
// changed networks should stop costing a dial forever.
//
// The timeout is shorter than dialBootstrap's: these addresses are guesses from
// a previous session, there can be dozens of them, and unlike the rendezvous
// nothing else depends on any single one succeeding.
func (n *Host) dialRemembered(peers []peer.AddrInfo) bool {
	var wg sync.WaitGroup
	for _, p := range peers {
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			continue
		}
		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
			defer cancel()
			if err := n.h.Connect(ctx, pi); err != nil {
				n.redialFailed(pi.ID)
			}
		}(p)
	}
	wg.Wait()
	reached := false
	for _, p := range peers {
		if n.h.Network().Connectedness(p.ID) == network.Connected {
			n.redialReached(p.ID)
			reached = true
		}
	}
	return reached
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
		case <-n.netKick(): // back on the network: look now, not in 15s
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
