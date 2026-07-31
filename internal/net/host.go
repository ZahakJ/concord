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
	stdnet "net"
	"strconv"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/net/conngater"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/multiformats/go-multiaddr"

	"github.com/zahak/concord/internal/identity"
)

// DefaultServiceTag namespaces Concord's mDNS discovery so instances only find
// other Concord peers on the LAN, not unrelated libp2p apps.
const DefaultServiceTag = "concord._mdns._v1"

// listenAddrs binds TCP and QUIC on all interfaces. Port 0 takes an ephemeral
// port, which is fine for a node reached over a relay but useless to forward:
// a router rule points at one number, and the number changes every launch.
func listenAddrs(port int) []string {
	return []string{
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port),
		fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", port),
		fmt.Sprintf("/ip6/::/tcp/%d", port),
		fmt.Sprintf("/ip6/::/udp/%d/quic-v1", port),
	}
}

// portFree reports whether the pinned port can still be taken exclusively.
//
// It exists because libp2p will not tell us. go-libp2p's TCP transport sets
// SO_REUSEPORT, so a second Concord binds a port the first one already holds
// and gets no error at all; and Swarm.Listen fails only when *every* listen
// addr fails, so the QUIC bind that did lose the race is swallowed with it.
// Measured with two processes on one pinned port: both hold LISTEN on it, the
// second with no UDP listener whatsoever. Both then advertise
// /ip4/<public>/tcp/<port> under *different* peer IDs, the kernel hands each
// inbound connection to whichever it likes, and a friend dialling the
// forwarded address gets the wrong identity about half the time — the Noise
// peer-ID check then fails with nothing logged and nothing to see.
//
// A plain bind carries no SO_REUSEPORT, so it fails whenever anything holds
// the port, which is exactly the question being asked. IPv4 only: a machine
// with no IPv6 must not be pushed off its pinned port by a probe that could
// never have succeeded there anyway.
func portFree(port int) error {
	l, err := stdnet.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	_ = l.Close()
	p, err := stdnet.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	_ = p.Close()
	return nil
}

// listeningOn reports whether the host really came up on port for both
// transports. The probe above closes its sockets before libp2p binds, so
// something can still slip into the gap; and since Swarm.Listen tolerates
// partial failure, TCP-only counts as success there. It does not count as
// success here: a forward the user opened for TCP and UDP that only half
// exists is worse than no pinned port, because nothing says so.
func listeningOn(h host.Host, port int) bool {
	var tcp, udp bool
	for _, a := range h.Network().ListenAddresses() {
		if addrPort(a) != port {
			continue
		}
		if _, err := a.ValueForProtocol(multiaddr.P_TCP); err == nil {
			tcp = true
		} else {
			udp = true
		}
	}
	return tcp && udp
}

// directAddrs puts this node's public address back into the set it advertises.
//
// It exists because ForceReachabilityPrivate (see New) costs more than the
// comment there admits: go-libp2p's address manager, on autonat v1 and with a
// relay reservation in hand, deletes *every* public direct address from
// Addrs() — measured, and provenance-blind, so a port the router forwards, a
// UPnP mapping and the address identify observed for us all go the same way.
// The node then advertises LAN addresses and a circuit, i.e. everything except
// the one address a friend could actually reach. An AddrsFactory is applied
// after that deletion, so it is where an address can be put back.
//
// The port is what keeps this honest. Only a public address seen on exactly
// the port the user pinned is re-advertised: on a symmetric NAT the outside
// world observes us on a fresh random port per connection, and re-advertising
// those would put a guaranteed-dead address at the front of every joiner's
// dial order. Matching the pinned port means the NAT kept our port — necessary
// for the forward to work, and the strongest evidence available here.
type directAddrs struct {
	mu   sync.RWMutex
	port int
	// all reads the unfiltered address set, before the deletion above.
	// BasicHost provides it; anything else leaves this feature inert.
	all interface{ AllAddrs() []multiaddr.Multiaddr }
}

// attach arms the factory once the host exists (and once the real port is
// known, which a fallback bind can change). Until then it is a no-op, so the
// address set libp2p computes during startup carries no public address — the
// first update after it, reservation included, recomputes with the factory on.
func (d *directAddrs) attach(h host.Host, port int) {
	all, ok := h.(interface{ AllAddrs() []multiaddr.Multiaddr })
	if !ok {
		if port > 0 {
			log.Printf("concord/net: libp2p host does not expose AllAddrs, cannot advertise port %d", port)
		}
		return
	}
	d.mu.Lock()
	d.all, d.port = all, port
	d.mu.Unlock()
}

// addrs re-adds the pinned public address, if there is one.
//
// AllAddrs is one update cycle stale on the path that matters: BasicHost's
// background loop computes fresh local addrs, applies this factory, and only
// *then* stores them, so the set we read here is still the previous cycle's.
// (h.Addrs() applies the factory after the store and does see the current one.)
// A newly observed public address therefore waits one more tick — the loop runs
// every 5s and on every address change — before it is advertised. Left alone
// because it self-heals and the state it lags is unexported: reading it fresh
// would mean reimplementing getLocalAddrs from the outside.
func (d *directAddrs) addrs(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	d.mu.RLock()
	all, port := d.all, d.port
	d.mu.RUnlock()
	if all == nil || port == 0 {
		return addrs
	}
	out := append([]multiaddr.Multiaddr(nil), addrs...)
	for _, a := range publicAddrs(all.AllAddrs()) {
		if addrPort(a) == port {
			out = append(out, a)
		}
	}
	return out
}

// addrPort is the transport port an address carries, or 0.
func addrPort(a multiaddr.Multiaddr) int {
	for _, proto := range []int{multiaddr.P_TCP, multiaddr.P_UDP} {
		if v, err := a.ValueForProtocol(proto); err == nil {
			if p, err := strconv.Atoi(v); err == nil {
				return p
			}
		}
	}
	return 0
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
	// ListenPort pins the TCP and QUIC port instead of taking an ephemeral one,
	// so a router port-forward stays valid across restarts and other people can
	// reach this node directly. 0 = ephemeral, the default. Ignored when
	// ListenAddrs is set, which spells the ports out itself.
	ListenPort int
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

	// BlockedIPs lists IPs this node must treat as unreachable: outbound dials
	// to them are refused and inbound connections from them rejected. It exists
	// so a test can build the topology every real deployment has and no test
	// ever had — two devices behind different NATs whose only path to each
	// other is a relay circuit. Production leaves it empty.
	BlockedIPs []string
}

// Host is Concord's networking node.
type Host struct {
	h          host.Host
	serviceTag string
	bwc        *metrics.BandwidthCounter // live in/out byte + rate meter
	pinger     *ping.PingService         // on-demand RTT

	ctx    context.Context
	cancel context.CancelFunc

	// portTaken records that the user pinned a listen port and did not get it,
	// so the UI can say so. A log line cannot: the fallback keeps the app
	// working, which is precisely why nobody would go looking for one.
	portTaken bool

	mdns interface{ Close() error }
	kdht interface{ Close() error }
	// disc is the same DHT, typed for lookups (FindPeer). nil without the DHT.
	disc   *dht.IpfsDHT
	relays *relaySource

	mu sync.RWMutex
	// relaySvc is the circuit relay we run for guild members while publicly
	// reachable; nil otherwise. Guarded by mu, which serveRelay also holds.
	relaySvc       interface{ Close() error }
	onConnected    []func(peer.ID)
	onDisconnected []func(peer.ID)
	onRedialFailed func(peer.ID)
	// connected is the set of peers upper layers have been told about, so a
	// connect fires exactly once per peer no matter how many transports carry
	// it, and the matching disconnect fires only if the connect did.
	connected map[peer.ID]bool
	// kick is closed-and-replaced when we regain a way into the network, so the
	// advertise and discovery loops restart immediately instead of waiting out a
	// timer scheduled while we were offline. See netKick/kickNetwork.
	kick chan struct{}
	// redialReported holds the remembered peers we have already reported as
	// unreachable during the current outage, so the retry loop's backoff cannot
	// turn one absent friend into a stream of failures. Cleared per peer the
	// moment we reach it again.
	redialReported map[peer.ID]bool

	// background: the mobile shell says the app is off screen, so the periodic
	// discovery loops stretch to backgroundBeat (see SetBackground in dht.go).
	// Guarded by mu.
	background bool
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

	// An explicit ListenAddrs spells its own ports out, so ListenPort has
	// nothing left to pin — and nothing to re-advertise either.
	fixedPort := cfg.ListenPort
	listen := cfg.ListenAddrs
	if len(listen) == 0 {
		listen = listenAddrs(fixedPort)
	} else {
		fixedPort = 0
	}

	// Ask the kernel before libp2p does, because libp2p's answer is unreliable
	// in the one case that matters — see portFree.
	portTaken := false
	if fixedPort > 0 {
		if err := portFree(fixedPort); err != nil {
			log.Printf("concord/net: port %d is already in use (%v), listening on an ephemeral port instead", fixedPort, err)
			portTaken, fixedPort = true, 0
			listen = listenAddrs(0)
		}
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
		// Encrypt every connection with the Noise protocol.
		libp2p.Security(noise.ID, noise.New),
		libp2p.EnableNATService(),
		// Meter traffic so the Stats panel can show live bandwidth.
		libp2p.BandwidthReporter(bwc),
	}
	// A simulated NAT, for tests only — see Config.BlockedIPs.
	if len(cfg.BlockedIPs) > 0 {
		gater, gerr := conngater.NewBasicConnectionGater(nil)
		if gerr != nil {
			return nil, fmt.Errorf("net: connection gater: %w", gerr)
		}
		for _, ip := range cfg.BlockedIPs {
			parsed := stdnet.ParseIP(ip)
			if parsed == nil {
				return nil, fmt.Errorf("net: BlockedIPs entry %q is not an IP", ip)
			}
			_ = gater.BlockAddr(parsed)
		}
		opts = append(opts, libp2p.ConnectionGater(gater))
	}
	// The address factory is the only path by which a public address survives
	// ForceReachabilityPrivate (see below and directAddrs); it stays out of the
	// option list entirely unless the user pinned a port.
	direct := &directAddrs{}
	if fixedPort > 0 {
		opts = append(opts, libp2p.AddrsFactory(direct.addrs))
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
				//
				// The price: once the reservation lands, libp2p stops
				// advertising every public direct address we have. directAddrs
				// buys back the one the user pinned a port for.
				libp2p.ForceReachabilityPrivate(),
			)
		}
	}

	h, err := libp2p.New(append(opts, libp2p.ListenAddrStrings(listen...))...)
	if fixedPort > 0 {
		if err == nil && !listeningOn(h, fixedPort) {
			err = fmt.Errorf("only part of port %d came up: %v", fixedPort, h.Network().ListenAddresses())
			_ = h.Close()
		}
		if err != nil {
			// The pinned port is a setting applied at startup, so anything else
			// holding it — a second Concord, an unrelated program — would leave
			// the app unable to start, and the setting that caused it is only
			// reachable from inside the app. Come up on an ephemeral port
			// instead; the user loses direct reachability, not their client.
			log.Printf("concord/net: could not listen on port %d (%v), retrying with an ephemeral port", fixedPort, err)
			portTaken, fixedPort = true, 0
			h, err = libp2p.New(append(opts, libp2p.ListenAddrStrings(listenAddrs(0)...))...)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("net: start libp2p host: %w", err)
	}

	direct.attach(h, fixedPort)
	relays.setHost(h)

	hctx, cancel := context.WithCancel(ctx)
	node := &Host{
		h:          h,
		serviceTag: cfg.ServiceTag,
		bwc:        bwc,
		pinger:     ping.NewPingService(h),
		relays:     relays,
		portTaken:  portTaken,
		ctx:        hctx,
		cancel:     cancel,
		connected:  map[peer.ID]bool{},
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
			// Deliberately not "is this the only connection to p": two
			// transports to the same peer can come up close enough together
			// that both notifications see a count of two, and then the peer
			// connects with nobody upstream ever hearing about it — the app
			// layer records its contact here, so that peer becomes invisible.
			// Our own seen-set has no such window.
			if n.markConnected(p) {
				n.fire(n.connectedCallbacks(), p)
			}
		},
		DisconnectedF: func(net network.Network, c network.Conn) {
			p := c.RemotePeer()
			if len(net.ConnsToPeer(p)) == 0 && n.markDisconnected(p) {
				n.fire(n.disconnectedCallbacks(), p)
			}
		},
	})
}

// markConnected records p as connected, reporting whether this is the first
// notification for it — i.e. whether upper layers should be told.
func (n *Host) markConnected(p peer.ID) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.connected[p] {
		return false
	}
	n.connected[p] = true
	return true
}

// markDisconnected forgets p, reporting whether it had been announced — a
// disconnect for a peer nobody heard connect must stay silent.
func (n *Host) markDisconnected(p peer.ID) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	was := n.connected[p]
	delete(n.connected, p)
	return was
}

// Connect dials a peer by its address info. Used by discovery and by
// invite/rendezvous flows to establish a direct connection.
func (n *Host) Connect(ctx context.Context, pi peer.AddrInfo) error {
	return n.h.Connect(ctx, pi)
}

// newStream opens an application stream to p, explicitly accepting a
// relay-limited connection.
//
// Every Concord protocol must open through here rather than h.NewStream. A
// connection through a relay that enforces per-circuit limits is marked
// "limited", and h.NewStream refuses to use such a connection — it blocks
// waiting for a direct one that, between two devices behind different NATs,
// never comes, and then fails with a deadline error that names neither the
// relay nor the reason. Measured on the relay-only topology in
// relayonly_test.go: two peers whose every stream — hello, history sync, DM,
// call signalling — died that way while the connection between them sat there
// carrying presence heartbeats. Concord's streams are small request/response
// exchanges, exactly what a limited circuit is for, so accept it.
func (n *Host) newStream(ctx context.Context, p peer.ID, proto protocol.ID) (network.Stream, error) {
	return n.h.NewStream(network.WithAllowLimitedConn(ctx, "concord"), p, proto)
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

// PinnedPortTaken reports that the user asked for a fixed listen port and this
// node could not have it, so the router forward they set up leads nowhere until
// the conflict is resolved.
func (n *Host) PinnedPortTaken() bool { return n.portTaken }

// Peers returns the peer IDs this node is currently connected to.
func (n *Host) Peers() []peer.ID { return n.h.Network().Peers() }

// LimitedOnly reports whether every connection we hold to p is a limited
// (relay-metered) one. Such a peer is present — presence events fired, it
// appears in Peers() — but gossipsub will not deliver to it, so anything that
// treats "connected" as "reachable by publish" must ask this first. False for
// a peer with any full connection, and for one with no connection at all.
func (n *Host) LimitedOnly(p peer.ID) bool {
	conns := n.h.Network().ConnsToPeer(p)
	if len(conns) == 0 {
		return false
	}
	for _, c := range conns {
		if !c.Stat().Limited {
			return false
		}
	}
	return true
}

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
