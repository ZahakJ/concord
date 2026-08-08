package net

import (
	"context"
	"log"
	"sync"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// relayTag marks peers whose connections we keep alive: guild members, applied
// by Host.Protect. It doubles as the relay ACL — we carry circuits for people
// we already share a guild with, and for nobody else.
const relayTag = "concord-member"

// relaySource feeds AutoRelay the peers that could carry our inbound traffic.
//
// The rendezvous nodes come first: they are dedicated, always public, and they
// are what invite codes point at. Everything after them exists for the day the
// rendezvous is gone — a friend on an unfiltered connection can relay for us
// just as well, and unlike a server, friends are plural.
//
// It is a live callback rather than a fixed list because the useful set changes
// while the process runs: peers connect, addresses get observed, the user points
// the app at a different rendezvous. AutoRelay re-invokes it whenever it needs
// candidates.
type relaySource struct {
	mu    sync.Mutex
	h     host.Host
	boot  []peer.AddrInfo // rendezvous nodes, offered first
	known []peer.AddrInfo // peers remembered from previous sessions
}

func (s *relaySource) setHost(h host.Host) {
	s.mu.Lock()
	s.h = h
	s.mu.Unlock()
}

// SetBootstrapPeers replaces the rendezvous nodes used for discovery hints and
// as relay candidates. The DHT's own bootstrap set is fixed at startup, so this
// does not re-home discovery — it stops AutoRelay offering a rendezvous the
// user has just replaced, which would otherwise stay in the candidate list for
// the life of the process.
func (n *Host) SetBootstrapPeers(peers []peer.AddrInfo) {
	if n.relays == nil {
		return
	}
	n.relays.mu.Lock()
	n.relays.boot = append([]peer.AddrInfo{}, peers...)
	n.relays.mu.Unlock()
}

// candidates implements autorelay.PeerSource. It must send at most num peers
// and must close the channel, or AutoRelay never asks for candidates again.
// The channel is buffered to num and never oversubscribed, so no send blocks.
func (s *relaySource) candidates(_ context.Context, num int) <-chan peer.AddrInfo {
	s.mu.Lock()
	h, boot, known := s.h, s.boot, s.known
	s.mu.Unlock()

	out := make(chan peer.AddrInfo, num)
	defer close(out)

	sent := 0
	seen := map[peer.ID]bool{}
	if h != nil {
		seen[h.ID()] = true
	}
	// offer reports whether there is still room for more. Candidates found on
	// our own go through a public-address filter: a relay behind the same kind
	// of NAT we are is no help, and a LAN address in the list only wastes a
	// reservation attempt.
	offer := func(id peer.ID, addrs []multiaddr.Multiaddr) bool {
		if sent >= num {
			return false
		}
		if seen[id] || len(addrs) == 0 {
			return true
		}
		seen[id] = true
		out <- peer.AddrInfo{ID: id, Addrs: addrs}
		sent++
		return sent < num
	}

	// The rendezvous nodes are offered exactly as configured, private addresses
	// included. They are the one entry the user typed in by hand, so filtering
	// them for "looks public" second-guesses an explicit choice — and quietly
	// breaks every deliberately-private deployment: a rendezvous on the office
	// LAN, or the loopback one a test builds a relay-only topology around.
	for _, pi := range boot {
		if !offer(pi.ID, pi.Addrs) {
			return out
		}
	}
	for _, pi := range known {
		if !offer(pi.ID, publicAddrs(pi.Addrs)) {
			return out
		}
	}
	if h == nil {
		return out
	}
	// Peers we are talking to right now — including ones we learned about after
	// startup, which is the case the seed lists can never cover.
	//
	// Only ones we have vouched for. Whoever relays for us sees every friend who
	// dials us through the circuit — their peer IDs, their addresses, when they
	// connect — and the circuit address lands in our invite codes and our DHT
	// record, so it is handed to people who never chose it. Offering any
	// connected stranger would let someone who simply advertises the rendezvous
	// key volunteer as our relay and collect the lot. memberACL is already this
	// careful about whom we relay FOR; this is the same care in the other
	// direction.
	for _, p := range h.Network().Peers() {
		if !h.ConnManager().IsProtected(p, relayTag) {
			continue
		}
		if !offer(p, publicAddrs(h.Peerstore().Addrs(p))) {
			return out
		}
	}
	return out
}

// RendezvousRelayResources sizes the circuit-relay service a rendezvous node
// runs. It lives here rather than in cmd/rendezvous so the tests that build a
// stand-in rendezvous carry exactly the production configuration — a test relay
// with library-default resources exhibits failures production doesn't have, and
// hides ones it does.
//
// Limit is nil — no per-circuit duration or byte cap — and that nil is
// load-bearing, not generosity. A relay that advertises ANY limit makes every
// connection through it a "limited" connection on both ends, and go-libp2p
// quarantines those: Connectedness reports Limited instead of Connected, and
// gossipsub (which checks exactly that, pubsub.go processLoop) refuses to
// attach the peer to any mesh. The result, measured on the relay-only topology
// in relayonly_test.go with the previous 1h/512MB limit: the two devices
// connect, presence fires, hole punching fails (as it does for real phones on
// carrier NAT), and then not one pubsub message crosses in either direction —
// no guild messages, no typing, no voice presence — while both sides render
// each other ONLINE. A relayed connection Concord cannot publish over is
// strictly worse than none, so the per-circuit meter goes. Abuse is bounded by
// the caps that remain: total reservations, concurrent circuits, and both
// per-peer and per-IP reservation counts.
func RendezvousRelayResources() relayv2.Resources {
	r := relayv2.DefaultResources()
	r.Limit = nil // unlimited circuits — see above, this is what makes gossipsub work
	r.MaxReservations = 512
	r.MaxCircuits = 64
	r.MaxReservationsPerPeer = 8
	r.MaxReservationsPerIP = 16
	return r
}

// peerRelayResources sizes the relay we run for friends.
//
// Limit is nil for the same reason as RendezvousRelayResources — a limited
// circuit is one gossipsub refuses to ride, so a friend relayed through us
// would show online and hear nothing, which is the exact failure this relay
// exists to prevent. This is somebody's laptop rather than a server, but the
// exposure is narrower than it looks: unlike the rendezvous, every reservation
// and every circuit here passes memberACL — only members of a guild we share
// get either — and the counts stay small. "Strangers spending the machine" is
// handled by the ACL, not by metering our own friends' sessions.
func peerRelayResources() relayv2.Resources {
	r := relayv2.DefaultResources()
	r.Limit = nil // unlimited circuits for guild members — see RendezvousRelayResources
	r.MaxReservations = 32
	r.MaxCircuits = 8
	r.MaxReservationsPerPeer = 4
	r.MaxReservationsPerIP = 4
	return r
}

// serveRelay runs a circuit-v2 relay on this node whenever it looks publicly
// reachable, so a friend stuck behind a NAT can be reached through us when the
// rendezvous is down. It is the other half of relaySource: without somebody
// willing to relay, "friends as relays" is a list of candidates that all refuse.
//
// Two guards keep the cost honest. We only start when an interface we listen on
// carries a routable address — the machines this fires on are VPSes and
// unfiltered connections, not the usual laptop behind a home NAT — and the ACL
// only admits peers Protect has tagged, i.e. members of a guild we share. This
// is not an open relay for the internet.
//
// This cannot use libp2p.EnableRelayService(): that starts the relay on an
// EvtLocalReachabilityChanged of Public, and we deliberately pin reachability to
// Private above so AutoRelay reserves immediately. Watching the address set is
// the equivalent signal for the half we control.
func (n *Host) serveRelay() {
	sub, err := n.h.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated), eventbus.Name("concord-relay"))
	if err != nil {
		return
	}
	go func() {
		defer sub.Close()
		for {
			select {
			case <-n.ctx.Done():
				return
			case _, ok := <-sub.Out():
				if !ok {
					return
				}
				n.syncRelayService()
			}
		}
	}()
}

func (n *Host) syncRelayService() {
	public := n.DirectlyReachable()

	n.relayMu.Lock()
	defer n.relayMu.Unlock()
	switch {
	case public && n.relaySvc == nil:
		svc, err := relayv2.New(n.h, relayv2.WithACL(memberACL{h: n.h}),
			relayv2.WithResources(peerRelayResources()))
		if err != nil {
			log.Printf("concord/net: could not start relay service: %v", err)
			return
		}
		n.relaySvc = svc
		log.Printf("concord/net: publicly reachable, relaying for guild members")
	case !public && n.relaySvc != nil:
		_ = n.relaySvc.Close()
		n.relaySvc = nil
	}
}

// DirectlyReachable reports whether this node has a routable public address —
// the same condition under which it runs the peer-relay service. When true it
// needs no relay reservation of its own (peers can dial it straight), so a
// missing reservation is expected rather than a fault.
//
// Listen addresses, not h.Addrs(): the latter grows to include the external
// address identify observed for us, which a NAT'd node has too. Only an address
// on one of our own interfaces means traffic can actually arrive.
//
// Measured here rather than read off relaySvc, which is the same answer only
// while the DHT is on: serveRelay never runs without it, so a node that pinned
// a forwarded port and configured no rendezvous would have reported itself
// unreachable while strangers were dialling it.
func (n *Host) DirectlyReachable() bool {
	v4, v6 := n.PublicAddrFamilies()
	return v4 || v6
}

// PublicAddrFamilies reports which internet-routable address families this node
// listens on. Split out from DirectlyReachable because the two answers promise
// very different things and only one of them is close to a guarantee.
//
// A public IPv4 address means this machine is not behind NAT at all, so an
// inbound connection arrives unless a firewall on the machine itself refuses it.
//
// A public IPv6 address means far less. Consumer routers hand every device a
// globally routable IPv6 address and then drop unsolicited inbound packets to it
// by default, so the address is routable in principle and unreachable in
// practice; and even where the router allows it, a friend whose network is
// IPv4-only cannot dial an IPv6 address at all. Callers that want to tell a user
// "people can reach you" must not treat the two as the same finding.
//
// Neither answer is a measurement of inbound connectivity — nothing here probes
// from outside. See ReachStatus.Reachable for why AutoNAT cannot supply that.
func (n *Host) PublicAddrFamilies() (v4, v6 bool) {
	listen, err := n.h.Network().InterfaceListenAddresses()
	if err != nil {
		return false, false
	}
	for _, a := range publicAddrs(listen) {
		if _, err := a.ValueForProtocol(multiaddr.P_IP4); err == nil {
			v4 = true
			continue
		}
		if _, err := a.ValueForProtocol(multiaddr.P_IP6); err == nil {
			v6 = true
		}
	}
	return v4, v6
}

// memberACL admits reservations and circuits only from peers tagged by
// Host.Protect, i.e. members of a guild we share. Everyone else is refused, so
// being publicly reachable does not turn a user's machine into free transit for
// strangers.
type memberACL struct{ h host.Host }

func (a memberACL) AllowReserve(p peer.ID, _ multiaddr.Multiaddr) bool {
	return a.h.ConnManager().IsProtected(p, relayTag)
}

func (a memberACL) AllowConnect(src peer.ID, _ multiaddr.Multiaddr, dest peer.ID) bool {
	return a.h.ConnManager().IsProtected(src, relayTag) && a.h.ConnManager().IsProtected(dest, relayTag)
}

// DialableAddrs returns the addresses worth remembering for a peer: public ones
// if it has any, LAN ones otherwise (useful where mDNS is blocked). Loopback and
// relay addresses are dropped — the first only works on this machine, the second
// is a pointer at a third party that may be gone by the next launch.
//
// Only addresses at a host we are genuinely connected to survive. The peerstore
// is largely self-reported: identify lets a peer claim any address it likes, and
// anything we keep here gets written to disk and dialled on every launch for the
// next month — so without this filter a hostile peer picks the addresses our
// users re-dial (/ip4/<victim>/tcp/443), and because its one real address keeps
// connecting, Remember resets the failure count and the planted ones never age
// out. The connection's own remote address is not enough by itself: on an
// inbound connection it carries the peer's ephemeral source port, which is dead
// by the next launch. So we keep the ports the peer claims, at the host the
// packets actually came from.
func (n *Host) DialableAddrs(p peer.ID) []multiaddr.Multiaddr {
	addrs := addrsAtHosts(n.h.Peerstore().Addrs(p), n.connectedHosts(p))
	if pub := publicAddrs(addrs); len(pub) > 0 {
		return pub
	}
	var lan []multiaddr.Multiaddr
	for _, a := range addrs {
		if manet.IsPrivateAddr(a) && !manet.IsIPLoopback(a) && !isRelayAddr(a) {
			lan = append(lan, a)
		}
	}
	return lan
}

// connectedHosts is the set of hosts we currently hold a connection to a peer
// on. Circuit connections are skipped: their remote address is the relay's, and
// vouching for addresses at a third party's host is exactly what this guards
// against.
func (n *Host) connectedHosts(p peer.ID) map[string]bool {
	hosts := map[string]bool{}
	for _, c := range n.h.Network().ConnsToPeer(p) {
		ra := c.RemoteMultiaddr()
		if isRelayAddr(ra) {
			continue
		}
		if host := addrHost(ra); host != "" {
			hosts[host] = true
		}
	}
	return hosts
}

// addrsAtHosts keeps the addresses whose host component is one of hosts.
func addrsAtHosts(addrs []multiaddr.Multiaddr, hosts map[string]bool) []multiaddr.Multiaddr {
	if len(hosts) == 0 {
		return nil
	}
	out := make([]multiaddr.Multiaddr, 0, len(addrs))
	for _, a := range addrs {
		if hosts[addrHost(a)] {
			out = append(out, a)
		}
	}
	return out
}

// addrHost is the address's host component — the IP or name a peer cannot lie
// about once traffic from it has reached us.
func addrHost(a multiaddr.Multiaddr) string {
	for _, proto := range []int{multiaddr.P_IP4, multiaddr.P_IP6, multiaddr.P_DNS, multiaddr.P_DNS4, multiaddr.P_DNS6, multiaddr.P_DNSADDR} {
		if v, err := a.ValueForProtocol(proto); err == nil {
			return v
		}
	}
	return ""
}

func publicAddrs(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	var out []multiaddr.Multiaddr
	for _, a := range addrs {
		if manet.IsPublicAddr(a) && !isRelayAddr(a) {
			out = append(out, a)
		}
	}
	return out
}

func isRelayAddr(a multiaddr.Multiaddr) bool {
	_, err := a.ValueForProtocol(multiaddr.P_CIRCUIT)
	return err == nil
}
