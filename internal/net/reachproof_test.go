package net

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/ZahakJ/concord/internal/identity"
)

func ma(t *testing.T, s string) multiaddr.Multiaddr {
	t.Helper()
	a, err := multiaddr.NewMultiaddr(s)
	if err != nil {
		t.Fatalf("multiaddr %q: %v", s, err)
	}
	return a
}

// TestPublicAddressAloneIsNotProofOfReach is the bug this file exists for.
//
// The relay used to start on manet.IsPublicAddr over our listen addresses. The
// case below is the commonest desktop there is — a machine on a home network
// with IPv6, holding a globally routable address that its router refuses every
// unsolicited packet to — and it passed that test. It then ran a relay, took
// reservations from guild members behind CGNAT, and handed each of them a
// circuit address at a port nobody outside could dial: those friends believed
// they were reachable and stopped looking for a relay that worked.
//
// Nothing about the address distinguishes that machine from a VPS. Only what
// has already arrived does.
func TestPublicAddressAloneIsNotProofOfReach(t *testing.T) {
	filtered := ma(t, "/ip6/2a01:4f8:1c1c:9a2b::1/tcp/4001")
	if !isPublicish(t, filtered) {
		t.Fatal("test premise broken: the filtered address must look public, that is the whole trap")
	}

	// What the old gate fed relayServiceWanted, and what it decided.
	if !relayServiceWanted(isPublicish(t, filtered), false) {
		t.Fatal("test premise broken: the address reading has to say yes here")
	}

	var p inboundProof
	v4, v6 := p.families()
	if v4 || v6 {
		t.Fatal("a node that has listened and heard nothing claimed to have been reached")
	}
	if relayServiceWanted(v4 || v6, false) {
		t.Fatal("a desktop with a routable-looking address but no visitor volunteered as a relay")
	}
}

// isPublicish reports what the old address test would have said, so the case
// above is pinned as one that genuinely fools it rather than one that happens
// to fail for an unrelated reason.
func isPublicish(t *testing.T, a multiaddr.Multiaddr) bool {
	t.Helper()
	return len(publicAddrs([]multiaddr.Multiaddr{a})) == 1
}

func ipnet(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("ParseCIDR %q: %v", cidr, err)
	}
	return n
}

func TestCountsAsInboundAcceptsOnlyAnUnsolicitedArrival(t *testing.T) {
	var (
		lanLocal   = ma(t, "/ip4/192.168.1.7/tcp/4001")
		v6Local    = ma(t, "/ip6/2a01:4f8:1c1c:9a2b::1/tcp/4001")
		wan        = ma(t, "/ip4/93.184.216.34/tcp/51234")
		wanV6      = ma(t, "/ip6/2606:2800:220:1:248:1893:25c8:1946/tcp/51234")
		lanRemote  = ma(t, "/ip4/192.168.1.99/tcp/51234")
		loopRemote = ma(t, "/ip4/127.0.0.1/tcp/51234")
		// The laptop in the next room: a globally routable IPv6 address out of
		// the same /64 the router handed this machine.
		nextRoom = ma(t, "/ip6/2a01:4f8:1c1c:9a2b::99/tcp/51234")
		circuit  = ma(t, "/ip4/93.184.216.5/tcp/4001/p2p/12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN/p2p-circuit")
	)
	// What this machine's interfaces are configured with: a home LAN and the
	// /64 the ISP delegated to it.
	ours := []*net.IPNet{ipnet(t, "192.168.1.0/24"), ipnet(t, "2a01:4f8:1c1c:9a2b::/64")}

	cases := []struct {
		name           string
		dir            network.Direction
		local, remote  multiaddr.Multiaddr
		first          bool
		attached       []*net.IPNet
		wantV4, wantV6 bool
	}{
		{
			// The one that counts. A stranger's packets reached a socket we
			// listen on, and we had never spoken to them.
			name: "unsolicited arrival on a private listener behind a port-forward",
			dir:  network.DirInbound, local: lanLocal, remote: wan, first: true, attached: ours,
			wantV4: true,
		},
		{
			name: "unsolicited arrival over IPv6",
			dir:  network.DirInbound, local: v6Local, remote: wanV6, first: true, attached: ours,
			wantV6: true,
		},
		{
			// The false positive that the naive public-address test cannot see.
			// Both machines hold globally routable addresses out of the same
			// delegated /64, so this connection is "from a public address" and
			// yet crossed nothing but the switch in the hall.
			name: "the laptop in the next room, over the home /64",
			dir:  network.DirInbound, local: v6Local, remote: nextRoom, first: true, attached: ours,
		},
		{
			// Same address, once the machine is no longer on that network: now
			// it genuinely arrived from elsewhere.
			name: "that same address once we have moved off its network",
			dir:  network.DirInbound, local: v6Local, remote: nextRoom, first: true,
			attached: []*net.IPNet{ipnet(t, "10.0.0.0/8")},
			wantV6:   true,
		},
		{
			// A default route configured on an interface must not swallow the
			// whole internet and silence every arrival.
			name: "a zero-length local prefix does not attach everything",
			dir:  network.DirInbound, local: lanLocal, remote: wan, first: true,
			attached: []*net.IPNet{ipnet(t, "0.0.0.0/0"), ipnet(t, "::/0")},
			wantV4:   true,
		},
		{
			// Our own dial. Proves the firewall lets traffic out, which every
			// firewall on earth does.
			name: "we dialled them",
			dir:  network.DirOutbound, local: lanLocal, remote: wan, first: true, attached: ours,
		},
		{
			// The hole-punch exclusion. DCUtR only ever runs over a relayed
			// connection we are already holding to that peer, so a punched
			// connection is never the first one to it. Without this clause a
			// NAT'd machine certifies itself off a connection that only exists
			// because both ends fired packets simultaneously.
			name: "a second connection to a peer we already had one to",
			dir:  network.DirInbound, local: lanLocal, remote: wan, first: false, attached: ours,
		},
		{
			name: "a neighbour on our own LAN",
			dir:  network.DirInbound, local: lanLocal, remote: lanRemote, first: true, attached: ours,
		},
		{
			name: "a peer on this very machine",
			dir:  network.DirInbound, local: lanLocal, remote: loopRemote, first: true, attached: ours,
		},
		{
			// A circuit's remote address carries the RELAY's IP, which is
			// public and would otherwise sail straight through the test.
			name: "arrived through somebody else's relay",
			dir:  network.DirInbound, local: lanLocal, remote: circuit, first: true, attached: ours,
		},
		{
			name: "our own end is a circuit",
			dir:  network.DirInbound, local: circuit, remote: wan, first: true, attached: ours,
		},
		{
			name: "nothing at all",
			dir:  network.DirInbound, local: nil, remote: nil, first: true, attached: ours,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v4, v6 := countsAsInbound(c.dir, c.local, c.remote, c.first, c.attached)
			if v4 != c.wantV4 || v6 != c.wantV6 {
				t.Fatalf("countsAsInbound = (v4=%v, v6=%v), want (v4=%v, v6=%v)", v4, v6, c.wantV4, c.wantV6)
			}
		})
	}
}

func TestInboundProofAccumulatesAndReportsNews(t *testing.T) {
	var p inboundProof

	if !p.note(true, false) {
		t.Fatal("the first IPv4 arrival was not reported as news; the relay would never be re-evaluated")
	}
	if p.note(true, false) {
		t.Fatal("a second IPv4 arrival was reported as news; the relay would be re-evaluated per visitor")
	}
	if v4, v6 := p.families(); !v4 || v6 {
		t.Fatalf("families = (%v, %v) after one IPv4 arrival, want (true, false)", v4, v6)
	}
	if !p.note(false, true) {
		t.Fatal("the first IPv6 arrival was not news even though IPv4 was all we had")
	}
	if v4, v6 := p.families(); !v4 || !v6 {
		t.Fatalf("families = (%v, %v) after both, want (true, true)", v4, v6)
	}
}

func TestInboundProofIsScopedToTheAddressesThatEarnedIt(t *testing.T) {
	var p inboundProof

	// Adopting a fingerprint for the first time discards nothing.
	if p.rebind("home", nil) {
		t.Fatal("the first rebind claimed to have thrown evidence away")
	}
	p.note(true, false)
	if p.rebind("home", nil) {
		t.Fatal("re-binding the same addresses discarded the evidence for them")
	}
	if v4, _ := p.families(); !v4 {
		t.Fatal("evidence for the current addresses was dropped by an unchanged rebind")
	}

	// The laptop left the building.
	if !p.rebind("cafe", nil) {
		t.Fatal("moving to a different address set did not report discarding the old evidence")
	}
	if v4, v6 := p.families(); v4 || v6 {
		t.Fatal("evidence earned on one network was still credited on another")
	}
	if relayServiceWanted(false, false) {
		t.Fatal("a node that has proven nothing on this network volunteered as a relay")
	}

	p.note(false, true)
	if !p.forget() {
		t.Fatal("forget did not report dropping the evidence it held")
	}
	if v4, v6 := p.families(); v4 || v6 {
		t.Fatal("forget left evidence behind")
	}
	if p.forget() {
		t.Fatal("forgetting nothing reported dropping something")
	}
}

func TestListenFingerprintIgnoresLoopbackAndOrder(t *testing.T) {
	a := []multiaddr.Multiaddr{
		ma(t, "/ip4/127.0.0.1/tcp/4001"),
		ma(t, "/ip4/192.168.1.7/tcp/4001"),
		ma(t, "/ip6/2001:db8::1/tcp/4001"),
	}
	b := []multiaddr.Multiaddr{
		ma(t, "/ip6/2001:db8::1/tcp/4001"),
		ma(t, "/ip6/::1/tcp/4001"),
		ma(t, "/ip4/192.168.1.7/tcp/4001"),
	}
	if listenFingerprint(a) != listenFingerprint(b) {
		t.Fatalf("the same addresses in a different order, plus loopback, changed the fingerprint:\n%q\n%q",
			listenFingerprint(a), listenFingerprint(b))
	}
	moved := []multiaddr.Multiaddr{ma(t, "/ip4/10.0.0.4/tcp/4001")}
	if listenFingerprint(a) == listenFingerprint(moved) {
		t.Fatal("a different address set produced the same fingerprint, so evidence would survive a move")
	}
	if listenFingerprint(nil) != "" {
		t.Fatal("no addresses should fingerprint as nothing")
	}
}

// TestLoopbackPeersProveNothing drives the real notifier. Two in-process hosts
// connect over loopback: the connection is genuinely inbound on one side, and
// the proof must stay empty anyway — the whole point is that only the public
// internet counts, and a test rig is not it.
//
// It is also the wiring test for the notifier itself: if ConnectedF stopped
// consulting the connection's direction or addresses this would go on passing,
// so the second half asserts the classifier would have counted the same
// connection had it come from a public address.
func TestLoopbackPeersProveNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	connected := make(chan struct{}, 1)
	a.OnPeerConnected(func(_ peer.ID) { connected <- struct{}{} })
	if err := b.Connect(ctx, a.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	select {
	case <-connected:
	case <-time.After(15 * time.Second):
		t.Fatal("the hosts never connected")
	}

	if v4, v6 := a.InboundProven(); v4 || v6 {
		t.Fatalf("a loopback peer was accepted as proof the internet can get in (v4=%v v6=%v)", v4, v6)
	}

	// The connection A accepted, replayed through the classifier with the
	// remote address rewritten to a public one: it must count, or the test
	// above is passing for the wrong reason.
	conns := a.h.Network().ConnsToPeer(b.PeerID())
	if len(conns) == 0 {
		t.Fatal("no connection to inspect")
	}
	c := conns[0]
	if c.Stat().Direction != network.DirInbound {
		t.Fatalf("A's connection is %v, expected inbound — the rig, not the code, is wrong", c.Stat().Direction)
	}
	// The family depends on which of A's addresses B happened to pick, so the
	// assertion is that it counted at all.
	v4, v6 := countsAsInbound(c.Stat().Direction, c.LocalMultiaddr(), ma(t, "/ip4/93.184.216.34/tcp/51234"), true, a.inbound.attached())
	if !v4 && !v6 {
		t.Fatalf("the same inbound connection from a public address would not have counted either (local %s)", c.LocalMultiaddr())
	}
}
