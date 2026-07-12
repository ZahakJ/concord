package net

import (
	"io"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// GuestProtocol carries a browser guest's meeting session: the rendezvous
// node opens one long-lived stream per guest (relaying that guest's
// WebSocket) and the HOST terminates it — validating the token and bridging
// chat into exactly one meeting. Line-delimited JSON both ways.
const GuestProtocol protocol.ID = "/concord/guest/1.0.0"

// GuestSessionHandler serves one guest stream. remote is the RELAY's peer ID
// (the rendezvous), not the guest — guests have no libp2p identity; the
// token inside the stream is what authenticates the session.
type GuestSessionHandler func(conn io.ReadWriteCloser, remote peer.ID)

// HandleGuestSessions registers the host-side handler for inbound guest
// streams. Unlike the request/response protocols, these live for the whole
// guest visit.
func (n *Host) HandleGuestSessions(h GuestSessionHandler) {
	n.h.SetStreamHandler(GuestProtocol, func(s network.Stream) {
		h(s, s.Conn().RemotePeer())
	})
}
