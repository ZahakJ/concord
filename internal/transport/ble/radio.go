package ble

import (
	"context"
	"crypto/rand"

	"github.com/ZahakJ/concord/internal/transport"
)

// Radio is the platform layer under a Link: BlueZ over D-Bus on Linux
// (bluez_linux.go), a fake in tests. It knows GATT and advertising; it does
// not know about peers, tie-breaks, rotation or frames — that is Link's job,
// so all of that is testable without a radio.
type Radio interface {
	// Probe reports availability. It must not transmit, advertise, scan or
	// power anything on.
	Probe() transport.Status
	// Up readies the adapter (it may power it on — the user enabled the
	// link), serves the Concord GATT service, and from then on reports
	// everything that happens through ev until Down.
	Up(ctx context.Context, ev Events) error
	// Advertise replaces the current advertisement with one carrying only
	// the Concord service UUID and data as its service data.
	Advertise(data []byte) error
	// Scan turns discovery of other Concord devices on or off.
	Scan(on bool) error
	// Connect opens a GATT connection to a device reported by Seen,
	// subscribes to its notifications and returns a handle for writing.
	Connect(ctx context.Context, id string) (PeerConn, error)
	// Notify sends frame to every central subscribed to our service.
	Notify(frame []byte) error
	// Down undoes everything Up and Advertise did: stops scanning, removes
	// the advertisement and the GATT service, and restores the adapter.
	Down() error
}

// Events is how a Radio reports what happens on the air. Every callback must
// be safe to call from any goroutine, and none may be called after Down
// returns.
type Events struct {
	// Inbound is a frame a central wrote to our service.
	Inbound func(frame []byte)
	// Seen is a device advertising the Concord service. ad is its service
	// data (nil if it advertised the UUID without data, as iOS must);
	// connected says whether a link to it already exists in either direction.
	Seen func(id string, ad []byte, connected bool)
	// Lost is a device that disconnected or vanished.
	Lost func(id string)
	// Notified is a frame a peripheral we connected to sent us.
	Notified func(id string, frame []byte)
	// Subscribed says whether any central is subscribed to our notifications.
	Subscribed func(on bool)
}

// PeerConn is one outbound GATT connection.
type PeerConn interface {
	Write(ctx context.Context, frame []byte) error
	Close() error
}

// The advertisement's service data is exactly this and nothing else:
//
//	[0]   adVersion
//	[1:5] nonce — random, regenerated every rotation, used only to decide
//	      which of two devices dials the other
//
// No key, no name, no counter, nothing derived from identity: five bytes that
// are fresh noise every ~15 minutes.
const (
	adVersion = 1
	nonceLen  = 4
	adLen     = 1 + nonceLen
)

type nonce [nonceLen]byte

func newNonce() nonce {
	var n nonce
	_, _ = rand.Read(n[:])
	return n
}

func encodeAd(n nonce) []byte {
	out := make([]byte, adLen)
	out[0] = adVersion
	copy(out[1:], n[:])
	return out
}

// decodeAd returns the peer's nonce. ok is false for data that is not ours;
// a nil ad (UUID-only advertiser) decodes to (nil, true).
func decodeAd(ad []byte) (n []byte, ok bool) {
	if ad == nil {
		return nil, true
	}
	if len(ad) != adLen || ad[0] != adVersion {
		return nil, false
	}
	return append([]byte(nil), ad[1:]...), true
}
