package app

import (
	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/meshtastic"
)

// LoRa through a Meshtastic node on USB or Wi-Fi. Configured by
// <dataDir>/meshtastic.json or CONCORD_MESHTASTIC; see docs/transports/meshtastic.md.
func init() {
	offgridFactories = append(offgridFactories, func(dataDir string) transport.Transport {
		return meshtastic.New(meshtastic.LoadConfig(dataDir), nil)
	})
}
