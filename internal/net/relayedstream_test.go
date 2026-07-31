package net

// Concord's peers are ordinary people behind ordinary routers. Two of them —
// or one person's phone on mobile data and their desktop at home — frequently
// have NO direct path to each other, and the circuit relay is the only way they
// can talk at all.
//
// libp2p calls a relayed connection "limited" and REFUSES to open a stream on
// one unless the caller opts in. Nothing in Concord opted in, so every protocol
// silently failed the moment the relay was the only route: history sync, the
// device hello, DM invites, attachments, voice signalling. Presence still worked
// (it needs no stream), so both sides showed ONLINE and did nothing — which is
// precisely the "we can see each other but messages never arrive" report this
// test exists to prevent coming back.

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/multiformats/go-multiaddr"
)

const probeProto protocol.ID = "/concord/test/relayed/1.0.0"

// relayedPair returns two hosts whose ONLY route to each other is through a
// relay: neither holds the other's direct address, and the dial is made over a
// /p2p-circuit multiaddr.
func relayedPair(t *testing.T, ctx context.Context) (host.Host, host.Host) {
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
	if _, err := relay.New(rl); err != nil {
		t.Fatalf("relay service: %v", err)
	}
	relayInfo := peer.AddrInfo{ID: rl.ID(), Addrs: rl.Addrs()}

	ha, hb := mk(), mk()
	for _, h := range []host.Host{ha, hb} {
		if err := h.Connect(ctx, relayInfo); err != nil {
			t.Fatalf("connect to relay: %v", err)
		}
	}
	// BOTH reserve. Gossipsub is bidirectional — the receiver opens a stream back
	// to the sender — so a test where only one side is reachable through the relay
	// would fail for a reason that has nothing to do with what is being examined.
	// It also matches reality: both devices are behind NATs and both reserve.
	for _, h := range []host.Host{ha, hb} {
		if _, err := client.Reserve(ctx, h, relayInfo); err != nil {
			t.Fatalf("reserve: %v", err)
		}
	}
	circuit, err := multiaddr.NewMultiaddr("/p2p/" + rl.ID().String() + "/p2p-circuit/p2p/" + hb.ID().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := ha.Connect(ctx, peer.AddrInfo{ID: hb.ID(), Addrs: []multiaddr.Multiaddr{circuit}}); err != nil {
		t.Fatalf("relayed connect: %v", err)
	}
	return ha, hb
}

func TestStreamsWorkOverARelayedConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ha, hb := relayedPair(t, ctx)
	hb.SetStreamHandler(probeProto, func(s network.Stream) { _ = s.Close() })

	// The bug: without opting in, the stream does not open. libp2p either refuses
	// outright with ErrLimitedConn or goes hunting for a direct path that does not
	// exist and times out — both are the same thing from Concord's side, which is
	// that the protocol never runs while the peer sits there looking online. Short
	// budget: we are proving it fails, not waiting out a real timeout.
	bareCtx, bareCancel := context.WithTimeout(ctx, 8*time.Second)
	defer bareCancel()
	if _, err := ha.NewStream(bareCtx, hb.ID(), probeProto); err == nil {
		t.Fatal("a bare stream over a relayed connection unexpectedly succeeded — " +
			"if libp2p stopped gating limited conns, relayCtx is now redundant and " +
			"this test should be rewritten rather than deleted")
	}

	// The fix: relayCtx marks the context as willing to use the relay.
	s, err := ha.NewStream(relayCtx(ctx, "test"), hb.ID(), probeProto)
	if err != nil {
		t.Fatalf("a stream over the relay STILL fails, so nothing will work between "+
			"two peers with no direct path: %v", err)
	}
	_ = s.Close()
}
