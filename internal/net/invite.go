package net

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"time"

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
		_ = s.SetDeadline(time.Now().Add(serveTimeout))
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
	// Never shorter than replyFloor: the owner commits us into the group on
	// this request, and leaving before its Welcome arrives strands us there.
	_ = s.SetDeadline(replyDeadline(ctx, serveTimeout, replyFloor))

	if err := sendRequest(s, request, maxInviteFrame); err != nil {
		return nil, err
	}
	return readFrame(s, maxInviteFrame)
}

// sendRequest writes a request's one frame and half-closes the stream.
//
// The half-close is a courtesy: every responder reads exactly one frame and
// never waits for the end of the stream. A responder that has already read the
// frame, answered and closed its end has also stopped reading, and on QUIC that
// cancels our sending side, so CloseWrite fails ("close called for canceled
// stream") while the answer sits in the stream waiting to be read. Under load
// that order is common. Failing the request there threw a good answer away: an
// archive page was reported as held by nobody, a heal or a catch-up was counted
// as unanswered. The read that follows decides: it gets the answer, or the
// error that really ended the stream.
func sendRequest(s network.Stream, request []byte, max int) error {
	if err := writeFrame(s, request, max); err != nil {
		return err
	}
	if testHookRequestWritten != nil {
		testHookRequestWritten()
	}
	_ = s.CloseWrite()
	return nil
}

// testHookRequestWritten runs between a request's frame and its half-close,
// so a test can let the responder answer and close first. Nothing in the
// product sets it.
var testHookRequestWritten func()

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
