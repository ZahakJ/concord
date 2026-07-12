//go:build windows

package bridge

import (
	"os"
	"os/exec"
	"strings"
	"time"
)

// RestartApp relaunches the (freshly updated) executable. Windows has no
// exec(2), so: spawn the new binary detached, then exit — the child retries
// binding until this process has released the port.
func (b *Bridge) RestartApp() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// After a self-update the running image was renamed to <exe>.old and the
	// OS reports that name; the fresh binary sits at the original path.
	exe = strings.TrimSuffix(exe, ".old")
	time.AfterFunc(400*time.Millisecond, func() {
		b.Close()
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		_ = cmd.Start()
		os.Exit(0)
	})
	return nil
}
