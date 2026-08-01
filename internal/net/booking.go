package net

import (
	"bufio"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// BookingProtocol carries one public-booking request from the rendezvous
// gateway to the HOST whose booking page a visitor opened: one newline-
// terminated JSON line in, one newline-terminated JSON line back, then the
// stream closes. Line-delimited like the guest protocol (the gateway relays
// whole lines), not length-prefixed like invite/sync — the gateway side
// already speaks lines and never needs to buffer more than one.
//
// This layer is deliberately semantics-free: the app layer (booking.go)
// defines the JSON shape, validates the token the host minted, and decides
// what — if anything — to answer. A stream from anyone who is not carrying a
// token this host issued gets a refusal, not silence, so the page can be
// honest with the visitor.
const BookingProtocol protocol.ID = "/concord/booking/1.0.0"

// maxBookingRequest caps the visitor's single request line. A booking carries
// a token, a short name and a bounded note; anything bigger is hostile.
const maxBookingRequest = 8 << 10

// maxBookingResponse caps the host's reply (a slot list, or a meeting URL
// plus a small ICS file). Bounded on BOTH ends: the gateway enforces the same
// cap when it reads, so a compromised host cannot OOM the relay either.
const MaxBookingResponse = 128 << 10

// BookingResponder answers one booking request. It must always return a
// complete JSON object — the visitor's page is waiting on the reply, and EOF
// with nothing said renders as a mystery error in someone's browser.
type BookingResponder func(from peer.ID, request []byte) []byte

// HandleBookings registers the host-side handler for inbound booking
// requests. remote is the RELAY's peer ID (the rendezvous) — visitors have no
// libp2p identity; the token inside the request is what scopes the answer.
func (n *Host) HandleBookings(responder BookingResponder) {
	n.h.SetStreamHandler(BookingProtocol, func(s network.Stream) {
		defer s.Close()
		req, err := readCappedLine(bufio.NewReaderSize(s, maxBookingRequest+1), maxBookingRequest)
		if err != nil {
			return
		}
		resp := responder(s.Conn().RemotePeer(), req)
		if len(resp) == 0 || len(resp) > MaxBookingResponse {
			return
		}
		_, _ = s.Write(append(resp, '\n'))
	})
}

// readCappedLine reads one newline-delimited frame, bounding the accumulated
// bytes at max. bufio's ReadBytes would buffer an unbounded newline-less
// stream before any size check — an OOM lever for any peer that can open the
// stream. (Same defence, same shape, as the guest gateway's reader.)
func readCappedLine(r *bufio.Reader, max int) ([]byte, error) {
	buf := make([]byte, 0, 512)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return buf, err
		}
		if b == '\n' {
			return buf, nil
		}
		if len(buf) >= max {
			return nil, fmt.Errorf("net: booking frame exceeds cap")
		}
		buf = append(buf, b)
	}
}
