package net

import (
	"time"

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
	// offer reports whether there is still room for more. Every candidate goes
	// through the same public-address filter, seeds included: a relay behind the
	// same kind of NAT we are is no help, and a LAN address in the list only
	// wastes a reservation attempt.
	offer := func(id peer.ID, addrs []multiaddr.Multiaddr) bool {
		if sent >= num {
			return false
		}
		addrs = publicAddrs(addrs)
		if seen[id] || len(addrs) == 0 {
			return true
		}
		seen[id] = true
		out <- peer.AddrInfo{ID: id, Addrs: addrs}
		sent++
		return sent < num
	}

	for _, pi := range boot {
		if !offer(pi.ID, pi.Addrs) {
			return out
		}
	}
	for _, pi := range known {
		if !offer(pi.ID, pi.Addrs) {
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
		if !offer(p, h.Peerstore().Addrs(p)) {
			return out
		}
	}
	return out
}

// peerRelayResources sizes the relay we run for friends.
//
// The library defaults cap a circuit at 128 KiB and two minutes, which is fine
// for a hole-punch handshake and useless for the job this relay exists to do:
// when the rendezvous is gone, a friend's whole session rides this circuit, and
// one image attachment is already past the byte cap. The rendezvous node solved
// the same problem with the same numbers (cmd/rendezvous/main.go) — but it is a
// server, and this is somebody's laptop, so the per-circuit allowance matches
// while the totals are far smaller. Unlimited is not on offer: a relay with no
// ceiling is a machine strangers can spend.
func peerRelayResources() relayv2.Resources {
	r := relayv2.DefaultResources()
	r.Limit = &relayv2.RelayLimit{Duration: time.Hour, Data: 512 << 20}
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
	// Listen addresses, not h.Addrs(): the latter grows to include the external
	// address identify observed for us, which a NAT'd node has too. Only an
	// address on one of our own interfaces means traffic can actually arrive.
	listen, err := n.h.Network().InterfaceListenAddresses()
	if err != nil {
		return
	}
	public := len(publicAddrs(listen)) > 0

	n.mu.Lock()
	defer n.mu.Unlock()
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
