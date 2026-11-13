package net

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// profileProtocol hands an account its own profile record back: "what do you
// hold that my account published about itself?". It is a stream of its own
// rather than a field of the hello because a profile carries pictures — an
// avatar up to 64 KiB and a banner up to 256 KiB — and the hello frame is held
// to 64 KiB by every build that reads it. Like the other streams it is
// semantics-free here: one opaque request, one opaque answer, and the app
// decides whose record it may give (only the requester's own, keyed by the key
// the connection is authenticated to) and verifies what comes back.
const profileProtocol protocol.ID = "/concord/profile/1.0.0"

// maxProfileFrame bounds one record: the pictures above, the orbit sprite and
// the game list, as JSON text, with headroom.
const maxProfileFrame = 1 << 20

// ProfileResponder answers an inbound request. Returning an error drops the
// stream without an answer.
type ProfileResponder func(ctx context.Context, from peer.ID, request []byte) ([]byte, error)

// HandleProfile registers the responder side.
func (n *Host) HandleProfile(responder ProfileResponder) {
	n.h.SetStreamHandler(profileProtocol, func(s network.Stream) {
		defer s.Close()
		_ = s.SetDeadline(time.Now().Add(serveTimeout))
		req, err := readFrame(s, 4<<10)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrameProgress(s, resp, maxProfileFrame)
	})
}

// AskProfile asks an already-connected peer for our record. Like the hello it
// never dials: it is worth a stream on a connection that exists, not a
// connection of its own. A peer on an older build has no handler, and the
// stream open fails, which is the same answer as "I hold nothing".
func (n *Host) AskProfile(ctx context.Context, p peer.ID, request []byte) ([]byte, error) {
	if c := n.h.Network().Connectedness(p); c != network.Connected && c != network.Limited {
		return nil, fmt.Errorf("net: profile: %s is not connected", p)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, err := n.newStream(ctx, p, profileProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open profile stream: %w", err)
	}
	defer s.Close()
	_ = s.SetDeadline(streamDeadline(ctx, serveTimeout))
	if err := writeFrame(s, request, 4<<10); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrameProgress(s, maxProfileFrame, time.Now().Add(streamIdle))
}
