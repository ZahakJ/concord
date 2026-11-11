package app

import (
	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/reticulum"
)

// Reticulum (RNS) mesh via the concord-rns.py sidecar. Registering it starts
// nothing: Python runs only once the user enables the link.
func init() {
	offgridFactories = append(offgridFactories, func(dataDir string) transport.Transport { return reticulum.New(dataDir) })
}
