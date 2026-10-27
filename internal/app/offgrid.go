package app

import (
	"path/filepath"

	"github.com/ZahakJ/concord/internal/transport"
)

// offgridFactories builds this platform's non-internet links. Each link lives in
// its own file (offgrid_<name>.go) and appends itself here from init, so adding
// one never edits another's code.
var offgridFactories []func(dataDir string) transport.Transport

// newOffGrid registers every link. None of them starts until the user enables
// it in Settings → Off-grid links.
func newOffGrid(dataDir string) *transport.Manager {
	m := transport.NewManager(filepath.Join(dataDir, "transports.json"), nil)
	for _, f := range offgridFactories {
		if t := f(dataDir); t != nil {
			m.Register(t)
		}
	}
	return m
}

// Links returns the non-internet link manager.
func (s *Service) OffGrid() *transport.Manager { return s.offgrid }
