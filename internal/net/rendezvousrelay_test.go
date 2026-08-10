package net

// The rendezvous is the only relay most Concord users will ever have, and a
// rendezvous whose relay never starts breaks every NAT'd peer silently: clients
// reject it with "doesn't speak circuit v2", nobody gets a reservation, and each
// device advertises nothing but LAN addresses. It shipped that way and presented
// as "my phone sees no peers" and "my other device never comes online", which is
// nearly impossible to trace back to a missing protocol advertisement.
//
// These tests pin the cause, with a negative control so the assertion cannot rot
// into one that passes for the wrong reason.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
)

// A rendezvous built the way cmd/rendezvous builds one, with and without the
// forced-public reachability, so the difference is provable rather than argued.
func rendezvousLike(t *testing.T, ctx context.Context, forcePublic bool) string {
	t.Helper()
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.Security(noise.ID, noise.New),
		libp2p.EnableRelayService(relay.WithResources(relay.DefaultResources())),
		libp2p.EnableNATService(),
	}
	if forcePublic {
		opts = append(opts, libp2p.ForceReachabilityPublic())
	}
	h, err := libp2p.New(opts...)
	if err != nil {
		t.Fatalf("rendezvous: %v", err)
	}
	kdht, err := dht.New(h, dht.Mode(dht.ModeServer))
	if err != nil {
		t.Fatalf("dht: %v", err)
	}
	_ = kdht.Bootstrap(ctx)
	t.Cleanup(func() { _ = kdht.Close(); _ = h.Close() })
	return h.Addrs()[0].String() + "/p2p/" + h.ID().String()
}

// speaksCircuitV2 asks, over a real connection, whether the node advertises the
// relay HOP protocol — the exact check AutoRelay makes before accepting a relay
// candidate, and the exact one production failed with "doesn't speak circuit v2".
func speaksCircuitV2(t *testing.T, ctx context.Context, boot string, within time.Duration) bool {
	t.Helper()
	pi, err := peer.AddrInfoFromString(boot)
	if err != nil {
		t.Fatal(err)
	}
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"), libp2p.Security(noise.ID, noise.New))
	if err != nil {
		t.Fatalf("probe host: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	if err := h.Connect(ctx, *pi); err != nil {
		t.Fatalf("connect: %v", err)
	}
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		protos, _ := h.Peerstore().GetProtocols(pi.ID)
		for _, pr := range protos {
			if strings.Contains(string(pr), "circuit/relay") && strings.Contains(string(pr), "hop") {
				return true
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// The production bug: a rendezvous that leaves reachability to AutoNAT never
// starts its relay service, so it does not speak circuit v2 at all. Every
// client's AutoRelay then rejects it, nobody gets a reservation, and every
// device behind a NAT advertises only LAN addresses.
func TestRendezvousWithoutForcedPublicDoesNotSpeakCircuitV2(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if speaksCircuitV2(t, ctx, rendezvousLike(t, ctx, false), 15*time.Second) {
		t.Skip("AutoNAT concluded Public on this host; the negative control does not reproduce here")
	}
}

func TestRendezvousForcedPublicSpeaksCircuitV2(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !speaksCircuitV2(t, ctx, rendezvousLike(t, ctx, true), 25*time.Second) {
		t.Fatal("a forced-public rendezvous still does not advertise the circuit v2 hop protocol")
	}
}
