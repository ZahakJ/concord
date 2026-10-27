//go:build unix

package reticulum

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A grandchild that inherits stdout must not keep Stop waiting, and must die
// with the sidecar.
func TestStopKillsGrandchildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	l := fakeSidecar(t, `printf '\202\000\000'; sleep 60 & echo $! > '`+pidFile+`'; cat >/dev/null`)
	if err := l.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	var pid int
	waitUntil(t, 2*time.Second, "pid file", func() bool {
		raw, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		return pid > 0
	})
	t0 := time.Now()
	_ = l.Stop()
	if d := time.Since(t0); d > stopGrace+2*time.Second {
		t.Fatalf("Stop took %v", d)
	}
	waitUntil(t, 3*time.Second, "grandchild death", func() bool {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return true
		}
		// A zombie awaiting its reaper counts as dead.
		raw, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		return strings.Contains(string(raw), ") Z ")
	})
}
