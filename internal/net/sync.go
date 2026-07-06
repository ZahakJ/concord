package net

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// syncProtocol carries history catch-up: a peer that was offline asks a
// connected peer for messages it missed. Like the invite protocol, this layer
// is semantics-free — one length-framed request, one length-framed response;
// the app layer defines the JSON shapes and encrypts the payload (MLS) so only
// guild members can read what comes back.
const syncProtocol protocol.ID = "/concord/sync/1.0.0"

// SyncResponder answers an inbound history request with response bytes.
type SyncResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleSync registers the responder for inbound sync requests. Call once.
func (n *Host) HandleSync(responder SyncResponder) {
	n.h.SetStreamHandler(syncProtocol, func(s network.Stream) {
		defer s.Close()
		req, err := readFrame(s)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrame(s, resp)
	})
}

// RequestSync asks an already-connected peer for missed history.
func (n *Host) RequestSync(ctx context.Context, to peer.ID, request []byte) ([]byte, error) {
	s, err := n.h.NewStream(ctx, to, syncProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open sync stream: %w", err)
	}
	defer s.Close()
	if err := writeFrame(s, request); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s)
}
