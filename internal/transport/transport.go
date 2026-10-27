// Package transport lets Concord's end-to-end encrypted traffic leave the
// internet: Bluetooth LE, an audio modem, QR codes and files, LoRa, satellite.
//
// Everything above this package is unchanged. The app still encrypts every
// channel message with MLS and signs it as its author before it reaches the
// network; what this package moves is that ciphertext and nothing else. It
// never sees a key, never sees plaintext, and has no way to ask for either. A
// link here is just a pipe for opaque bytes, and the security of a message
// that crossed a LoRa radio is exactly the security of one that crossed
// gossipsub: MLS decides whether it is genuine, and the author's signature
// inside it is what survives.
//
// The pieces:
//
//   - Transport (this file) is one physical link, described by its Caps: how
//     big a frame it carries, how fast, what it costs, whether ciphertext is
//     even legal on it, and whether it moves frames live or only when a person
//     carries them.
//   - Bundle (bundle.go) is the unit that travels: a compact header plus one
//     ciphertext, fragmented to fit a 200-byte radio frame and reassembled on
//     the far side.
//   - Manager (manager.go) owns the links: which are on, which a message goes
//     out on, deduplication across all of them, relaying for nearby phones,
//     and the outbox that holds frames for links a person must trigger or pay
//     for.
package transport

import (
	"context"
	"time"
)

// Mode says how a link moves frames.
type Mode string

const (
	// Live links send as soon as they are handed a frame: BLE, a LAN,
	// a LoRa node. The Manager fans every eligible message out on them.
	Live Mode = "live"
	// Manual links hold frames in an outbox until the person acts: plays the
	// audio, shows the QR sequence, writes the file, or approves the charge.
	// Nothing on a manual link is ever sent behind the user's back.
	Manual Mode = "manual"
)

// Caps is what a link can do, as plainly as the settings page shows it.
type Caps struct {
	// MTU is the largest frame Send accepts, in bytes. Bundles larger than
	// this are fragmented (bundle.go).
	MTU int `json:"mtu"`
	// Bitrate is the sustained useful throughput in bits per second — the
	// honest number, after framing and duty-cycle, not the PHY rate.
	Bitrate int `json:"bitrate"`
	// Latency is a typical one-hop delivery time for a single frame.
	Latency time.Duration `json:"latency"`
	// CostMicroUSD is what one frame costs the sender, in millionths of a
	// dollar. Any non-zero cost makes the link Manual regardless of Mode: a
	// paid frame waits in the outbox until the user approves the total.
	CostMicroUSD int64 `json:"costMicroUSD"`
	// Encryption is whether ciphertext may lawfully be sent on this medium.
	// Amateur (FCC §97.113) and GMRS/FRS (§95.1733) bands forbid it. Concord
	// only ever has ciphertext to send, so a link with this false is never
	// selected for anything — the field exists so such a link can be listed
	// and explained, not used.
	Encryption bool `json:"encryption"`
	// Local means the link needs no infrastructure: no internet, no tower, no
	// satellite. Offline mode uses only Local links.
	Local bool `json:"local"`
	// Mode is Live or Manual (see above).
	Mode Mode `json:"mode"`
	// Range is a human description of reach, e.g. "~30 m", "2–15 km".
	Range string `json:"range"`
}

// Paid reports whether sending on the link costs money.
func (c Caps) Paid() bool { return c.CostMicroUSD > 0 }

// State is a link's availability, independent of whether the user turned it
// on.
type State string

const (
	Available  State = "available"   // hardware present and usable
	NoHardware State = "no-hardware" // nothing to talk to (no adapter, no node, no modem)
	Failed     State = "error"       // hardware present but misbehaving; Detail says how
)

// Status is a point-in-time availability report.
type Status struct {
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// Deliver hands a frame that arrived on a link to the Manager.
type Deliver func(frame []byte)

// Transport is one physical link.
//
// Implementations must be safe for concurrent use. Start is only called once
// the user has enabled the link — a transport must not power a radio,
// advertise, open a microphone, or touch a serial port before that, because
// enabling a radio is the user's decision, not the app's.
type Transport interface {
	// Name is a short, stable identifier: "ble", "audio", "meshtastic".
	Name() string
	// Label is what the settings page calls it: "Bluetooth LE".
	Label() string
	Caps() Caps
	// Probe reports availability without starting anything: it may look for
	// an adapter or a serial device, but must not transmit.
	Probe() Status
	// Start begins receiving; every arriving frame goes to deliver. It returns
	// once the link is up; reception continues until ctx ends or Stop.
	Start(ctx context.Context, deliver Deliver) error
	Stop() error
	// Send transmits one frame of at most Caps().MTU bytes. For a Manual link
	// the Manager calls this only when the user releases the outbox.
	Send(ctx context.Context, frame []byte) error
}
