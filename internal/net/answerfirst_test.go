package net

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
)

// TestAnswerThatBeatsTheHalfCloseIsRead pins a race every request/response
// protocol here shares. The requester writes its frame and then half-closes.
// A responder reads the frame, answers and closes its end without waiting for
// that half-close, and on QUIC closing also stops it reading, which cancels the
// requester's sending side. If the requester only gets to CloseWrite after
// that, the call used to fail although the answer had arrived: under load an
// archive page came back as "no reachable member holds that page". The hook
// holds the requester between its frame and its half-close until the
// responder has answered and closed, which is the order load produces.
func TestAnswerThatBeatsTheHalfCloseIsRead(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	for _, tc := range []struct{ name, addr string }{
		{"quic", "/ip4/127.0.0.1/udp/0/quic-v1"},
		{"tcp", "/ip4/127.0.0.1/tcp/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			idA, _ := identity.Generate()
			idB, _ := identity.Generate()
			a, err := New(ctx, Config{Identity: idA, ListenAddrs: []string{tc.addr}})
			if err != nil {
				t.Fatalf("New A: %v", err)
			}
			defer a.Close()
			b, err := New(ctx, Config{Identity: idB, ListenAddrs: []string{tc.addr}})
			if err != nil {
				t.Fatalf("New B: %v", err)
			}
			defer b.Close()

			page := bytes.Repeat([]byte("archived page "), 256)
			answered := make(chan struct{}, 1)
			b.HandleChronicle(func(context.Context, peer.ID, []byte) ([]byte, error) {
				answered <- struct{}{}
				return page, nil
			})
			testHookRequestWritten = func() {
				select {
				case <-answered:
				case <-time.After(5 * time.Second):
					return
				}
				// The responder writes its answer and closes as soon as the
				// responder function returns; give that a moment to reach us.
				time.Sleep(300 * time.Millisecond)
			}
			defer func() { testHookRequestWritten = nil }()

			if err := a.Connect(ctx, b.AddrInfo()); err != nil {
				t.Fatalf("connect: %v", err)
			}
			got, err := a.RequestChronicleChunk(ctx, b.PeerID(), []byte(`{"chunk":"x"}`))
			if err != nil {
				t.Fatalf("the responder answered and closed first, and the request failed: %v", err)
			}
			if !bytes.Equal(got, page) {
				t.Fatalf("got %d bytes back, want the %d-byte page", len(got), len(page))
			}
		})
	}
}
