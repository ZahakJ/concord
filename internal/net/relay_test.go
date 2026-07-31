package net

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/zahak/concord/internal/identity"
)

func addrInfo(t *testing.T, id string, addrs ...string) peer.AddrInfo {
	t.Helper()
	pi := peer.AddrInfo{ID: peer.ID(id)}
	for _, a := range addrs {
		ma, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			t.Fatalf("multiaddr %q: %v", a, err)
		}
		pi.Addrs = append(pi.Addrs, ma)
	}
	return pi
}

// drainCandidates reads a candidate channel to completion. AutoRelay calls the
// source from a goroutine it reuses, so a source that blocks on a send or never
// closes takes relay discovery down with it for the rest of the process — the
// deadline here is the assertion, not a convenience.
func drainCandidates(t *testing.T, ch <-chan peer.AddrInfo) []peer.AddrInfo {
	t.Helper()
	var got []peer.AddrInfo
	deadline := time.After(2 * time.Second)
	for {
		select {
		case pi, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, pi)
		case <-deadline:
			t.Fatal("candidates neither filled nor closed the channel")
		}
	}
}

func TestRelayCandidatesHonourTheLimit(t *testing.T) {
	s := &relaySource{}
	for i := 0; i < 10; i++ {
		s.boot = append(s.boot, addrInfo(t, fmt.Sprintf("node-%d", i), "/ip4/1.2.3.4/tcp/4001"))
	}

	for _, num := range []int{0, 1, 3, 9, 10, 25} {
		got := drainCandidates(t, s.candidates(context.Background(), num))
		if len(got) > num {
			t.Fatalf("asked for at most %d candidates, got %d", num, len(got))
		}
		if want := min(num, 10); len(got) != want {
			t.Fatalf("asked for %d candidates, want %d offered, got %d", num, want, len(got))
		}
	}
}

// TestRelayCandidatesFilterAndDedupe pins what the candidate list must not do:
// offer the same peer twice (AutoRelay would hold one reservation and count it
// as two), or offer a REMEMBERED peer only reachable on the LAN — a relay
// behind the same NAT we are is no help. Configured rendezvous nodes are the
// exception: the user typed that address in deliberately, so it is offered
// exactly as given, private or not — a LAN- or loopback-hosted rendezvous is a
// legitimate deployment, and filtering it left relay-only topologies silently
// reservation-less.
func TestRelayCandidatesFilterAndDedupe(t *testing.T) {
	s := &relaySource{
		boot: []peer.AddrInfo{
			addrInfo(t, "rendezvous", "/ip4/1.2.3.4/tcp/4001"),
			addrInfo(t, "lan-rendezvous", "/ip4/192.168.1.9/tcp/4001"),
		},
		known: []peer.AddrInfo{
			addrInfo(t, "rendezvous", "/ip4/1.2.3.4/tcp/4001"),
			addrInfo(t, "friend", "/ip4/10.0.0.4/tcp/4001", "/ip4/8.8.8.8/tcp/4001"),
			addrInfo(t, "friend-behind-nat", "/ip4/10.0.0.5/tcp/4001"),
		},
	}

	got := drainCandidates(t, s.candidates(context.Background(), 8))
	if len(got) != 3 {
		t.Fatalf("want both rendezvous nodes and the one public friend, got %v", got)
	}
	if got[0].ID != peer.ID("rendezvous") {
		t.Fatalf("want the rendezvous offered first, got %s", got[0].ID)
	}
	if got[1].ID != peer.ID("lan-rendezvous") {
		t.Fatalf("want the LAN rendezvous offered as configured, got %s", got[1].ID)
	}
	if got[2].ID != peer.ID("friend") {
		t.Fatalf("want the publicly-addressed friend last, got %s", got[2].ID)
	}
	if len(got[2].Addrs) != 1 || got[2].Addrs[0].String() != "/ip4/8.8.8.8/tcp/4001" {
		t.Fatalf("want the friend's LAN address stripped, got %v", got[2].Addrs)
	}
}

// TestRelaySourceLearnsANewRendezvous covers the "live" half of the doc comment:
// changing the rendezvous in-app has to reach AutoRelay, or it keeps offering
// the address the user just replaced until the process restarts.
func TestRelaySourceLearnsANewRendezvous(t *testing.T) {
	id, _ := identity.Generate()
	h := newTestHost(t, id)

	if got := drainCandidates(t, h.relays.candidates(context.Background(), 4)); len(got) != 0 {
		t.Fatalf("a host with no rendezvous offered %v", got)
	}

	h.SetBootstrapPeers([]peer.AddrInfo{addrInfo(t, "new-rendezvous", "/ip4/1.2.3.5/tcp/4001")})

	got := drainCandidates(t, h.relays.candidates(context.Background(), 4))
	if len(got) != 1 || got[0].ID != peer.ID("new-rendezvous") {
		t.Fatalf("want the newly-set rendezvous offered, got %v", got)
	}
}

// TestDialableAddrsRejectsUnreachedHosts is the anti-abuse property: identify
// lets a peer claim any address it likes, and whatever we return here is written
// to disk and re-dialled on every launch for a month. A claim at a host we have
// never exchanged a packet with is an attacker aiming our users somewhere.
func TestDialableAddrsRejectsUnreachedHosts(t *testing.T) {
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	if err := a.Connect(context.Background(), b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	bogus, err := multiaddr.NewMultiaddr("/ip4/93.184.216.34/tcp/443")
	if err != nil {
		t.Fatalf("multiaddr: %v", err)
	}
	a.Libp2p().Peerstore().AddAddr(b.PeerID(), bogus, time.Hour)

	for _, got := range a.DialableAddrs(b.PeerID()) {
		if got.Equal(bogus) {
			t.Fatalf("remembered an address the peer merely claimed: %s", got)
		}
	}
}

// TestAddrsAtHostsKeepsClaimedPorts is the other side of it: the connection's
// own remote address cannot be the whole answer, because on an inbound
// connection it carries an ephemeral source port that is dead by the next
// launch. The peer's claimed listen ports are kept — at the host we reached it.
func TestAddrsAtHostsKeepsClaimedPorts(t *testing.T) {
	claimed := []multiaddr.Multiaddr{
		multiaddr.StringCast("/ip4/8.8.8.8/tcp/4001"),
		multiaddr.StringCast("/ip4/8.8.8.8/udp/4001/quic-v1"),
		multiaddr.StringCast("/ip4/93.184.216.34/tcp/443"),
		multiaddr.StringCast("/dns4/victim.example/tcp/443"),
	}

	got := addrsAtHosts(claimed, map[string]bool{"8.8.8.8": true})
	if len(got) != 2 {
		t.Fatalf("want both listen addresses at the observed host, got %v", got)
	}
	for _, a := range got {
		if addrHost(a) != "8.8.8.8" {
			t.Fatalf("kept an address at an unreached host: %s", a)
		}
	}
	if len(addrsAtHosts(claimed, nil)) != 0 {
		t.Fatal("with no connection to judge by, nothing is worth remembering")
	}
}
