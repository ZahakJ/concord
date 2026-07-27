package net

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/multiformats/go-multiaddr"

	"github.com/zahak/concord/internal/identity"
)

// freePort asks the kernel for a port nothing is using, then hands it back.
// There is an unavoidable race with the rest of the machine; the alternative
// is a hard-coded port that collides with whatever the developer runs.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// The point of the setting: the port a user forwards on their router must be
// the port the node is on, every launch.
func TestListenPortIsPinnedAcrossRestarts(t *testing.T) {
	id, _ := identity.Generate()
	port := freePort(t)

	for range 2 {
		h, err := New(context.Background(), Config{Identity: id, ListenPort: port})
		if err != nil {
			t.Fatalf("New host: %v", err)
		}
		found := false
		for _, a := range h.Addrs() {
			if addrPort(a) == port {
				found = true
			}
		}
		taken := h.PinnedPortTaken()
		_ = h.Close()
		if !found {
			t.Fatalf("want a listen addr on port %d, got %v", port, h.Addrs())
		}
		// The bind checks must not cry wolf: a false positive here demotes every
		// user with a working forward to an ephemeral port, silently.
		if taken {
			t.Fatalf("port %d bound fine but was reported as taken", port)
		}
	}
}

// An explicit ListenAddrs already names its ports; ListenPort must not fight it.
func TestListenAddrsOverridesListenPort(t *testing.T) {
	id, _ := identity.Generate()
	h, err := New(context.Background(), Config{
		Identity:    id,
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
		ListenPort:  4001,
	})
	if err != nil {
		t.Fatalf("New host: %v", err)
	}
	defer h.Close()
	for _, a := range h.Addrs() {
		if addrPort(a) == 4001 {
			t.Fatalf("ListenPort leaked past an explicit ListenAddrs: %v", h.Addrs())
		}
	}
}

// A port another program holds must not stop the app starting — the setting
// that caused it is only reachable from inside the app — and it must not be
// used either. Both halves matter: libp2p's TCP transport sets SO_REUSEPORT and
// Swarm.Listen tolerates partial failure, so "the host came up" says nothing
// about whether the port is really ours.
func TestListenPortFallsBackWhenTaken(t *testing.T) {
	for _, tc := range []struct {
		name string
		hold func(port int) (io.Closer, error)
	}{
		// TCP alone is the case libp2p hides: another SO_REUSEPORT holder would
		// let our TCP bind "succeed" beside it. A plain listener here is the
		// stricter test of the same detection.
		{"tcp", func(port int) (io.Closer, error) {
			return net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
		}},
		// UDP alone is the half that vanishes without a word: TCP binds, QUIC
		// does not, and Swarm.Listen still returns nil.
		{"udp", func(port int) (io.Closer, error) {
			return net.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", port))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, _ := identity.Generate()
			port := freePort(t)
			blocker, err := tc.hold(port)
			if err != nil {
				t.Skipf("cannot hold port %d: %v", port, err)
			}
			defer blocker.Close()

			h, err := New(context.Background(), Config{Identity: id, ListenPort: port})
			if err != nil {
				t.Fatalf("a taken port must not stop the host coming up: %v", err)
			}
			defer h.Close()
			if len(h.Addrs()) == 0 {
				t.Fatal("host came up with no addresses")
			}
			for _, a := range h.Libp2p().Network().ListenAddresses() {
				if addrPort(a) == port {
					t.Fatalf("still listening on the taken port %d: %v", port, h.Libp2p().Network().ListenAddresses())
				}
			}
			if !h.PinnedPortTaken() {
				t.Fatal("the fallback must be reportable; nothing else tells the user")
			}
		})
	}
}

// Two Concords on one machine, both told to pin the same port. Without the bind
// checks the second one comes up sharing the first one's TCP port under its own
// peer ID, the kernel splits inbound connections between them, and a friend
// dialling the forwarded address lands on the wrong identity about half the
// time — the Noise peer-ID check then fails with nothing logged.
func TestSecondHostDoesNotShareThePinnedPort(t *testing.T) {
	id1, _ := identity.Generate()
	id2, _ := identity.Generate()
	port := freePort(t)

	first, err := New(context.Background(), Config{Identity: id1, ListenPort: port})
	if err != nil {
		t.Fatalf("New host: %v", err)
	}
	defer first.Close()
	second, err := New(context.Background(), Config{Identity: id2, ListenPort: port})
	if err != nil {
		t.Fatalf("New second host: %v", err)
	}
	defer second.Close()

	for _, a := range second.Libp2p().Network().ListenAddresses() {
		if addrPort(a) == port {
			t.Fatalf("two hosts on port %d: %v", port, a)
		}
	}
	if !second.PinnedPortTaken() {
		t.Fatal("the second host took the port without reporting it")
	}
}

// directAddrs is the whole reason a forwarded port is worth anything: without
// it, libp2p deletes every public address once a relay reservation lands.
func TestDirectAddrsReinjectsOnlyThePinnedPort(t *testing.T) {
	const port = 4001
	d := &directAddrs{port: port, all: fakeAllAddrs{
		"/ip4/93.184.216.34/tcp/4001",         // the forward
		"/ip4/93.184.216.34/udp/4001/quic-v1", // ditto
		"/ip4/93.184.216.34/tcp/51823",        // symmetric NAT: observed on a throwaway port
		"/ip4/192.168.1.23/tcp/4001",          // LAN, already advertised
	}}

	got := d.addrs([]multiaddr.Multiaddr{multiaddr.StringCast("/ip4/192.168.1.23/tcp/4001")})
	var s []string
	for _, a := range got {
		s = append(s, a.String())
	}
	joined := strings.Join(s, " ")
	for _, want := range []string{"/ip4/93.184.216.34/tcp/4001", "/ip4/93.184.216.34/udp/4001/quic-v1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("public addr on the pinned port must be re-advertised, got %v", s)
		}
	}
	if strings.Contains(joined, "51823") {
		t.Fatalf("an observed throwaway port is a dead address, got %v", s)
	}
}

// directAddrs reaches the unfiltered address set through a type assertion on
// libp2p's concrete host. A libp2p upgrade that wraps it would leave direct
// reachability silently doing nothing.
func TestHostStillExposesUnfilteredAddrs(t *testing.T) {
	id, _ := identity.Generate()
	h, err := New(context.Background(), Config{Identity: id})
	if err != nil {
		t.Fatalf("New host: %v", err)
	}
	defer h.Close()
	if _, ok := h.Libp2p().(interface{ AllAddrs() []multiaddr.Multiaddr }); !ok {
		t.Fatal("libp2p host no longer exposes AllAddrs; directAddrs is inert")
	}
}

func TestDirectAddrsInertWithoutAPinnedPort(t *testing.T) {
	d := &directAddrs{all: fakeAllAddrs{"/ip4/93.184.216.34/tcp/4001"}}
	in := []multiaddr.Multiaddr{multiaddr.StringCast("/ip4/192.168.1.23/tcp/4001")}
	if got := d.addrs(in); len(got) != 1 {
		t.Fatalf("want the address set untouched, got %v", got)
	}
}

type fakeAllAddrs []string

func (f fakeAllAddrs) AllAddrs() []multiaddr.Multiaddr {
	out := make([]multiaddr.Multiaddr, 0, len(f))
	for _, a := range f {
		out = append(out, multiaddr.StringCast(a))
	}
	return out
}
