package app

import (
	"slices"
	"testing"

	"github.com/multiformats/go-multiaddr"
)

const (
	liveRelayID = "12D3KooWCzonwxmETSLwgDY9JAe9WczUHssnYTrSpCqkp6UXZg1q"
	deadRelayID = "12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTA"
	friendID    = "12D3KooWPx4quLj6rDB4b9H9ywbvqepNHVCUt1g27vLiSYbMpcWh"
)

func mas(t *testing.T, addrs ...string) []multiaddr.Multiaddr {
	t.Helper()
	out := make([]multiaddr.Multiaddr, 0, len(addrs))
	for _, a := range addrs {
		ma, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			t.Fatalf("bad test addr %q: %v", a, err)
		}
		out = append(out, ma)
	}
	return out
}

// A configured rendezvous is advertised whether or not the reservation has
// landed yet. Emitting only confirmed ones sounds more honest and is worse: a
// code minted in the seconds after launch would carry LAN addresses only and be
// dead forever for anyone on another network, while a not-yet-ready circuit
// costs one failed dial and works on the retry.
func TestCodeAddrsAdvertisesConfiguredRendezvousBeforeTheReservationLands(t *testing.T) {
	live := "/dns/live.example.dev/tcp/4001/p2p/" + liveRelayID
	dead := "/dns/gone.example.dev/tcp/4001/p2p/" + deadRelayID

	got := codeAddrs(mas(t,
		"/ip4/192.168.1.23/tcp/4001",
		"/ip4/203.0.113.9/tcp/4001/p2p/"+liveRelayID+"/p2p-circuit",
	), []string{live, dead})

	if !slices.Contains(got, live+"/p2p-circuit") {
		t.Fatalf("held reservation must be advertised in its configured form: %v", got)
	}
	if !slices.Contains(got, dead+"/p2p-circuit") {
		t.Fatalf("a configured rendezvous we have not reserved with yet is still the\nonly route a joiner off our LAN has; it must be in the code: %v", got)
	}
	// libp2p's rendering of the same reservation is a snapshot of where the
	// relay answered today; the configured /dns form replaces it.
	if slices.Contains(got, "/ip4/203.0.113.9/tcp/4001/p2p/"+liveRelayID+"/p2p-circuit") {
		t.Fatalf("configured relay should be advertised once, in /dns form: %v", got)
	}
	if !slices.Contains(got, "/ip4/192.168.1.23/tcp/4001") {
		t.Fatalf("direct addrs must pass through untouched: %v", got)
	}
}

// A friend relaying for us is not in anyone's config, so libp2p's rendering is
// the only address there is.
func TestCodeAddrsKeepsUnconfiguredRelaysVerbatim(t *testing.T) {
	circuit := "/ip4/198.51.100.7/tcp/4001/p2p/" + friendID + "/p2p-circuit"
	live := "/dns/live.example.dev/tcp/4001/p2p/" + liveRelayID
	got := codeAddrs(mas(t, circuit), []string{live})
	if !slices.Equal(got, []string{circuit, live + "/p2p-circuit"}) {
		t.Fatalf("got %v", got)
	}
}

// With no rendezvous configured at all the code is LAN-only, and must say so
// rather than inventing a relay path.
func TestCodeAddrsWithoutBootstrap(t *testing.T) {
	got := codeAddrs(mas(t, "/ip4/192.168.1.23/tcp/4001"), nil)
	if !slices.Equal(got, []string{"/ip4/192.168.1.23/tcp/4001"}) {
		t.Fatalf("got %v", got)
	}
}
