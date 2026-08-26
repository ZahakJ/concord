package net

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// chronicleProtocol fetches the bulk chunks of a guild's history archive. It is
// a sibling of attachProtocol and of releaseProtocol, and it is worth saying why
// it is neither of them:
//
//   - An attachment is one blob per message, fetched because somebody scrolled
//     an image into view. A chronicle chunk is a thousand archived messages,
//     fetched because somebody scrolled into a year that predates the guild.
//     The two want different frame caps and different politeness.
//   - A release is offset-addressed because a desktop build is far too big to
//     read into memory whole. A chunk is capped at a megabyte by construction —
//     the builder splits until it fits — so one framed read is the whole answer
//     and there is no resume state to keep.
//
// What it takes from release rather than from attach is the DEADLINE. A libp2p
// stream has none unless one is set, and readFrame is a bare io.ReadFull: a
// context bounds NewStream and nothing after it. Without a deadline on both
// sides, a peer that accepts a stream and then says nothing parks the caller for
// the life of the process — and unlike an image, nobody is watching a background
// history fetch closely enough to notice it never returned.
//
// Serving is ungated, exactly as attachments are. A chunk is secretbox
// ciphertext whose key lives only in the chronicle manifest, and the manifest
// only ever travels MLS-encrypted inside guild gossip and history sync. The
// bytes are therefore useless to anyone who is not already a member, and the
// chunk id is an unguessable 256-bit capability. Gating the serve would buy
// nothing and would cost the property that makes this work at all: every member
// who has ever read a page of the archive becomes a source for it.
const chronicleProtocol protocol.ID = "/concord/chronicle/1.0.0"

// maxChronicleRequest bounds the request frame (a tiny JSON chunk-id lookup).
const maxChronicleRequest = 4 << 10 // 4 KiB

// MaxChronicleChunk is the ciphertext ceiling on one chunk. Exported because
// the app layer's chunk builder must split against the same number — a chunk
// that cannot be served is a chunk that cannot be built.
const MaxChronicleChunk = 1 << 20 // 1 MiB

// MaxChronicleResponse bounds a served frame: one chunk plus headroom, so the
// cap the builder respects and the cap the reader enforces cannot drift into
// each other by a byte of framing.
const MaxChronicleResponse = MaxChronicleChunk + (64 << 10)

// chronicleStreamTimeout is the ceiling on one chunk round trip: the deadline
// used when the caller gave none, and the whole budget on the serving side,
// where the work is a single indexed disk read. A var so tests need not wait it
// out.
var chronicleStreamTimeout = 60 * time.Second

// ChronicleResponder answers an inbound chunk request; an empty response means
// "don't have it".
type ChronicleResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleChronicle registers the responder for inbound chunk requests.
func (n *Host) HandleChronicle(responder ChronicleResponder) {
	n.h.SetStreamHandler(chronicleProtocol, func(s network.Stream) {
		defer s.Close()
		// A stream is a goroutine plus buffers; a peer that opens one and never
		// asks anything must not hold them indefinitely.
		_ = s.SetDeadline(time.Now().Add(chronicleStreamTimeout))
		req, err := readFrame(s, maxChronicleRequest)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrame(s, resp, MaxChronicleResponse)
	})
}

// RequestChronicleChunk asks an already-connected peer for one archive chunk.
func (n *Host) RequestChronicleChunk(ctx context.Context, to peer.ID, request []byte) ([]byte, error) {
	s, err := n.newStream(ctx, to, chronicleProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open chronicle stream: %w", err)
	}
	defer s.Close()
	_ = s.SetDeadline(streamDeadline(ctx, chronicleStreamTimeout))
	if err := writeFrame(s, request, maxChronicleRequest); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, MaxChronicleResponse)
}
