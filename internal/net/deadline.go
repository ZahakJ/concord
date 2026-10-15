package net

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
)

// Stream deadlines for the one-request, one-response protocols.
//
// A libp2p stream is only as patient as its deadline, and a context passed to
// NewStream stops applying the moment the stream is open. A peer that accepts a
// stream and never answers — a stalled process, a half-dead connection, a
// relay circuit whose relay has gone — otherwise holds the stream and the
// goroutine waiting on it for the life of the process. Every requester below
// therefore carries its deadline onto the stream, and every responder bounds
// its own end.

// serveTimeout bounds a responder's whole exchange on the small protocols
// (invite, DM invite, hello, link): read the request, answer it, write the
// reply. Generous, since an owner admitting a joiner commits to its group
// first; a peer that has not finished in two minutes is not going to.
const serveTimeout = 2 * time.Minute

// replyFloor is the least a requester waits for the reply on the protocols
// whose request has an effect the requester must not walk away from. An
// invite request makes the owner commit the joiner into the group, and a link
// request burns the issuer's single-use secret; giving up between the two
// halves strands the member (in the group with no Welcome) or burns the code
// for nothing. So those wait at least this long for the answer whatever their
// context says, and are still bounded.
const replyFloor = 2 * time.Minute

// streamIdle is how long a large transfer may go without moving a byte before
// it is abandoned. Progress-based rather than a wall-clock budget, because the
// attachment stream carries up to MaxAttachResponse: 32 MiB over a metered relay
// can take many minutes and is fine, while a stream that has not moved in
// thirty seconds is not going to.
const streamIdle = 30 * time.Second

// chunk is the unit a progress-bounded transfer refreshes its deadline on.
const chunk = 64 << 10

// replyDeadline is the context's deadline, but no earlier than floor from now.
func replyDeadline(ctx context.Context, ceiling, floor time.Duration) time.Time {
	d := streamDeadline(ctx, ceiling)
	if min := time.Now().Add(floor); d.Before(min) {
		return min
	}
	return d
}

// readFrameProgress reads one frame whose first byte must arrive by firstByte
// and whose body may then take as long as it keeps moving.
func readFrameProgress(s network.Stream, max int, firstByte time.Time) ([]byte, error) {
	_ = s.SetReadDeadline(firstByte)
	var hdr [4]byte
	if _, err := io.ReadFull(s, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint32(hdr[:]))
	if n > max {
		return nil, fmt.Errorf("net: frame too large: %d bytes (max %d)", n, max)
	}
	buf := make([]byte, n)
	for off := 0; off < n; {
		end := min(off+chunk, n)
		_ = s.SetReadDeadline(time.Now().Add(streamIdle))
		if _, err := io.ReadFull(s, buf[off:end]); err != nil {
			return nil, err
		}
		off = end
	}
	return buf, nil
}

// writeFrameProgress writes one frame, failing only if the far end stops
// taking bytes for streamIdle.
func writeFrameProgress(s network.Stream, data []byte, max int) error {
	if len(data) > max {
		return fmt.Errorf("net: frame too large: %d bytes (max %d)", len(data), max)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	_ = s.SetWriteDeadline(time.Now().Add(streamIdle))
	if _, err := s.Write(hdr[:]); err != nil {
		return err
	}
	for off := 0; off < len(data); {
		end := min(off+chunk, len(data))
		_ = s.SetWriteDeadline(time.Now().Add(streamIdle))
		if _, err := s.Write(data[off:end]); err != nil {
			return err
		}
		off = end
	}
	return nil
}
