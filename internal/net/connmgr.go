package net

import (
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
)

// connLimits is the connection manager's water marks: trimming starts once the
// node holds more than high connections and sheds down towards low.
type connLimits struct {
	low, high int
	grace     time.Duration
}

// peerConnLimits sizes the connection manager.
//
// Desktop keeps the numbers go-libp2p would have picked anyway. Stating them
// rather than inheriting them is the point: a library default is a decision
// made for a general-purpose DHT node, and it changed under us once already
// without anybody choosing it. Written down, an upgrade that moves the default
// shows up as a diff instead of as a bandwidth report.
//
// Mobile is a quarter of that, and the reasoning runs through what is exempt
// rather than through the marks. Everything Concord cannot afford to lose is
// protected and therefore never trimmed: guild members (Host.Protect, which is
// also the relay ACL), the rendezvous nodes (Host.protectBootstrap — they carry
// the mailbox, so trimming one loses offline delivery), this account's own
// linked devices (Host.ProtectDevice), the relays AutoRelay reserved on
// (go-libp2p protects those itself), the DHT's closest buckets and gossipsub's
// direct peers (the libraries protect those themselves). What the marks govern
// is the remainder: peers the rendezvous key introduced us to and strangers we
// dialled resolving a provider record. Each one costs identify, ping, a
// keepalive and its share of gossipsub gossip, forever, over a metered radio.
//
// 48 rather than something tighter because gossipsub wants D=6 mesh peers per
// topic and Concord opens one topic per channel; the mesh is drawn from guild
// members, who are protected, but a phone in several guilds still legitimately
// holds a few dozen connections before a single stranger is counted. The grace
// period is left at the library's minute: a guild member is only protected once
// the hello handshake completes, and over a relayed circuit that is seconds,
// not milliseconds.
func peerConnLimits(mobile bool) connLimits {
	if mobile {
		return connLimits{low: 32, high: 48, grace: time.Minute}
	}
	return connLimits{low: 160, high: 192, grace: time.Minute}
}

// peerConnCap is how many connections the resource manager lets one peer hold
// at once, in each direction and in total. go-libp2p's default is 8.
//
// Eight is a number about dials, not about trust: it was picked to match the
// swarm's concurrent dials to one peer. Two Concord devices exceed it without
// anybody misbehaving. Each advertises every interface it has (loopback, LAN,
// container bridges, IPv6) over TCP and QUIC, and when both start dialling the
// other at the same moment, as a phone and its desktop do the instant they are
// linked, each side has a dial in flight per address while the other's arrive.
// Every one of those holds a slot. The ninth is refused by the LISTENER after
// the dialer's handshake has already succeeded, so the dialer is handed a
// connection that is closed a moment later (application error 0x1001), and the
// stream it opens on it, a guild join for instance, fails. Measured with
// several loaded copies of the linked-device tests: about one join in five.
//
// Raising the cap does not change what a connection may do, only how many
// spares one peer may hold for the seconds a dial storm lasts; the connection
// manager trims the extras afterwards, and the system-wide limits still bound
// the total.
const peerConnCap = 32

// newResourceManager is go-libp2p's default resource manager (the same scaled
// limits and service limits libp2p.New would have built) with the per-peer
// connection cap above.
func newResourceManager() (network.ResourceManager, error) {
	limits := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&limits)
	limits.PeerBaseLimit.Conns = peerConnCap
	limits.PeerBaseLimit.ConnsInbound = peerConnCap
	limits.PeerBaseLimit.ConnsOutbound = peerConnCap
	return rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limits.AutoScale()))
}
