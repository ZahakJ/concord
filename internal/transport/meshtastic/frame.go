package meshtastic

import (
	"bufio"
	"errors"
	"io"
)

// The stream API, identical on USB serial and TCP port 4403: each protobuf is
// preceded by START1 START2 and a big-endian 16-bit length. On serial the
// node also prints its debug log as plain text between frames, so the reader
// resynchronises on START1/START2 and treats everything else as noise.
const (
	start1 = 0x94
	start2 = 0xC3
	// TCPPort is the Meshtastic firmware's stream API port.
	TCPPort = 4403
)

var errFrameTooBig = errors.New("meshtastic: protobuf exceeds 512 bytes")

// EncodeFrame wraps one protobuf for the stream.
func EncodeFrame(pb []byte) ([]byte, error) {
	if len(pb) > maxProto {
		return nil, errFrameTooBig
	}
	out := make([]byte, 4+len(pb))
	out[0], out[1] = start1, start2
	out[2], out[3] = byte(len(pb)>>8), byte(len(pb))
	copy(out[4:], pb)
	return out, nil
}

// FrameReader pulls framed protobufs out of a byte stream that may also carry
// log text, line noise and truncated frames.
type FrameReader struct {
	r *bufio.Reader
	// Noise counts bytes skipped while hunting for a frame start.
	Noise int
}

// NewFrameReader reads frames from r.
func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{r: bufio.NewReaderSize(r, 1024)}
}

// Next returns the next frame's protobuf. Only an error from the underlying
// stream ends it: a bad length is noise, not a reason to stop listening.
func (fr *FrameReader) Next() ([]byte, error) {
	for {
		b, err := fr.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b != start1 {
			fr.Noise++
			continue
		}
	second:
		b, err = fr.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == start1 {
			fr.Noise++
			goto second
		}
		if b != start2 {
			fr.Noise += 2
			continue
		}
		hdr, err := fr.r.Peek(2)
		if err != nil {
			return nil, err
		}
		n := int(hdr[0])<<8 | int(hdr[1])
		if n > maxProto {
			// Not a real header; the length bytes may begin the next frame.
			fr.Noise += 2
			continue
		}
		_, _ = fr.r.Discard(2)
		pb := make([]byte, n)
		if _, err := io.ReadFull(fr.r, pb); err != nil {
			return nil, err
		}
		return pb, nil
	}
}
