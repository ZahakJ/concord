package ble

import (
	"context"

	"github.com/ZahakJ/concord/internal/transport"
)

// Radio is the platform layer under a Link: BlueZ over D-Bus on Linux
// (bluez_linux.go), a fake in tests, and on phones a native class behind
// gomobile. It knows GATT and advertising. Peers, dial policy, backoff and
// eviction belong to Link, so all of that is testable without a radio.
type Radio interface {
	// Probe reports availability. It must not transmit, advertise, scan or
	// power anything on.
	Probe() transport.Status
	// Up readies the adapter (it may power it on, because the user enabled
	// the link), serves the Concord GATT service, and reports everything that
	// happens through ev until Down.
	Up(ctx context.Context, ev Events) error
	// Advertise registers the advertisement. It carries the Concord service
	// UUID and nothing else.
	Advertise() error
	// Scan turns discovery of other Concord devices on or off.
	Scan(on bool) error
	// Connect opens a GATT connection to a device reported by Seen,
	// subscribes to its notifications and returns a handle for writing.
	Connect(ctx context.Context, id string) (PeerConn, error)
	// Notify sends frame to every central subscribed to our service. It must
	// be harmless when nobody is subscribed.
	Notify(frame []byte) error
	// Down undoes everything Up and Advertise did. It stops scanning, removes
	// the advertisement and the GATT service, disconnects every Concord peer
	// in either direction, and only then restores the adapter.
	Down() error
}

// Events is how a Radio reports what happens on the air. Every callback must
// be safe to call from any goroutine. None may be called after Down returns.
type Events struct {
	// Inbound is a frame a central wrote to our service.
	Inbound func(frame []byte)
	// Seen is a device advertising the Concord service that was just heard on
	// air. connected says whether a link to it already exists in either
	// direction.
	Seen func(id string, connected bool)
	// Lost is a device that disconnected or vanished.
	Lost func(id string)
	// Notified is a frame that a peripheral we connected to sent us.
	Notified func(id string, frame []byte)
	// Subscribed says whether any central is subscribed to our notifications.
	Subscribed func(on bool)
}

// PeerConn is one outbound GATT connection.
type PeerConn interface {
	Write(ctx context.Context, frame []byte) error
	Close() error
}
