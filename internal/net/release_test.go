package net

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/identity"
)

// A release transfer is many chunk round trips on one protocol, so the thing
// worth proving here is that a full-sized chunk survives the framing intact.
func TestReleaseRoundTrip(t *testing.T) {
	ctx := context.Background()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	chunk := make([]byte, ReleaseChunkSize)
	if _, err := rand.Read(chunk); err != nil {
		t.Fatal(err)
	}
	var asked []byte
	b.HandleRelease(func(_ context.Context, from peer.ID, req []byte) ([]byte, error) {
		if from != a.PeerID() {
			t.Errorf("request from %s, want %s", from, a.PeerID())
		}
		asked = req
		return chunk, nil
	})

	if err := a.Connect(ctx, b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	got, err := a.RequestRelease(ctx, b.PeerID(), []byte(`{"op":"chunk"}`))
	if err != nil {
		t.Fatalf("RequestRelease: %v", err)
	}
	if !bytes.Equal(got, chunk) {
		t.Fatalf("got %d bytes back, want %d identical", len(got), len(chunk))
	}
	if string(asked) != `{"op":"chunk"}` {
		t.Fatalf("responder saw request %q", asked)
	}
}

// A peer that claims a frame larger than the cap must be cut off, not trusted
// to be reasonable: this stream is the one place we accept megabytes.
func TestReleaseResponseCapped(t *testing.T) {
	ctx := context.Background()
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	b.HandleRelease(func(context.Context, peer.ID, []byte) ([]byte, error) {
		return make([]byte, MaxReleaseResponse+1), nil
	})
	if err := a.Connect(ctx, b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := a.RequestRelease(ctx, b.PeerID(), []byte(`{"op":"chunk"}`)); err == nil {
		t.Fatal("oversized response accepted")
	}
}

// A peer that accepts the stream and then says nothing must not be able to park
// the caller. The context bounds NewStream and nothing after it, so without a
// stream deadline this never returned: not the 8s metadata timeout, not the 60s
// chunk timeout, not the 10-minute transfer budget, and not the offers fan-out
// the Settings pane fires today.
func TestRequestReleaseBoundedAgainstASilentPeer(t *testing.T) {
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	mute := make(chan struct{})
	defer close(mute)
	b.h.SetStreamHandler(releaseProtocol, func(s network.Stream) { <-mute })

	if err := a.Connect(context.Background(), b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := a.RequestRelease(ctx, b.PeerID(), []byte(`{"op":"offer"}`))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a peer that answered nothing produced a successful response")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("RequestRelease ignored its 500ms deadline and is still blocked")
	}
}

// The mirror image, and the reason the deadline is on the serving side too: an
// inbound stream is a goroutine and a pair of buffers, and a peer that opens one
// without ever asking a question would otherwise own them until the process
// exits.
func TestReleaseHandlerGivesUpOnASilentClient(t *testing.T) {
	saved := releaseStreamTimeout
	t.Cleanup(func() { releaseStreamTimeout = saved })
	releaseStreamTimeout = 300 * time.Millisecond

	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a := newTestHost(t, idA)
	b := newTestHost(t, idB)

	b.HandleRelease(func(context.Context, peer.ID, []byte) ([]byte, error) {
		t.Error("responder ran for a client that never sent a request")
		return nil, nil
	})
	if err := a.Connect(context.Background(), b.AddrInfo()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	s, err := a.h.NewStream(context.Background(), b.PeerID(), releaseProtocol)
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	defer s.Close()

	// The handler giving up closes the stream, which is what this read sees.
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(s, make([]byte, 1))
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the responder is still waiting on a client that never spoke")
	}
}
