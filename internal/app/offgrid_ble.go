//go:build linux && !android

package app

import (
	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/ble"
)

// Bluetooth LE through BlueZ. Registering it touches no radio: the adapter is
// only powered, advertised on or scanned with once the user enables the link.
func init() {
	offgridFactories = append(offgridFactories, func(string) transport.Transport { return ble.Default() })
}
