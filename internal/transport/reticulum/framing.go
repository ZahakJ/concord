package reticulum

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The pipe protocol between Concord and concord-rns.py: one type byte, a
// 2-byte big-endian length, then the payload. It is deliberately dumb — the
// sidecar is a pipe for opaque ciphertext, not a peer with opinions.
const (
	tSend     byte = 0x01 // Go → sidecar: fan this frame out to every peer
	tAnnounce byte = 0x02 // Go → sidecar: announce now
	tRecv     byte = 0x81 // sidecar → Go: a frame arrived
	tReady    byte = 0x82 // sidecar → Go: up; payload is our destination hash
	tError    byte = 0x83 // sidecar → Go: UTF-8 reason
	tPeers    byte = 0x84 // sidecar → Go: 2-byte peer count

	headerLen = 3
	// maxPipeFrame bounds what we will read from the sidecar. Nothing it
	// legitimately sends comes near it; a larger length means garbage on
	// stdout, and the process is killed.
	maxPipeFrame = 4096
)

var errPipeFrame = errors.New("reticulum: malformed frame from sidecar")

// encodeFrame builds one pipe frame.
func encodeFrame(kind byte, payload []byte) ([]byte, error) {
	if len(payload) > maxPipeFrame {
		return nil, fmt.Errorf("reticulum: %d-byte pipe frame exceeds %d", len(payload), maxPipeFrame)
	}
	b := make([]byte, headerLen+len(payload))
	b[0] = kind
	binary.BigEndian.PutUint16(b[1:3], uint16(len(payload)))
	copy(b[headerLen:], payload)
	return b, nil
}

// readFrame reads one pipe frame. Any length over maxPipeFrame is an error,
// so a confused or hostile child can never make us allocate more than that.
func readFrame(r io.Reader) (kind byte, payload []byte, err error) {
	var h [headerLen]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	n := int(binary.BigEndian.Uint16(h[1:3]))
	if n > maxPipeFrame {
		return 0, nil, errPipeFrame
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}
	return h[0], payload, nil
}
