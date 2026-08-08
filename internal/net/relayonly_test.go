package net

// The smallest version of the real topology: two Hosts that cannot dial each
// other directly and a rendezvous relay as the only shared address. Everything
// the app layer needs over that path — connection, streams, pubsub — is pinned
// here at the transport layer, where a failure names the mechanism instead of
// a symptom.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/multiformats/go-multiaddr"

	"github.com/ZahakJ/concord/internal/identity"
)

// relayRendezvous runs a DHT+relay node the way cmd/rendezvous does, on loopback.
//
// The AddrsFactory is the one concession to running a "public server" inside a
// test: autorelay composes a client's /p2p-circuit addresses only from the
// relay's PUBLIC addresses (autorelay/addrsplosion.go cleanupAddressSet), so a
// relay that advertises nothing but loopback produces NO circuit addresses at
// all and every client silently fails to become reachable. Advertising one
// fabricated public address alongside the real loopback one satisfies that
// filter; the fabricated address is never actually dialled, because the hop
// stream to the relay always finds the existing bootstrap connection first.
func relayRendezvous(t *testing.T, ctx context.Context) string {
	t.Helper()
	fakePublic := multiaddr.StringCast("/ip4/11.0.0.1/tcp/4001")
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0", "/ip4/127.0.0.1/udp/0/quic-v1"),
		libp2p.Security(noise.ID, noise.New),
		libp2p.AddrsFactory(func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
			return append(addrs, fakePublic)
		}),
	)
	if err != nil {
		t.Fatalf("rendezvous host: %v", err)
	}
	if _, err := relayv2.New(h, relayv2.WithResources(RendezvousRelayResources())); err != nil {
		t.Fatalf("rendezvous relay: %v", err)
	}
	kdht, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		t.Fatalf("rendezvous dht: %v", err)
	}
	_ = kdht.Bootstrap(ctx)
	t.Cleanup(func() { _ = kdht.Close(); _ = h.Close() })
	for _, a := range h.Addrs() {
		if strings.HasPrefix(a.String(), "/ip4/127.0.0.1/tcp/") {
			return fmt.Sprintf("%s/p2p/%s", a, h.ID())
		}
	}
	t.Fatal("rendezvous has no loopback TCP addr")
	return ""
}

func natdHost(t *testing.T, ctx context.Context, boot, ownIP string, blocked ...string) *Host {
	t.Helper()
	id, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pi, err := peer.AddrInfoFromString(boot)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(ctx, Config{
		Identity: id,
		ListenAddrs: []string{
			fmt.Sprintf("/ip4/%s/tcp/0", ownIP),
			fmt.Sprintf("/ip4/%s/udp/0/quic-v1", ownIP),
		},
		EnableDHT:      true,
		BootstrapPeers: []peer.AddrInfo{*pi},
		BlockedIPs:     blocked,
	})
	if err != nil {
		t.Fatalf("host on %s: %v", ownIP, err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func circuitAddrOf(h *Host) multiaddr.Multiaddr {
	for _, a := range h.Addrs() {
		if isRelayAddr(a) {
			return a
		}
	}
	return nil
}

// TestRelayOnlyPeersConnectAndGossip walks the exact ladder a phone and a
// desktop behind two NATs climb: reserve at the relay, connect through it,
// open an application stream, and exchange a pubsub message. Each rung is
// asserted separately so the first missing one names itself.
func TestRelayOnlyPeersConnectAndGossip(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := relayRendezvous(t, ctx)
	a := natdHost(t, ctx, boot, "127.0.0.2", "127.0.0.3")
	b := natdHost(t, ctx, boot, "127.0.0.3", "127.0.0.2")

	// Rung 1: both get a relay reservation (a /p2p-circuit addr of their own).
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && (circuitAddrOf(a) == nil || circuitAddrOf(b) == nil) {
		time.Sleep(250 * time.Millisecond)
	}
	if circuitAddrOf(a) == nil || circuitAddrOf(b) == nil {
		t.Fatalf("no relay reservation: a=%v b=%v", a.Addrs(), b.Addrs())
	}

	// Rung 2: a connects to b through the circuit.
	cctx, ccancel := context.WithTimeout(ctx, 20*time.Second)
	defer ccancel()
	if err := a.Connect(cctx, peer.AddrInfo{ID: b.PeerID(), Addrs: []multiaddr.Multiaddr{circuitAddrOf(b)}}); err != nil {
		t.Fatalf("connect over the relay: %v", err)
	}
	for _, c := range a.Libp2p().Network().ConnsToPeer(b.PeerID()) {
		if !strings.Contains(c.RemoteMultiaddr().String(), "p2p-circuit") {
			t.Fatalf("a direct connection slipped through: %s", c.RemoteMultiaddr())
		}
	}

	// Rung 3: an application stream over that connection. RequestSync is the
	// history catch-up every reconnect leans on.
	b.HandleSync(func(_ context.Context, _ peer.ID, req []byte) ([]byte, error) {
		return append([]byte("echo:"), req...), nil
	})
	sctx, scancel := context.WithTimeout(ctx, 15*time.Second)
	defer scancel()
	resp, err := a.RequestSync(sctx, b.PeerID(), []byte("hi"))
	if err != nil {
		t.Fatalf("sync stream over the relay: %v", err)
	}
	if string(resp) != "echo:hi" {
		t.Fatalf("sync response corrupted: %q", resp)
	}

	// Rung 4: gossipsub across the relayed connection — the path every guild
	// message, typing notice and voice-presence announcement rides.
	psA, err := a.NewPubSub(ctx)
	if err != nil {
		t.Fatal(err)
	}
	psB, err := b.NewPubSub(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	if err := psB.Subscribe(ctx, "room", func(_ peer.ID, data []byte) {
		select {
		case got <- string(data):
		default:
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := psA.Subscribe(ctx, "room", func(peer.ID, []byte) {}); err != nil {
		t.Fatal(err)
	}
	// Publish on a retry loop: mesh grafting takes a heartbeat or two.
	pubDeadline := time.Now().Add(45 * time.Second)
	for {
		_ = psA.Publish(ctx, "room", []byte("over the circuit"))
		select {
		case msg := <-got:
			if msg != "over the circuit" {
				t.Fatalf("pubsub message corrupted: %q", msg)
			}
			return
		case <-time.After(2 * time.Second):
		}
		if time.Now().After(pubDeadline) {
			t.Fatal("a pubsub message never crossed the relayed connection")
		}
	}
}
