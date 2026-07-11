package net

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// linkProtocol is the device-linking handshake stream. The joiner (a new device)
// dials the issuer (an already-unlocked device that displayed the QR), proves
// knowledge of the linking secret, and receives the account material. Like the
// invite stream, this layer is semantics-free: it moves one opaque request/
// response frame; the app layer defines the JSON shape and the crypto. The
// connection is Noise-encrypted by libp2p, and the mutual secret proof inside
// the frames authenticates both ends against a man-in-the-middle.
const linkProtocol protocol.ID = "/concord/link/1.0.0"

// maxLinkFrame caps a link frame. The response carries the account seed, profile,
// bootstrap config, and guild list — kept well under 1 MiB.
const maxLinkFrame = 1 << 20

// LinkResponder handles an inbound link request and returns the response bytes.
// Returning an error aborts the stream (a failed secret proof, expired offer).
type LinkResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleLink registers the issuer-side link stream handler. Call once after the
// host is up; the app supplies the responder that verifies the joiner's proof
// and produces the account material.
func (n *Host) HandleLink(responder LinkResponder) {
	n.h.SetStreamHandler(linkProtocol, func(s network.Stream) {
		defer s.Close()
		req, err := readFrame(s, maxLinkFrame)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrame(s, resp, maxLinkFrame)
	})
}

// RequestLink dials the issuer (from the QR's address info) and performs the
// joiner side of the handshake, returning the issuer's response.
func (n *Host) RequestLink(ctx context.Context, issuer peer.AddrInfo, request []byte) ([]byte, error) {
	if err := n.h.Connect(ctx, issuer); err != nil {
		return nil, fmt.Errorf("net: connect to linking device: %w", err)
	}
	s, err := n.h.NewStream(ctx, issuer.ID, linkProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open link stream: %w", err)
	}
	defer s.Close()
	if err := writeFrame(s, request, maxLinkFrame); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, maxLinkFrame)
}
