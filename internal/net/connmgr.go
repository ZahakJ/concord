package net

import "time"

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
