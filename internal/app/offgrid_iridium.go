package app

import (
	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/iridium"
)

// Satellite through a RockBLOCK 9603. Paid per frame, so the Manager holds
// everything in the outbox until the user approves the cost. Configured by
// <dataDir>/iridium.json or CONCORD_IRIDIUM / CONCORD_IRIDIUM_PEERS; see
// docs/transports/iridium.md.
func init() {
	offgridFactories = append(offgridFactories, func(dataDir string) transport.Transport {
		return iridium.New(iridium.LoadConfig(dataDir), nil)
	})
}
