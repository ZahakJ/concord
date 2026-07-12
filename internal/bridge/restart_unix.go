//go:build !windows

package bridge

import (
	"os"
	"strings"
	"syscall"
	"time"
)

// RestartApp relaunches the (freshly updated) executable in place. The RPC
// response is given a beat to flush, then exec replaces the process image —
// Go sockets are close-on-exec, so the listener frees and the new binary
// rebinds the same port. The UI polls until the backend answers again.
func (b *Bridge) RestartApp() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// After a self-update the running image was RENAMED to <exe>.old, and
	// /proc/self/exe faithfully reports that. The fresh binary lives at the
	// original path — exec that, not our own parked corpse.
	exe = strings.TrimSuffix(exe, ".old")
	time.AfterFunc(400*time.Millisecond, func() {
		b.Close() // flush/close the store cleanly before the image swap
		_ = syscall.Exec(exe, os.Args, os.Environ())
		os.Exit(0) // exec failed; exit so a supervisor/user can restart
	})
	return nil
}
