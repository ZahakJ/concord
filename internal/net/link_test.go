package net

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/ZahakJ/concord/internal/identity"
)

// libp2p reporting a peer as Connected does not mean the connection works. A
// relay circuit whose relay has gone, or a socket the far end forgot across a
// restart, stays Connected here and only fails when something tries to use it.
//
// dialLinkStream used to take Connected at face value and skip the dial, so
// every retry hung on the same dead connection until the budget ran out and the
// user got "open link stream: context deadline exceeded" — while the addresses
// in the QR code, which would have worked, were never tried. Retiring the
// connection is what turns the next attempt back into a real dial.
func TestDialLinkStreamRetiresADeadConnection(t *testing.T) {
	ctx := context.Background()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	// Opening a stream always fails, standing in for a connection that is up
	// but unusable. (B lacking the protocol would not do: that is a definite
	// answer from the real peer, reported as ErrLinkPeerTooOld.)
	a.openStream = func(context.Context, peer.ID, ...protocol.ID) (network.Stream, error) {
		return nil, errors.New("stream went nowhere")
	}
	if err := a.Connect(ctx, b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if a.h.Network().Connectedness(b.PeerID()) != network.Connected {
		t.Fatal("expected a live connection to start from")
	}

	// Budget for a single pass: enough for one failed attempt, not enough to
	// reach the retry that would dial again and leave a fresh connection behind.
	// What is being tested is the reuse decision, not the retry loop.
	dialCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	if _, err := a.dialLinkStream(dialCtx, b.AddrInfo(), linkConfirmProtocol); err == nil {
		t.Fatal("expected the dial to fail: the stream cannot open")
	}

	// The failing connection must not still be sitting there ready to be reused
	// by the next attempt — that is the loop that never recovers.
	if a.h.Network().Connectedness(b.PeerID()) == network.Connected {
		t.Fatal("a connection that could not carry the link stream was left open;\n" +
			"the next attempt will skip the dial and hang on it again")
	}
}

// An issuer from before 0.61 has only /concord/link/1.0.0, which hands the
// account to anyone with the code. A newer device must say so at once, and
// must not send its request over the old protocol instead.
func TestRequestLinkConfirmedRefusesAnOldIssuer(t *testing.T) {
	ctx := context.Background()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)
	var oldAsked atomic.Bool
	b.HandleLink(func(context.Context, peer.ID, []byte) ([]byte, error) {
		oldAsked.Store(true)
		return []byte("seed"), nil
	})

	for _, reuse := range []bool{false, true} {
		if reuse {
			if err := a.Connect(ctx, b.AddrInfo()); err != nil {
				t.Fatalf("Connect: %v", err)
			}
		}
		dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		start := time.Now()
		_, err := a.RequestLinkConfirmed(dialCtx, b.AddrInfo(), []byte("{}"), nil)
		cancel()
		if !errors.Is(err, ErrLinkPeerTooOld) {
			t.Fatalf("reuse=%v: want ErrLinkPeerTooOld, got %v", reuse, err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("reuse=%v: took %v to say the issuer is too old; it retried", reuse, d)
		}
	}
	if oldAsked.Load() {
		t.Fatal("the request fell back to the pre-0.61 protocol")
	}
}

// The confirm protocol carries one wait frame, then the answer; the joiner
// hears the wait before the answer and gets the answer's bytes intact.
func TestLinkConfirmedWaitThenAnswer(t *testing.T) {
	ctx := context.Background()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)
	release := make(chan struct{})
	fromCh := make(chan peer.ID, 1)
	b.HandleLinkConfirmed(func(_ context.Context, p peer.ID, req []byte, wait func([]byte) error) ([]byte, error) {
		fromCh <- p
		if err := wait([]byte("w")); err != nil {
			return nil, err
		}
		if wait([]byte("again")) == nil {
			return nil, errors.New("a second wait frame was allowed")
		}
		<-release
		return append([]byte("answer:"), req...), nil
	})
	var heard []byte
	got, err := a.RequestLinkConfirmed(ctx, b.AddrInfo(), []byte("q"), func(w []byte) {
		heard = append([]byte(nil), w...)
		close(release)
	})
	if err != nil {
		t.Fatalf("RequestLinkConfirmed: %v", err)
	}
	if string(heard) != "w" || string(got) != "answer:q" {
		t.Fatalf("heard %q then %q; want \"w\" then \"answer:q\"", heard, got)
	}
	if from := <-fromCh; from != a.PeerID() {
		t.Fatalf("responder saw dialler %s, want %s", from, a.PeerID())
	}
}
