package net

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"

	"github.com/zahak/concord/internal/identity"
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

	// B never answers the link protocol, so opening the stream always fails —
	// standing in for a connection that is up but unusable.
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
	if _, err := a.dialLinkStream(dialCtx, b.AddrInfo()); err == nil {
		t.Fatal("expected the dial to fail: B has no link handler")
	}

	// The failing connection must not still be sitting there ready to be reused
	// by the next attempt — that is the loop that never recovers.
	if a.h.Network().Connectedness(b.PeerID()) == network.Connected {
		t.Fatal("a connection that could not carry the link stream was left open;\n" +
			"the next attempt will skip the dial and hang on it again")
	}
}
