package net

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
)

func connectedPair(t *testing.T, ctx context.Context) (*Host, *Host) {
	t.Helper()
	aID, _ := identity.Generate()
	bID, _ := identity.Generate()
	a, err := New(ctx, Config{Identity: aID})
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	b, err := New(ctx, Config{Identity: bID})
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if err := b.Connect(ctx, a.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return a, b
}

// TestStalledRespondersDoNotHoldTheRequester: every request/response protocol
// used to apply its caller's context only to opening the stream, so a peer that
// accepted the stream and never answered held the caller — a goroutine, a
// stream, and whatever the caller was waiting to do next — for the life of the
// process. Each request must now give up when its context does. (Invite and
// link deliberately wait at least two minutes for their reply, because their
// request has an effect the requester must not walk away from; they are
// bounded, just not by a two-second context, so they are not timed here.)
func TestStalledRespondersDoNotHoldTheRequester(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := connectedPair(t, ctx)
	stall := make(chan struct{})
	defer close(stall)
	never := func(context.Context, peer.ID, []byte) ([]byte, error) {
		<-stall
		return nil, nil
	}
	a.HandleDMInvites(never)
	a.HandleHello(never)
	a.HandleAttachments(never)

	for name, ask := range map[string]func(context.Context) error{
		"dm-invite": func(c context.Context) error { _, err := b.RequestDMInvite(c, a.PeerID(), []byte("{}")); return err },
		"hello":     func(c context.Context) error { _, err := b.SayHello(c, a.PeerID(), []byte("{}")); return err },
		"attach":    func(c context.Context) error { _, err := b.RequestAttachment(c, a.PeerID(), []byte("{}")); return err },
	} {
		done := make(chan error, 1)
		go func() {
			rctx, rcancel := context.WithTimeout(ctx, 2*time.Second)
			defer rcancel()
			done <- ask(rctx)
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: a request nobody answered reported success", name)
			}
		case <-time.After(10 * time.Second):
			t.Errorf("%s: the request outlived its two-second context by eight seconds", name)
		}
	}
}

// TestSlowAttachmentOutlivesItsContext: the attachment stream carries files up
// to 32 MiB, which on a slow relay take far longer than the fifteen seconds a
// fetch allows for an answer. The context bounds how long the peer may take to
// START answering; a reply that keeps moving may then take as long as it needs.
func TestSlowAttachmentOutlivesItsContext(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b := connectedPair(t, ctx)
	body := bytes.Repeat([]byte{7}, 3*chunk)
	a.h.SetStreamHandler(attachProtocol, func(s network.Stream) {
		defer s.Close()
		_, _ = readFrame(s, maxAttachRequest)
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
		_, _ = s.Write(hdr[:])
		for off := 0; off < len(body); off += chunk {
			time.Sleep(700 * time.Millisecond) // a slow link, still moving
			_, _ = s.Write(body[off : off+chunk])
		}
	})
	rctx, rcancel := context.WithTimeout(ctx, time.Second)
	defer rcancel()
	got, err := b.RequestAttachment(rctx, a.PeerID(), []byte("{}"))
	if err != nil {
		t.Fatalf("a slow but moving attachment was cut off: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %d bytes, want %d", len(got), len(body))
	}
}
