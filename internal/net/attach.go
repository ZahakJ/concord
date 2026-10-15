package net

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// attachProtocol fetches attachment blobs out-of-band. Chat messages carry
// only a tiny content-addressed reference (the blob is secretbox ciphertext,
// its key rides inside the MLS-encrypted message), so image bytes never touch
// the 1 MiB-capped gossipsub/sync paths — this stream is the only place large
// payloads travel, with its own generous frame cap.
const attachProtocol protocol.ID = "/concord/attach/1.0.0"

// maxAttachRequest bounds the request frame (a tiny JSON blob-ID lookup).
const maxAttachRequest = 4 << 10 // 4 KiB

// MaxAttachResponse bounds a served blob: up to a 25 MiB file plaintext +
// secretbox overhead, rounded up generously. Exported so the app layer can
// reject oversized blobs before ever creating them. Blobs travel over this
// dedicated stream (one framed read into memory), never over the 1 MiB-capped
// gossipsub/sync paths.
const MaxAttachResponse = 32 << 20 // 32 MiB

// AttachResponder answers an inbound blob request; an empty response means
// "don't have it".
type AttachResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleAttachments registers the responder for inbound blob requests.
func (n *Host) HandleAttachments(responder AttachResponder) {
	n.h.SetStreamHandler(attachProtocol, func(s network.Stream) {
		defer s.Close()
		// The request is a few bytes and must arrive promptly; the reply can
		// be 32 MiB over a slow relay, so it is bounded by progress, not by
		// a clock (writeFrameProgress).
		_ = s.SetReadDeadline(time.Now().Add(streamIdle))
		req, err := readFrame(s, maxAttachRequest)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrameProgress(s, resp, MaxAttachResponse)
	})
}

// RequestAttachment asks an already-connected peer for a blob.
func (n *Host) RequestAttachment(ctx context.Context, to peer.ID, request []byte) ([]byte, error) {
	s, err := n.newStream(ctx, to, attachProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open attach stream: %w", err)
	}
	defer s.Close()
	_ = s.SetWriteDeadline(streamDeadline(ctx, streamIdle))
	if err := writeFrame(s, request, maxAttachRequest); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	// The peer must START answering within the caller's context; after that a
	// large blob may take as long as it keeps moving. The context alone would
	// cut a 25 MiB file off at fifteen seconds on a slow link.
	return readFrameProgress(s, MaxAttachResponse, streamDeadline(ctx, streamIdle))
}
