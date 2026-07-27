package net

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// releaseProtocol lets a node hand its own release binary to a peer that is
// running an older one. It is a sibling of attachProtocol rather than a reuse
// of it: attachments are one blob per stream read whole into memory, and a
// desktop build is far too big for that. Here the payload is fetched in
// offset-addressed chunks, so the frame cap stays small and a transfer that
// dies halfway costs only the chunk in flight.
//
// Safety comes from the RECEIVER: the release manifest is signed with an
// offline key whose public half is compiled into every binary, and nothing is
// installed until that signature verifies (internal/bridge/releasekey.go). That
// is what makes an untrusted peer an acceptable source at all. The bytes
// themselves are a published release and leak nothing; who may ASK is still
// gated, because the answer names the version (see handleReleaseRequest).
const releaseProtocol protocol.ID = "/concord/release/1.0.0"

// maxReleaseRequest bounds the request frame (a tiny JSON op + offset).
const maxReleaseRequest = 1 << 10 // 1 KiB

// ReleaseChunkSize is how much binary a single chunk request asks for. Big
// enough that a 60 MiB build is ~60 round trips, small enough that the frame
// cap stays modest and progress moves visibly.
const ReleaseChunkSize = 1 << 20 // 1 MiB

// MaxReleaseResponse bounds a served frame: one chunk, plus headroom for the
// JSON metadata responses (the signed manifest is a few KiB).
const MaxReleaseResponse = ReleaseChunkSize + (64 << 10)

// releaseStreamTimeout is the ceiling on one release round trip: the deadline
// used when the caller gave no deadline of its own, and the whole budget on the
// serving side, where the work is a disk read and a minute is already absurd.
// A libp2p stream has no deadline unless one is set, and readFrame is a bare
// io.ReadFull — a context bounds NewStream and NOTHING after it — so without
// this a peer that accepts a stream and then says nothing parks the caller for
// the life of the process. A var so tests need not wait it out.
var releaseStreamTimeout = 60 * time.Second

// streamDeadline is when a release stream must have finished: the caller's
// deadline if it set one, otherwise our own ceiling.
func streamDeadline(ctx context.Context) time.Time {
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	return time.Now().Add(releaseStreamTimeout)
}

// ReleaseResponder answers an inbound release request; an empty response means
// "nothing to offer".
type ReleaseResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleRelease registers the responder for inbound release requests.
func (n *Host) HandleRelease(responder ReleaseResponder) {
	n.h.SetStreamHandler(releaseProtocol, func(s network.Stream) {
		defer s.Close()
		// A stream is a goroutine plus buffers; a peer that opens one and never
		// asks anything must not hold them indefinitely.
		_ = s.SetDeadline(time.Now().Add(releaseStreamTimeout))
		req, err := readFrame(s, maxReleaseRequest)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrame(s, resp, MaxReleaseResponse)
	})
}

// RequestRelease asks an already-connected peer for release metadata or a
// chunk of its binary.
func (n *Host) RequestRelease(ctx context.Context, to peer.ID, request []byte) ([]byte, error) {
	s, err := n.h.NewStream(ctx, to, releaseProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open release stream: %w", err)
	}
	defer s.Close()
	// Every caller above this one — the chunk timeout, the transfer budget, the
	// offers fan-out behind Settings -> check for peer update — expresses its
	// patience as a context, and every one of them was decorative: the context
	// stopped applying the moment the stream opened.
	_ = s.SetDeadline(streamDeadline(ctx))
	if err := writeFrame(s, request, maxReleaseRequest); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, MaxReleaseResponse)
}
