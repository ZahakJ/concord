//go:build linux && !android

package app

import (
	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/audio"
)

// The audio modem drives the desktop's own sound tools (pw-record/pw-play,
// parec/pacat, arecord/aplay). Phones get their own audio I/O through the
// native shell, not these subprocesses.
func init() {
	offgridFactories = append(offgridFactories, func(string) transport.Transport { return audio.New() })
}
