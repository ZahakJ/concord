// Package qr is the QR-code link: queued frames leave as an animated
// sequence of QR codes on one screen and come in through another device's
// camera. No radio, no cable, no network — only light, which makes it the
// link for an air-gapped machine or a border crossing with a phone.
//
// The Go side only describes the link; the sequence is drawn and scanned in
// the UI (frontend/src/lib/qrseq.js). Each code carries exactly one transport
// frame, so the Manager's fragmentation and reassembly do all the work and a
// partly scanned sequence still delivers every message whose frames all made
// it. What a code holds is a bundle header and MLS ciphertext, base64 — the
// same bytes gossip carries.
package qr

import (
	"context"
	"errors"

	"github.com/ZahakJ/concord/internal/transport"
)

// MTU keeps one frame (base64, plus the short sequence header) inside a
// version-12 QR code at error-correction level M: 65×65 modules, which a phone
// camera reads off a laptop screen at arm's length.
const MTU = 192

// Link is the QR-code sequence link. Like the file link it is Manual and
// export-only: the outbox is drained by Manager.Export when the user opens the
// sequence, and frames arrive through Manager.Receive as the camera reads them.
type Link struct{}

func (Link) Name() string  { return "qr" }
func (Link) Label() string { return "QR codes (screen to camera)" }
func (Link) Caps() transport.Caps {
	return transport.Caps{
		MTU: MTU,
		// ~3 codes a second shown, and a camera misses some on each pass.
		Bitrate:    2000,
		Encryption: true,
		Local:      true,
		Mode:       transport.Manual,
		Range:      "line of sight to the screen",
	}
}
func (Link) Probe() transport.Status                        { return transport.Status{State: transport.Available} }
func (Link) Start(context.Context, transport.Deliver) error { return nil }
func (Link) Stop() error                                    { return nil }
func (Link) Send(context.Context, []byte) error             { return ErrShowOnly }

// ErrShowOnly is Send's answer: there is nowhere for a frame to go until the
// user puts the sequence on screen.
var ErrShowOnly = errors.New("qr: show this link's outbox as QR codes instead")
