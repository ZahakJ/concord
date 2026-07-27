// Package net is layer 2 of Concord: the peer-to-peer transport. It wraps a
// libp2p host whose identity is derived from the same Ed25519 keypair as the
// user's account (internal/identity), so a peer's network address (its libp2p
// PeerID) is cryptographically bound to its account and cannot be spoofed.
//
// Layer-2 responsibilities: bring up the host, encrypt every connection
// (Noise), discover peers on the LAN (mDNS), and surface connect/disconnect
// events upward. Group messaging (gossipsub) and NAT traversal live in sibling
// files as later phases build them out.
package net

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/multiformats/go-multiaddr"

	"github.com/zahak/concord/internal/identity"
)

// DefaultServiceTag namespaces Concord's mDNS discovery so instances only find
// other Concord peers on the LAN, not unrelated libp2p apps.
const DefaultServiceTag = "concord._mdns._v1"

// defaultListenAddrs binds TCP and QUIC on all interfaces, ephemeral ports.
var defaultListenAddrs = []string{
	"/ip4/0.0.0.0/tcp/0",
	"/ip4/0.0.0.0/udp/0/quic-v1",
	"/ip6/::/tcp/0",
	"/ip6/::/udp/0/quic-v1",
}

// Config configures a Host.
type Config struct {
	// Identity is the account keypair; the libp2p PeerID is derived from it by
	// default (single-device, and every pre-multi-device client).
	Identity *identity.Identity
	// HostKey, when set, overrides the account key as the libp2p host key — a
	// LINKED device uses its own per-device key so its PeerID doesn't collide
	// with the same account running on another device. The account seed still
	// lives in Identity (for MLS credentials, mailbox keys, etc.); only the
	// network identity differs. nil = derive the PeerID from the account key.
	HostKey ed25519.PrivateKey
	// ListenAddrs overrides the default listen multiaddrs when non-empty.
	ListenAddrs []string
	// ServiceTag overrides the mDNS discovery namespace when non-empty.
	ServiceTag string
	// EnableMDNS turns on LAN peer discovery. Tests that wire peers manually
	// leave it false to stay deterministic.
	EnableMDNS bool

	// EnableDHT turns on internet-wide discovery via the Kademlia DHT: peers
	// advertise and find each other under Rendezvous, bootstrapping from
	// BootstrapPeers. Combined with relay + hole punching, this connects peers
	// across NATs beyond the local network.
	EnableDHT      bool
	BootstrapPeers []peer.AddrInfo
	Rendezvous     string

	// RememberedPeers are peers this node has connected to before, loaded from
	// disk by the app layer. They are the fallback that needs no server at all:
	// if the rendezvous disappears, yesterday's friends are still dialable at
	// yesterday's addresses. They seed the DHT routing table, the startup dial,
	// and the relay candidate list.
	RememberedPeers []peer.AddrInfo

	// PublicBootstrap adds the public IPFS DHT bootstrappers alongside (or
	// instead of) the configured rendezvous, so two peers who have never met can
	// still find each other with no Concord-specific server alive. It costs
	// metadata: this node's peer ID and addresses become visible to a public
	// network. Opt-in only — see NetConfig.PublicDHT.
	PublicBootstrap bool
}

// Host is Concord's networking node.
type Host struct {
	h          host.Host
	serviceTag string
	bwc        *metrics.BandwidthCounter // live in/out byte + rate meter
	pinger     *ping.PingService         // on-demand RTT

	ctx    context.Context
	cancel context.CancelFunc

	mdns   interface{ Close() error }
	kdht   interface{ Close() error }
	relays *relaySource

	mu sync.RWMutex
	// relaySvc is the circuit relay we run for guild members while publicly
	// reachable; nil otherwise. Guarded by mu, which serveRelay also holds.
	relaySvc       interface{ Close() error }
	onConnected    []func(peer.ID)
	onDisconnected []func(peer.ID)
	onRedialFailed func(peer.ID)
	// redialReported holds the remembered peers we have already reported as
	// unreachable during the current outage, so the retry loop's backoff cannot
	// turn one absent friend into a stream of failures. Cleared per peer the
	// moment we reach it again.
	redialReported map[peer.ID]bool
}

// New brings up a libp2p host bound to cfg.Identity. The caller owns shutdown
// via Close. mDNS discovery starts immediately when cfg.EnableMDNS is set.
func New(ctx context.Context, cfg Config) (*Host, error) {
	if cfg.Identity == nil {
		return nil, fmt.Errorf("net: Config.Identity is required")
	}

	hostKey := ed25519.PrivateKey(cfg.Identity.PrivateKey())
	if cfg.HostKey != nil {
		hostKey = cfg.HostKey
	}
	priv, err := p2pcrypto.UnmarshalEd25519PrivateKey(hostKey)
	if err != nil {
		return nil, fmt.Errorf("net: convert identity to libp2p key: %w", err)
	}

	listen := cfg.ListenAddrs
	if len(listen) == 0 {
		listen = defaultListenAddrs
	}

	// The relay candidate source is built before the host because AutoRelay may
	// call it during libp2p.New; it learns the host afterwards.
	relays := &relaySource{
		boot:  append([]peer.AddrInfo{}, cfg.BootstrapPeers...),
		known: append([]peer.AddrInfo{}, cfg.RememberedPeers...),
	}

	bwc := metrics.NewBandwidthCounter()
	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(listen...),
		// Encrypt every connection with the Noise protocol.
		libp2p.Security(noise.ID, noise.New),
		libp2p.EnableNATService(),
		// Meter traffic so the Stats panel can show live bandwidth.
		libp2p.BandwidthReporter(bwc),
	}
	// Internet-reach options: hole-punching to establish direct connections
	// through NATs, and AutoRelay — over the rendezvous nodes and, failing
	// those, over friends — as a fallback when no direct path can be found.
	if cfg.EnableDHT {
		opts = append(opts, libp2p.EnableHolePunching())
		if len(cfg.BootstrapPeers) > 0 || len(cfg.RememberedPeers) > 0 {
			opts = append(opts,
				// A peer source rather than WithStaticRelays: static relays pin
				// us to the rendezvous for the life of the process, so when it
				// dies a NAT'd peer has no way to be reached at all. The source
				// keeps offering the rendezvous first, then falls back to
				// friends who happen to have a public address.
				libp2p.EnableAutoRelayWithPeerSource(relays.candidates,
					// WithStaticRelays set this implicitly. Without it AutoRelay
					// sits out a 3-minute boot delay collecting four candidates
					// before reserving anything, which would leave the
					// /p2p-circuit addr in a freshly-generated invite code dead
					// for those three minutes.
					autorelay.WithMinCandidates(1),
				),
				// AutoRelay only reserves a relay slot once AutoNAT concludes we
				// are NAT'd, which with a single bootstrap node as the only
				// AutoNAT server can take minutes or never conclude — leaving the
				// /p2p-circuit addr advertised in invite codes undialable
				// (NO_RESERVATION). Concord peers are desktop machines behind
				// home NATs essentially always, so skip detection and reserve
				// immediately; hole punching still upgrades relayed connections
				// to direct ones.
				libp2p.ForceReachabilityPrivate(),
			)
		}
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("net: start libp2p host: %w", err)
	}

	relays.setHost(h)

	hctx, cancel := context.WithCancel(ctx)
	node := &Host{
		h:          h,
		serviceTag: cfg.ServiceTag,
		bwc:        bwc,
		pinger:     ping.NewPingService(h),
		relays:     relays,
		ctx:        hctx,
		cancel:     cancel,
	}
	if node.serviceTag == "" {
		node.serviceTag = DefaultServiceTag
	}

	node.registerConnEvents()

	if cfg.EnableMDNS {
		// mDNS is best-effort LAN discovery. Its failure must not abort startup:
		// Android's SELinux denies the netlink socket bind zeroconf needs, and
		// locked-down/corporate networks block multicast — in both cases the node
		// still works over DHT + relay. Log and continue.
		if err := node.startMDNS(); err != nil {
			log.Printf("concord/net: mDNS discovery unavailable, continuing without LAN discovery: %v", err)
		}
	}
	if cfg.EnableDHT {
		if err := node.startDHT(cfg); err != nil {
			_ = h.Close()
			cancel()
			return nil, err
		}
		node.serveRelay()
		node.syncRelayService()
	}
	return node, nil
}

// registerConnEvents bridges libp2p's low-level network notifications into
// Concord's peer-level connect/disconnect callbacks. libp2p may open several
// connections to one peer; we only fire on the first connect and last
// disconnect so upper layers see clean per-peer presence transitions.
func (n *Host) registerConnEvents() {
	n.h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(_ network.Network, c network.Conn) {
			p := c.RemotePeer()
			if len(n.h.Network().ConnsToPeer(p)) == 1 {
				n.fire(n.connectedCallbacks(), p)
			}
		},
		DisconnectedF: func(net network.Network, c network.Conn) {
			p := c.RemotePeer()
			if len(net.ConnsToPeer(p)) == 0 {
				n.fire(n.disconnectedCallbacks(), p)
			}
		},
	})
}

// Connect dials a peer by its address info. Used by discovery and by
// invite/rendezvous flows to establish a direct connection.
func (n *Host) Connect(ctx context.Context, pi peer.AddrInfo) error {
	return n.h.Connect(ctx, pi)
}

// Protect marks a peer connection as important so the connection manager won't
// prune it — used to keep guild members (esp. over a relay) reachable.
func (n *Host) Protect(p peer.ID) {
	n.h.ConnManager().Protect(p, relayTag)
}

// PeerID returns this node's libp2p peer ID.
func (n *Host) PeerID() peer.ID { return n.h.ID() }

// Addrs returns the node's current listen multiaddrs (host part only).
func (n *Host) Addrs() []multiaddr.Multiaddr { return n.h.Addrs() }

// AddrInfo returns the full dialable address info other peers use to reach us.
func (n *Host) AddrInfo() peer.AddrInfo {
	return peer.AddrInfo{ID: n.h.ID(), Addrs: n.h.Addrs()}
}

// Peers returns the peer IDs this node is currently connected to.
func (n *Host) Peers() []peer.ID { return n.h.Network().Peers() }

// Host exposes the underlying libp2p host for sibling layers (pubsub, media
// signaling) within package net. Kept unexported-in-spirit by returning the
// interface; callers outside net should use the higher-level methods.
func (n *Host) Libp2p() host.Host { return n.h }

// Bandwidth returns cumulative + live-rate traffic totals across all peers.
func (n *Host) Bandwidth() metrics.Stats {
	if n.bwc == nil {
		return metrics.Stats{}
	}
	return n.bwc.GetBandwidthTotals()
}

// PingRTT measures round-trip time to a connected peer (best-effort; the caller
// should pass a short-timeout context since it sends real packets).
func (n *Host) PingRTT(ctx context.Context, p peer.ID) (time.Duration, error) {
	if n.pinger == nil {
		return 0, fmt.Errorf("net: ping service unavailable")
	}
	select {
	case res := <-n.pinger.Ping(ctx, p):
		return res.RTT, res.Error
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// OnPeerConnected registers a callback fired when a peer first connects.
func (n *Host) OnPeerConnected(fn func(peer.ID)) {
	n.mu.Lock()
	n.onConnected = append(n.onConnected, fn)
	n.mu.Unlock()
}

// OnPeerDisconnected registers a callback fired when a peer fully disconnects.
func (n *Host) OnPeerDisconnected(fn func(peer.ID)) {
	n.mu.Lock()
	n.onDisconnected = append(n.onDisconnected, fn)
	n.mu.Unlock()
}

// OnRedialFailed registers a callback fired when a remembered peer could not be
// re-dialled, so the app layer can retire addresses that have gone stale
// instead of paying for them on every launch.
func (n *Host) OnRedialFailed(fn func(peer.ID)) {
	n.mu.Lock()
	n.onRedialFailed = fn
	n.mu.Unlock()
}

func (n *Host) connectedCallbacks() []func(peer.ID) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]func(peer.ID){}, n.onConnected...)
}

func (n *Host) disconnectedCallbacks() []func(peer.ID) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]func(peer.ID){}, n.onDisconnected...)
}

func (n *Host) fire(cbs []func(peer.ID), p peer.ID) {
	for _, cb := range cbs {
		go cb(p)
	}
}

// redialFailed reports a remembered peer that would not answer — at most once
// per contiguous outage, not once per attempt.
//
// keepBootstrapped re-dials on a 2s/4s/8s/16s backoff, and the app layer retires
// a peer after a handful of failures. Reporting every attempt spends that whole
// budget in about half a minute, so the friend who happens to be asleep on the
// night the rendezvous dies is deleted from the cache — deleting, with no
// in-app way back, the only route to them that did not need a server. The
// budget is meant to count launches (or outages), which is what this makes it
// count.
func (n *Host) redialFailed(p peer.ID) {
	n.mu.Lock()
	fn := n.onRedialFailed
	if fn == nil {
		// The app registers its callback a moment after the host starts, so the
		// first attempt can land here with nobody listening. Don't spend the
		// peer's one report on a call that goes nowhere, or an address that
		// really is dead would never be retired.
		n.mu.Unlock()
		return
	}
	if n.redialReported == nil {
		n.redialReported = map[peer.ID]bool{}
	}
	reported := n.redialReported[p]
	n.redialReported[p] = true
	n.mu.Unlock()
	if reported {
		return
	}
	fn(p)
}

// redialReached re-arms the failure report for a peer we have got back to, so a
// genuinely separate outage later in the same session counts once more.
func (n *Host) redialReached(p peer.ID) {
	n.mu.Lock()
	delete(n.redialReported, p)
	n.mu.Unlock()
}

// Close shuts down discovery and the libp2p host.
func (n *Host) Close() error {
	n.cancel()
	if n.mdns != nil {
		_ = n.mdns.Close()
	}
	if n.kdht != nil {
		_ = n.kdht.Close()
	}
	n.mu.Lock()
	if n.relaySvc != nil {
		_ = n.relaySvc.Close()
		n.relaySvc = nil
	}
	n.mu.Unlock()
	return n.h.Close()
}
