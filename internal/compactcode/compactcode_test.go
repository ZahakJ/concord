package compactcode

import (
	"fmt"
	"slices"
	"testing"
)

const relay = "/dns/rdv.example.dev/tcp/4001/p2p/12D3KooWCzonwxmETSLwgDY9JAe9WczUHssnYTrSpCqkp6UXZg1q"

// The case that motivated ranking: a laptop with Wi-Fi, ethernet, a Docker
// bridge, a VPN adapter and IPv6 privacy addresses hands libp2p more than
// MaxAddrs addresses, and the forwarded public port is not first in that list.
// Capping the raw order truncates away the only address a friend can reach.
func TestRankAddrsSurvivesTheCap(t *testing.T) {
	raw := []string{
		"/ip4/127.0.0.1/tcp/4001",
		"/ip4/192.168.1.23/tcp/4001",
		"/ip4/192.168.1.23/udp/4001/quic-v1",
		"/ip4/172.17.0.1/tcp/4001",
		"/ip4/172.17.0.1/udp/4001/quic-v1",
		"/ip4/10.8.0.6/tcp/4001",
		"/ip4/10.8.0.6/udp/4001/quic-v1",
		"/ip6/fe80::1/tcp/4001",
		"/ip6/fd00::5/tcp/4001",
		relay + "/p2p-circuit",
		"/ip4/93.184.216.34/tcp/4001", // the forwarded port, dead last
	}

	if got := DedupeCap(raw, MaxAddrs); slices.Contains(got, "/ip4/93.184.216.34/tcp/4001") {
		t.Fatalf("test premise broken: the public addr already survives an unranked cap: %v", got)
	}

	got := DedupeCap(RankAddrs(raw), MaxAddrs)
	if len(got) != MaxAddrs {
		t.Fatalf("want %d addrs, got %d: %v", MaxAddrs, len(got), got)
	}
	if got[0] != "/ip4/93.184.216.34/tcp/4001" {
		t.Fatalf("public direct addr must be dialled first, got %q (%v)", got[0], got)
	}
	if got[1] != relay+"/p2p-circuit" {
		t.Fatalf("circuit must follow the public addr, got %q (%v)", got[1], got)
	}
	for _, dropped := range []string{"/ip4/127.0.0.1/tcp/4001", "/ip6/fe80::1/tcp/4001"} {
		if slices.Contains(got, dropped) {
			t.Fatalf("%q reaches nothing from another machine, must be dropped: %v", dropped, got)
		}
	}
}

func TestRankAddrsKeepsUnjudgeableEntriesLast(t *testing.T) {
	got := RankAddrs([]string{"typed-by-hand", "/ip4/192.168.1.23/tcp/4001", "/ip4/93.184.216.34/tcp/4001"})
	want := []string{"/ip4/93.184.216.34/tcp/4001", "/ip4/192.168.1.23/tcp/4001", "typed-by-hand"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRankAddrsKeepsLoopbackWhenItIsAllThereIs(t *testing.T) {
	in := []string{"/ip4/127.0.0.1/tcp/4001", "/ip6/::1/tcp/4001"}
	if got := RankAddrs(in); !slices.Equal(got, in) {
		t.Fatalf("an offline host must still be reachable from itself, got %v", got)
	}
}

// Ranking must not reshuffle addresses of equal usefulness: within a tier the
// host's own order is the best signal there is.
func TestRankAddrsIsStableWithinATier(t *testing.T) {
	in := []string{"/ip4/192.168.1.23/tcp/4001", "/ip4/10.8.0.6/tcp/4001", "/ip4/172.17.0.1/tcp/4001"}
	if got := RankAddrs(in); !slices.Equal(got, in) {
		t.Fatalf("got %v, want %v", got, in)
	}
}

// The elide/restore round-trip must reproduce exactly the circuits the issuer
// listed — not one per rendezvous it happens to carry, which is how a dead
// relay used to end up in every code.
func TestCircuitMaskRestoresOnlyHeldReservations(t *testing.T) {
	dead := "/dns/gone.example.dev/tcp/4001/p2p/12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTA"
	boot := []string{relay, dead}

	kept, mask := ElideCircuits([]string{"/ip4/192.168.1.23/tcp/4001", relay + "/p2p-circuit"}, boot)
	if slices.Contains(kept, relay+"/p2p-circuit") {
		t.Fatalf("derivable circuit should be elided: %v", kept)
	}
	if mask != 1 {
		t.Fatalf("mask should name bootstrap 0 only, got %b", mask)
	}

	got := RestoreCircuits(kept, boot, mask)
	if !slices.Contains(got, relay+"/p2p-circuit") {
		t.Fatalf("held circuit lost: %v", got)
	}
	if slices.Contains(got, dead+"/p2p-circuit") {
		t.Fatalf("circuit for a relay we hold no reservation with must not appear: %v", got)
	}
}

// Codes minted before the mask existed carry no mask; they meant "a circuit
// for every rendezvous" and must keep decoding that way.
func TestRestoreCircuitsLegacyMask(t *testing.T) {
	boot := []string{relay}
	got := RestoreCircuits(nil, boot, AllCircuits)
	if !slices.Equal(got, []string{relay + "/p2p-circuit"}) {
		t.Fatalf("got %v", got)
	}
}

// The mask indexes the bootstrap list, so it cannot describe entries past the
// 64 bits it has; those keep their circuit address verbatim instead.
func TestElideCircuitsBeyondMaskWidth(t *testing.T) {
	var boot []string
	for i := range 66 {
		boot = append(boot, fmt.Sprintf("/ip4/93.184.216.%d/tcp/4001", i))
	}
	far := boot[65] + "/p2p-circuit"
	kept, _ := ElideCircuits([]string{far}, boot)
	if !slices.Contains(kept, far) {
		t.Fatalf("un-maskable circuit must be carried literally: %v", kept)
	}
}
