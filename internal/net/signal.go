package net

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// signalProtocol carries WebRTC signaling (SDP offers/answers and ICE
// candidates) directly between two peers for voice/video. Media itself never
// touches Go — it flows browser-to-browser over WebRTC (DTLS-SRTP). This
// protocol only relays the small, opaque signaling blobs that let two browsers
// establish that peer connection. Each message is a single length-framed
// payload, fire-and-forget (no response).
const signalProtocol protocol.ID = "/concord/signal/1.0.0"

// SignalHandler receives a signaling payload from a peer.
type SignalHandler func(from peer.ID, data []byte)

// HandleSignals registers a handler for inbound signaling messages. Call once.
func (n *Host) HandleSignals(handler SignalHandler) {
	n.h.SetStreamHandler(signalProtocol, func(s network.Stream) {
		defer s.Close()
		data, err := readFrame(s)
		if err != nil {
			return
		}
		handler(s.Conn().RemotePeer(), data)
	})
}

// SendSignal delivers a signaling payload to a specific peer. It assumes a
// connection already exists (voice peers discover each other via presence and
// are already connected); it does not dial.
func (n *Host) SendSignal(ctx context.Context, to peer.ID, data []byte) error {
	s, err := n.h.NewStream(ctx, to, signalProtocol)
	if err != nil {
		return fmt.Errorf("net: open signal stream: %w", err)
	}
	defer s.Close()
	if err := writeFrame(s, data); err != nil {
		return fmt.Errorf("net: write signal: %w", err)
	}
	return nil
}
