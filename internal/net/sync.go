package net

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// syncProtocol carries history catch-up: a peer that was offline asks a
// connected peer for what it missed. Like the invite protocol, this layer is
// semantics-free — one length-framed request, one length-framed response; the
// app layer defines the JSON shapes and encrypts the payload (MLS) so only
// guild members can read what comes back.
//
// v2 added MLS commit backfill plus guild/profile/message-state snapshots and
// is not wire-compatible with v1, so the ID was bumped: a v1 peer's request
// simply fails protocol negotiation and it falls back to live gossip only.
const syncProtocol protocol.ID = "/concord/sync/2.0.0"

// syncStreamTimeout bounds one sync exchange on either end when the caller
// gives no deadline of its own. A stream is only as patient as its deadline:
// a peer that accepts one and never answers — a stalled process, a
// half-dead connection — otherwise holds it, and the request waiting on it,
// forever.
const syncStreamTimeout = time.Minute

// SyncResponder answers an inbound history request with response bytes.
type SyncResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleSync registers the responder for inbound sync requests. Call once.
func (n *Host) HandleSync(responder SyncResponder) {
	n.h.SetStreamHandler(syncProtocol, func(s network.Stream) {
		defer s.Close()
		_ = s.SetDeadline(time.Now().Add(syncStreamTimeout))
		req, err := readFrame(s, maxInviteFrame)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrame(s, resp, maxInviteFrame)
	})
}

// RequestSync asks an already-connected peer for missed history.
func (n *Host) RequestSync(ctx context.Context, to peer.ID, request []byte) ([]byte, error) {
	s, err := n.newStream(ctx, to, syncProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open sync stream: %w", err)
	}
	defer s.Close()
	// The context used to stop applying the moment the stream opened, so a
	// peer that never answered held this request — and, since catch-ups run
	// one per guild and peer, every later catch-up between the two — forever.
	_ = s.SetDeadline(streamDeadline(ctx, syncStreamTimeout))
	if err := writeFrame(s, request, maxInviteFrame); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, maxInviteFrame)
}
