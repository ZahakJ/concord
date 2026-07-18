package net

import (
	"context"
	"fmt"
	"time"

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
//
// Getting a connection + stream to the issuer is the flaky part: off-LAN it
// rides a relay reservation and a hole-punch that routinely need several seconds
// and a couple of attempts to settle. So the connect-and-open phase RETRIES with
// backoff until ctx expires, reporting the last real error rather than a bare
// "context deadline exceeded". No request frame is sent during that phase, so
// retrying is safe. Only once the stream is open do we deliver the request
// (which burns the issuer's single-use secret) — that part runs exactly once.
func (n *Host) RequestLink(ctx context.Context, issuer peer.AddrInfo, request []byte) ([]byte, error) {
	s, err := n.dialLinkStream(ctx, issuer)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	if err := writeFrame(s, request, maxLinkFrame); err != nil {
		return nil, fmt.Errorf("net: send link request: %w", err)
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, maxLinkFrame)
}

// dialLinkStream connects to the issuer and opens the link stream, retrying the
// whole connect+open with capped exponential backoff until ctx is done.
func (n *Host) dialLinkStream(ctx context.Context, issuer peer.AddrInfo) (network.Stream, error) {
	backoff := time.Second
	var lastErr error
	for attempt := 0; ; attempt++ {
		// Each attempt gets a bounded slice of the overall budget, so one wedged
		// dial can't consume the entire deadline before we retry over a fresh path.
		attemptCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if n.h.Network().Connectedness(issuer.ID) != network.Connected {
			lastErr = n.h.Connect(attemptCtx, issuer)
		} else {
			lastErr = nil
		}
		if lastErr == nil {
			s, serr := n.h.NewStream(attemptCtx, issuer.ID, linkProtocol)
			cancel()
			if serr == nil {
				return s, nil
			}
			lastErr = fmt.Errorf("open link stream: %w", serr)
		} else {
			cancel()
			lastErr = fmt.Errorf("connect to linking device: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			// Surface the concrete reachability failure, not the deadline.
			if lastErr != nil {
				return nil, fmt.Errorf("net: couldn't reach the other device (is it still on the linking screen, and are both devices online?): %w", lastErr)
			}
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
}
