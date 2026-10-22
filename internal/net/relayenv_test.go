package net

// RendezvousRelayResourcesFromEnv exists so a node that relays only signaling
// can cap its circuits. Two things must hold: with nothing set, Concord's own
// rendezvous gets exactly the resources it always had (an accidental limit
// would quietly break gossipsub through it, see RendezvousRelayResources); and
// with a cap set, a circuit that carries more than the cap really is cut.

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/multiformats/go-multiaddr"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRelayEnvUnsetIsConcordsOwn(t *testing.T) {
	got, err := RendezvousRelayResourcesFromEnv(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := RendezvousRelayResources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("with no CONCORD_RELAY_* set the relay must be Concord's own:\n got %+v\nwant %+v", got, want)
	}
	if got.Limit != nil {
		t.Fatal("no variables set, yet circuits are limited: gossipsub through this node would stop")
	}
}

func TestRelayEnvOverrides(t *testing.T) {
	got, err := RendezvousRelayResourcesFromEnv(envOf(map[string]string{
		"CONCORD_RELAY_CIRCUIT_BYTES":             "1048576",
		"CONCORD_RELAY_CIRCUIT_DURATION":          "2m",
		"CONCORD_RELAY_MAX_CIRCUITS":              "4",
		"CONCORD_RELAY_MAX_RESERVATIONS":          "256",
		"CONCORD_RELAY_MAX_RESERVATIONS_PER_PEER": "2",
		"CONCORD_RELAY_MAX_RESERVATIONS_PER_IP":   "8",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Limit == nil || got.Limit.Data != 1<<20 || got.Limit.Duration != 2*time.Minute {
		t.Fatalf("circuit limit = %+v, want 1 MiB and 2m", got.Limit)
	}
	if got.MaxCircuits != 4 || got.MaxReservations != 256 || got.MaxReservationsPerPeer != 2 || got.MaxReservationsPerIP != 8 {
		t.Fatalf("counts not applied: %+v", got)
	}

	// One cap alone brings the other from the library default.
	got, err = RendezvousRelayResourcesFromEnv(envOf(map[string]string{"CONCORD_RELAY_CIRCUIT_BYTES": "65536"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Limit == nil || got.Limit.Data != 65536 || got.Limit.Duration != relay.DefaultLimit().Duration {
		t.Fatalf("bytes alone: limit = %+v", got.Limit)
	}
}

func TestRelayEnvRejectsBadValues(t *testing.T) {
	for _, bad := range []map[string]string{
		{"CONCORD_RELAY_CIRCUIT_BYTES": "lots"},
		{"CONCORD_RELAY_CIRCUIT_BYTES": "0"},
		{"CONCORD_RELAY_CIRCUIT_DURATION": "120"},
		{"CONCORD_RELAY_CIRCUIT_DURATION": "-1m"},
		{"CONCORD_RELAY_MAX_CIRCUITS": "-3"},
		{"CONCORD_RELAY_MAX_RESERVATIONS": "many"},
	} {
		if _, err := RendezvousRelayResourcesFromEnv(envOf(bad)); err == nil {
			t.Errorf("%v accepted; a typo must stop the node, not leave circuits unlimited", bad)
		}
	}
}

// cappedPair is relayedPair with the relay built from the given resources.
func cappedPair(t *testing.T, ctx context.Context, res relay.Resources) (host.Host, host.Host) {
	t.Helper()
	mk := func(opts ...libp2p.Option) host.Host {
		h, err := libp2p.New(append([]libp2p.Option{
			libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
			libp2p.Security(noise.ID, noise.New),
		}, opts...)...)
		if err != nil {
			t.Fatalf("host: %v", err)
		}
		t.Cleanup(func() { _ = h.Close() })
		return h
	}
	rl := mk(libp2p.ForceReachabilityPublic())
	if _, err := relay.New(rl, relay.WithResources(res)); err != nil {
		t.Fatalf("relay service: %v", err)
	}
	relayInfo := peer.AddrInfo{ID: rl.ID(), Addrs: rl.Addrs()}
	ha, hb := mk(), mk()
	for _, h := range []host.Host{ha, hb} {
		if err := h.Connect(ctx, relayInfo); err != nil {
			t.Fatalf("connect to relay: %v", err)
		}
		if _, err := client.Reserve(ctx, h, relayInfo); err != nil {
			t.Fatalf("reserve: %v", err)
		}
	}
	circuit, err := multiaddr.NewMultiaddr("/p2p/" + rl.ID().String() + "/p2p-circuit/p2p/" + hb.ID().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := ha.Connect(relayCtx(ctx, "test"), peer.AddrInfo{ID: hb.ID(), Addrs: []multiaddr.Multiaddr{circuit}}); err != nil {
		t.Fatalf("relayed connect: %v", err)
	}
	return ha, hb
}

func TestRelayEnvCapCutsBulkData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const capBytes = 64 << 10
	res, err := RendezvousRelayResourcesFromEnv(envOf(map[string]string{"CONCORD_RELAY_CIRCUIT_BYTES": "65536"}))
	if err != nil {
		t.Fatal(err)
	}
	ha, hb := cappedPair(t, ctx, res)

	got := make(chan int64, 2)
	hb.SetStreamHandler(probeProto, func(s network.Stream) {
		n, _ := io.Copy(io.Discard, s)
		got <- n
		_ = s.Close()
	})
	send := func(n int) error {
		s, err := ha.NewStream(relayCtx(ctx, "test"), hb.ID(), probeProto)
		if err != nil {
			return err
		}
		defer s.Close()
		if _, err := s.Write(make([]byte, n)); err != nil {
			return err
		}
		return s.CloseWrite()
	}

	// Signaling-sized traffic goes through a capped circuit untouched.
	if err := send(4 << 10); err != nil {
		t.Fatalf("a small exchange through a capped relay failed: %v", err)
	}
	if n := <-got; n != 4<<10 {
		t.Fatalf("small exchange: %d bytes arrived, want %d", n, 4<<10)
	}

	// Bulk data does not: the relay resets the circuit at the cap.
	_ = send(4 << 20)
	select {
	case n := <-got:
		t.Logf("%d of %d bytes crossed before the relay cut the circuit", n, 4<<20)
		if n >= 4<<20 || n > 2*capBytes {
			t.Fatalf("%d bytes crossed a relay capped at %d per circuit", n, capBytes)
		}
	case <-ctx.Done():
		t.Fatal("receiver never saw the circuit end")
	}
}
