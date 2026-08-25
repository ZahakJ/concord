package net

import (
	stdnet "net"
	"sort"
	"strings"
	"sync"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// Proof that the internet can get in.
//
// PublicAddrFamilies answers "does an interface we listen on carry an address
// that PARSES as routable". That is not the same question as "can a stranger
// open a connection to it", and on the commonest desktop of all — a machine on
// a home network with IPv6 — the two answers differ. The router hands out a
// globally routable /64 address and then drops every unsolicited inbound packet
// to it, which is the default on essentially every consumer router sold. The
// address is real, routable, and unreachable.
//
// Believing the address there is worse than having no answer, because of what
// the relay does with it: we start a circuit-relay service, a guild member
// behind CGNAT reserves a slot on us over the connection THEY opened, and the
// circuit address they then hand out in their invite codes and their DHT record
// points at a port nobody outside can reach. They believe they are reachable.
// They are not, and the reservation they are holding is the reason they stopped
// looking for one that works.
//
// So the relay stops asking what our address looks like and starts asking what
// has already happened to us. One inbound, direct, non-relayed connection from a
// public remote address is not an inference — it is a packet that arrived. It
// proves the path exists for that address family, on this network, right now.

// inboundProof records which address families have been reached from the public
// internet since the current set of listen addresses came up.
//
// Scoped to the address set on purpose, rather than aged out on a timer. What
// invalidates the proof is the machine moving — a laptop leaving the office,
// a VPS renumbered, an interface coming or going — and that is exactly what
// changes the listen-address set and fires EvtLocalAddressesUpdated, which is
// already the relay's trigger. A timer would instead expire the proof on a
// quiet-but-perfectly-reachable node and stop it relaying for no reason, then
// restart it on the next visitor: churn that tells the user's friends nothing
// except that our relay flaps.
type inboundProof struct {
	mu    sync.Mutex
	addrs string // fingerprint of the listen-address set this evidence belongs to
	// nets are the networks directly attached to this machine as of that
	// address set: the ones a packet reaches without passing the router, and
	// therefore without passing whatever the router does to unsolicited inbound
	// traffic. Anything arriving from one of them proves nothing. See
	// fromAttachedNetwork.
	nets []*stdnet.IPNet
	v4   bool
	v6   bool
}

// note records that an inbound connection arrived on one or both families.
// Reports whether this was news, so the caller can re-evaluate the relay only
// when something actually changed rather than on every visitor.
func (p *inboundProof) note(v4, v6 bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	fresh := (v4 && !p.v4) || (v6 && !p.v6)
	p.v4 = p.v4 || v4
	p.v6 = p.v6 || v6
	return fresh
}

// families reports which address families have been proven.
func (p *inboundProof) families() (v4, v6 bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.v4, p.v6
}

// rebind discards the evidence if it belongs to a different set of listen
// addresses than the one we have now, reporting whether it threw anything away.
// The first call simply adopts the fingerprint. nets comes along because the
// networks attached to this machine change with exactly the same event.
func (p *inboundProof) rebind(fingerprint string, nets []*stdnet.IPNet) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nets = nets
	if p.addrs == fingerprint {
		return false
	}
	had := p.v4 || p.v6
	p.addrs = fingerprint
	p.v4, p.v6 = false, false
	return had
}

// attached is the directly-attached network list the evidence is being judged
// against right now.
func (p *inboundProof) attached() []*stdnet.IPNet {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nets
}

// forget drops the evidence outright, keeping the fingerprint. For the moment
// we lose every route out and find one again: whatever reached us belonged to
// a stretch of network that has since ended, and re-proving costs one visitor.
func (p *inboundProof) forget() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	had := p.v4 || p.v6
	p.v4, p.v6 = false, false
	return had
}

// countsAsInbound reports which address family, if any, a new connection proves
// inbound reachability for. Every clause is load-bearing:
//
//   - Inbound direction. An outbound dial proves our firewall lets traffic OUT,
//     which every firewall does.
//
//   - Not a circuit. A relayed connection arrives through somebody else's
//     socket; it says what the relay is reachable at, not us. Both ends are
//     checked because a circuit's remote address carries the relay's IP, which
//     is usually public and would otherwise sail through the test below.
//
//   - A public remote address, on a network that is not one of ours. The first
//     half is obvious; the second half is the trap. Every device on a home
//     network with IPv6 holds a globally routable address out of the same /64,
//     so a laptop dialling a desktop one room away arrives from an address that
//     is public by every test — while the packets went over the LAN switch and
//     never met the router that would have dropped them coming from outside.
//     That is precisely the machine this whole file exists to stop certifying
//     itself, and the address alone cannot tell the two cases apart. What can is
//     the interface netmask: a remote inside a prefix configured on one of our
//     own interfaces is directly attached, and reached us without a router
//     deciding anything.
//
//   - The FIRST connection to that peer. This is the hole-punch exclusion, and
//     it is the whole reason the naive test is not enough. DCUtR only ever runs
//     over an existing relayed connection, and the punched connection can land
//     on us as an inbound one — so a machine that is reachable only because both
//     sides fired packets at each other simultaneously would otherwise certify
//     itself as publicly reachable and start relaying. A peer we have never
//     spoken to, arriving directly, had nothing to punch through.
//
// The family is read from OUR side of the connection — the listener that
// accepted it — not from the remote. That is what lets a home server behind a
// port-forward count: its own address is a private 192.168.x.x, so the
// address-parsing test called it unreachable, while packets from the internet
// were arriving on it all day.
func countsAsInbound(dir network.Direction, local, remote multiaddr.Multiaddr, firstToPeer bool, attached []*stdnet.IPNet) (v4, v6 bool) {
	if dir != network.DirInbound || !firstToPeer || local == nil || remote == nil {
		return false, false
	}
	if isRelayAddr(local) || isRelayAddr(remote) {
		return false, false
	}
	if !manet.IsPublicAddr(remote) || fromAttachedNetwork(remote, attached) {
		return false, false
	}
	if _, err := local.ValueForProtocol(multiaddr.P_IP4); err == nil {
		return true, false
	}
	if _, err := local.ValueForProtocol(multiaddr.P_IP6); err == nil {
		return false, true
	}
	return false, false
}

// fromAttachedNetwork reports whether a remote address sits inside a prefix
// configured on one of this machine's own interfaces, i.e. whether it could
// have reached us without any router forwarding it.
//
// A zero-length prefix is ignored: a default route configured on an interface
// would otherwise make every address on the internet "attached" and silence the
// evidence entirely.
func fromAttachedNetwork(remote multiaddr.Multiaddr, attached []*stdnet.IPNet) bool {
	if len(attached) == 0 {
		return false
	}
	ip, err := manet.ToIP(remote)
	if err != nil || ip == nil {
		return false
	}
	for _, n := range attached {
		if n == nil {
			continue
		}
		if ones, _ := n.Mask.Size(); ones == 0 {
			continue
		}
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// attachedNetworks reads the prefixes configured on this machine's interfaces.
// Read at the same moments the listen-address set is re-fingerprinted, because
// an interface coming or going changes both.
func attachedNetworks() []*stdnet.IPNet {
	addrs, err := stdnet.InterfaceAddrs()
	if err != nil {
		return nil
	}
	out := make([]*stdnet.IPNet, 0, len(addrs))
	for _, a := range addrs {
		if n, ok := a.(*stdnet.IPNet); ok && n != nil {
			out = append(out, n)
		}
	}
	return out
}

// listenFingerprint identifies the set of addresses we can currently be reached
// at, so evidence gathered at one of them is not credited to another.
//
// Loopback is excluded because it is present on every machine in every state
// and carries no information; everything else is kept, private addresses
// included, since a port-forwarded box is reached at a private one and moving
// it to a different LAN must still invalidate the proof.
func listenFingerprint(addrs []multiaddr.Multiaddr) string {
	var keep []string
	for _, a := range addrs {
		if manet.IsIPLoopback(a) || isRelayAddr(a) {
			continue
		}
		keep = append(keep, a.String())
	}
	sort.Strings(keep)
	return strings.Join(keep, "|")
}
