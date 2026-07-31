package net

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// helloProtocol is the identity introduction: one peer tells another which
// ACCOUNT it belongs to, by presenting the account-signed certificate for the
// device key its PeerID is built on.
//
// It exists because a linked device's PeerID is its own device key, which no
// observer can map to an account on its own. Everything upstairs — "is this
// person a member", "is this my own phone", the presence feed, history catch-up,
// even the name in a voice room — asks that question, and until now the only
// answers came from unrelated traffic: a group roster we happened to be able to
// read, a message we happened to decrypt, a join handshake. A device that
// connected and then said nothing stayed a stranger for the whole session; its
// history sync was refused, its voice presence was unattributable, and the UI
// dropped it from the presence feed entirely.
//
// Like the invite and link streams this layer is semantics-free: it moves one
// opaque request/response frame and the app layer defines their meaning and does
// the verification. The security property that makes it safe lives one layer up
// and is worth stating here: the connection is Noise-authenticated to the remote
// device key, so the app can require the presented certificate to name THAT key.
// A peer can therefore only ever claim its own account-signed cert; replaying
// somebody else's proves nothing.
const helloProtocol protocol.ID = "/concord/hello/1.0.0"

// maxHelloFrame caps a hello frame. A frame is a certificate and a label — a
// few hundred bytes.
const maxHelloFrame = 1 << 16

// HelloResponder handles an inbound hello and returns the reply bytes. Returning
// an error aborts the stream without a reply — which is also how the app declines
// to introduce itself to a peer it cannot place.
type HelloResponder func(ctx context.Context, from peer.ID, request []byte) (response []byte, err error)

// HandleHello registers the responder side. Call once after the host is up.
func (n *Host) HandleHello(responder HelloResponder) {
	n.h.SetStreamHandler(helloProtocol, func(s network.Stream) {
		defer s.Close()
		req, err := readFrame(s, maxHelloFrame)
		if err != nil {
			return
		}
		resp, err := responder(n.ctx, s.Conn().RemotePeer(), req)
		if err != nil {
			return
		}
		_ = writeFrame(s, resp, maxHelloFrame)
	})
}

// SayHello introduces us to an already-connected peer and returns its reply.
//
// Deliberately no dial: a hello is worth one stream on a connection that already
// exists, and never worth establishing a connection of its own. If the peer is
// gone the caller learns nothing, which is the same as not having asked.
func (n *Host) SayHello(ctx context.Context, p peer.ID, request []byte) ([]byte, error) {
	if n.h.Network().Connectedness(p) != network.Connected {
		return nil, fmt.Errorf("net: hello: %s is not connected", p)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s, err := n.h.NewStream(relayCtx(ctx, "hello"), p, helloProtocol)
	if err != nil {
		return nil, fmt.Errorf("net: open hello stream: %w", err)
	}
	defer s.Close()
	if err := writeFrame(s, request, maxHelloFrame); err != nil {
		return nil, err
	}
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, maxHelloFrame)
}
