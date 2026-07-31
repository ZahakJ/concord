package net

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// inviteProtocol is the libp2p stream protocol for the guild join handshake.
// A joiner opens a stream to the guild owner, sends a request (its MLS
// KeyPackage), and receives a response (an MLS Welcome plus guild metadata).
// This layer is deliberately semantics-free: it moves opaque request/response
// byte frames, and the app layer defines their JSON shape and MLS meaning.
const inviteProtocol protocol.ID = "/concord/invite/1.0.0"

// maxInviteFrame caps a single handshake frame to defend against a peer
// claiming an enormous length. KeyPackages/Welcomes are a few KiB.
const maxInviteFrame = 1 << 20 // 1 MiB

// InviteResponder handles an inbound join request and produces the response
// bytes to send back. Returning an error aborts the stream without a response.
type InviteResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleInvites registers a stream handler that runs responder for each inbound
// join request. Call once after the host is up.
func (n *Host) HandleInvites(responder InviteResponder) {
	n.h.SetStreamHandler(inviteProtocol, func(s network.Stream) {
		defer s.Close()
		req, err := readFrame(s, maxInviteFrame)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return // no response; joiner will see EOF and fail
		}
		_ = writeFrame(s, resp, maxInviteFrame)
	})
}

// RequestInvite dials owner, sends request, and returns the owner's response.
// The caller supplies the owner's AddrInfo (carried in the invite code).
func (n *Host) RequestInvite(ctx context.Context, owner peer.AddrInfo, request []byte) ([]byte, error) {
	if err := n.h.Connect(ctx, owner); err != nil {
		return nil, fmt.Errorf("net: connect to guild owner: %w", err)
	}
	s, err := n.newStream(ctx, owner.ID, inviteProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open invite stream: %w", err)
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

// Framing: a 4-byte big-endian length prefix followed by the payload. Each
// protocol passes its own max frame size (invite/sync stay small; attachment
// responses are allowed to be much larger).
func writeFrame(w io.Writer, data []byte, max int) error {
	if len(data) > max {
		return fmt.Errorf("net: frame too large: %d bytes (max %d)", len(data), max)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func readFrame(r io.Reader, max int) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if int64(n) > int64(max) {
		return nil, fmt.Errorf("net: frame too large: %d bytes (max %d)", n, max)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
