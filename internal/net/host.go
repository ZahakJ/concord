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

	"github.com/libp2p/go-libp2p"
	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
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
}

// Host is Concord's networking node.
type Host struct {
	h          host.Host
	serviceTag string

	ctx    context.Context
	cancel context.CancelFunc

	mdns interface{ Close() error }
	kdht interface{ Close() error }

	mu             sync.RWMutex
	onConnected    []func(peer.ID)
	onDisconnected []func(peer.ID)
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

	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(listen...),
		// Encrypt every connection with the Noise protocol.
		libp2p.Security(noise.ID, noise.New),
		libp2p.EnableNATService(),
	}
	// Internet-reach options: hole-punching to establish direct connections
	// through NATs, and AutoRelay (using the bootstrap nodes as relays) as a
	// fallback when a direct path can't be found.
	if cfg.EnableDHT {
		opts = append(opts, libp2p.EnableHolePunching())
		if len(cfg.BootstrapPeers) > 0 {
			opts = append(opts,
				libp2p.EnableAutoRelayWithStaticRelays(cfg.BootstrapPeers),
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

	hctx, cancel := context.WithCancel(ctx)
	node := &Host{
		h:          h,
		serviceTag: cfg.ServiceTag,
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
	n.h.ConnManager().Protect(p, "concord-member")
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

// Close shuts down discovery and the libp2p host.
func (n *Host) Close() error {
	n.cancel()
	if n.mdns != nil {
		_ = n.mdns.Close()
	}
	if n.kdht != nil {
		_ = n.kdht.Close()
	}
	return n.h.Close()
}
