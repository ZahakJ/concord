package net

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	"github.com/ZahakJ/concord/internal/identity"
)

const echoProto protocol.ID = "/concord/test-echo/1.0.0"

// echoPair is two connected hosts, b answering echoProto by echoing one line.
func echoPair(t *testing.T) (a, b *Host) {
	t.Helper()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a, b = newTestHost(t, idA), newTestHost(t, idB)
	b.h.SetStreamHandler(echoProto, func(s network.Stream) {
		defer s.Close()
		buf, _ := io.ReadAll(s)
		_, _ = s.Write(buf)
	})
	if err := a.Connect(context.Background(), b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return a, b
}

func echoThrough(t *testing.T, s network.Stream) {
	t.Helper()
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := s.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = s.CloseWrite()
	got, err := io.ReadAll(s)
	if err != nil || string(got) != "ping" {
		t.Fatalf("echo = %q, %v; want ping", got, err)
	}
}

// A linked phone joining its desktop's guild used to fail 40–60% of the time
// under load with "open invite stream: connection failed": the desktop's
// resource manager refused one of a burst of simultaneous connections after
// the phone had already counted it as up, and the stream open died with it.
// The first open here loses its connection the same way; the second must
// find the one that comes back.
func TestStreamOpenSurvivesItsConnectionDying(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"no connection left", errors.New("connection failed")},
		{"refused by the listener", &network.ConnError{Remote: true, ErrorCode: network.ConnResourceLimitExceeded}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := echoPair(t)
			calls := 0
			a.openStream = func(ctx context.Context, p peer.ID, protos ...protocol.ID) (network.Stream, error) {
				calls++
				if calls == 1 {
					_ = a.h.Network().ClosePeer(p)
					return nil, tc.err
				}
				return a.h.NewStream(ctx, p, protos...)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			s, err := a.newStream(ctx, b.PeerID(), echoProto)
			if err != nil {
				t.Fatalf("newStream after its connection died: %v (after %d tries)", err, calls)
			}
			echoThrough(t, s)
		})
	}
}

// The retry is for a connection lost under the open, not a blanket one: a
// peer that does not speak the protocol is answered at once.
func TestStreamOpenDoesNotRetryARefusal(t *testing.T) {
	a, b := echoPair(t)
	calls := 0
	a.openStream = func(ctx context.Context, p peer.ID, protos ...protocol.ID) (network.Stream, error) {
		calls++
		return a.h.NewStream(ctx, p, protos...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.newStream(ctx, b.PeerID(), "/concord/nobody-speaks-this/1.0.0"); err == nil {
		t.Fatal("opened a protocol the peer does not serve")
	}
	if calls != 1 {
		t.Fatalf("a refused protocol was tried %d times, want 1", calls)
	}
}

// The other half of the same failure: the slot the dial storm used up. A
// device must be able to hold more than go-libp2p's default eight connections
// from one peer for the moment both sides are dialling every address at once,
// or the listener refuses the connection the dialer already counts as up.
func TestOnePeerMayHoldADialStormOfConnections(t *testing.T) {
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a, b := newTestHost(t, idA), newTestHost(t, idB)
	rm := a.h.Network().ResourceManager()
	// Loopback, because the per-IP limiter exempts it: what is measured here
	// is the per-peer cap alone.
	addr := multiaddr.StringCast("/ip4/127.0.0.1/udp/4001/quic-v1")
	for i, dir := range []network.Direction{
		network.DirInbound, network.DirOutbound, network.DirInbound, network.DirOutbound,
		network.DirInbound, network.DirInbound, network.DirInbound, network.DirInbound,
		network.DirInbound, network.DirInbound, network.DirInbound, network.DirInbound,
	} {
		cs, err := rm.OpenConnection(dir, false, addr)
		if err != nil {
			t.Fatalf("connection %d: %v", i+1, err)
		}
		defer cs.Done()
		if err := cs.SetPeer(b.PeerID()); err != nil {
			t.Fatalf("connection %d from one peer refused: %v", i+1, err)
		}
	}
}
