package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
)

// FileLink is sneakernet: frames leave in a file a person carries — a USB
// stick, an email attachment, a photo of a screen — and come back in through
// Import. It is the purest store-and-forward link there is, and the one that
// works when every radio is off.
//
// Export drains the outbox (Manager.Export); this type only describes the link
// and refuses a direct Send, because there is nowhere for it to go until the
// user saves the file.
type FileLink struct{}

func (FileLink) Name() string  { return "file" }
func (FileLink) Label() string { return "File / USB" }
func (FileLink) Caps() Caps {
	return Caps{MTU: 64 << 10, Encryption: true, Local: true, Mode: Manual, Range: "wherever you carry it"}
}
func (FileLink) Probe() Status                        { return Status{State: Available} }
func (FileLink) Start(context.Context, Deliver) error { return nil }
func (FileLink) Stop() error                          { return nil }
func (FileLink) Send(context.Context, []byte) error   { return errExportOnly }

var errExportOnly = errors.New("transport: save this link's outbox as a file instead")

// A pack is the file form: a magic line, then each frame as a uvarint length
// and its bytes. Everything in it is ciphertext plus bundle headers.
var packMagic = []byte("CONCORD-BUNDLES/1\n")

const (
	maxPackFrames = 8192
	maxPackBytes  = 32 << 20
)

// EncodePack writes frames as a pack.
func EncodePack(frames [][]byte) []byte {
	var buf bytes.Buffer
	buf.Write(packMagic)
	var n [binary.MaxVarintLen64]byte
	for _, f := range frames {
		buf.Write(n[:binary.PutUvarint(n[:], uint64(len(f)))])
		buf.Write(f)
	}
	return buf.Bytes()
}

// DecodePack reads a pack, refusing anything oversized or malformed.
func DecodePack(raw []byte) ([][]byte, error) {
	if len(raw) > maxPackBytes || !bytes.HasPrefix(raw, packMagic) {
		return nil, ErrMalformed
	}
	raw = raw[len(packMagic):]
	var out [][]byte
	for len(raw) > 0 {
		if len(out) >= maxPackFrames {
			return nil, ErrMalformed
		}
		l, n := binary.Uvarint(raw)
		if n <= 0 || l == 0 || l > uint64(len(raw)-n) {
			return nil, ErrMalformed
		}
		out = append(out, append([]byte(nil), raw[n:n+int(l)]...))
		raw = raw[n+int(l):]
	}
	return out, nil
}
