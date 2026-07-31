package net

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// dmInviteProtocol pushes a direct-message invitation to a peer. Starting a DM
// reuses the whole guild invite handshake — the initiator creates a 2-person
// MLS group and this stream just hands the recipient the invite code so their
// client auto-redeems it (dials back, joins). Semantics-free: one framed
// request in, one small ack out.
const dmInviteProtocol protocol.ID = "/concord/dm-invite/1.0.0"

// DMInviteResponder handles an inbound DM invitation.
type DMInviteResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleDMInvites registers the responder for inbound DM invitations.
func (n *Host) HandleDMInvites(responder DMInviteResponder) {
	n.h.SetStreamHandler(dmInviteProtocol, func(s network.Stream) {
		defer s.Close()
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

// RequestDMInvite pushes a DM invitation to an already-connected peer.
func (n *Host) RequestDMInvite(ctx context.Context, to peer.ID, request []byte) ([]byte, error) {
	s, err := n.newStream(ctx, to, dmInviteProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open dm-invite stream: %w", err)
	}
	defer s.Close()
	if err := writeFrame(s, request, maxInviteFrame); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, maxInviteFrame)
}
