//go:build !wails

package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/cli/clitest"
)

// The release binary is stamped through -ldflags, which a test binary never
// is; this builds the web binary the way `make release` does and asks it.
func TestReleaseBuildPrintsItsStampedVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "concord")
	build := exec.Command("go", "build", "-trimpath", "-o", bin,
		"-ldflags", "-X github.com/ZahakJ/concord/internal/version.Version=v0.0.0-clitest", ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	sb := clitest.NewSandbox(t)
	sb.Extra = []string{"CONCORD_WEB_ADDR=" + holdWebPort(t)}
	r := sb.RunBinary(t, 20*time.Second, bin, "--version")
	want := "concord v0.0.0-clitest (web, " + runtime.GOOS + "/" + runtime.GOARCH + ")\n"
	if r.Code != 0 || r.Stdout != want || r.Stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q; want 0 %q", r.Code, r.Stdout, r.Stderr, want)
	}
}

// A copy built with a rendezvous says so in --version and --help; one built
// without says nothing about it.
func TestBuiltInRendezvousIsNamedByTheBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	for _, rdv := range []string{"", "/dns4/rdv.example.com/tcp/4001/p2p/12D3KooWCzonwxmETSLwgDY9JAe9WczUHssnYTrSpCqkp6UXZg1q"} {
		bin := filepath.Join(t.TempDir(), "concord")
		build := exec.Command("go", "build", "-trimpath", "-o", bin,
			"-ldflags", "-X github.com/ZahakJ/concord/internal/version.Rendezvous="+rdv, ".")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
		sb := clitest.NewSandbox(t)
		sb.Extra = []string{"CONCORD_WEB_ADDR=" + holdWebPort(t)}
		v := sb.RunBinary(t, 20*time.Second, bin, "--version")
		h := sb.RunBinary(t, 20*time.Second, bin, "--help")
		named := strings.Contains(v.Stdout, "built-in rendezvous: rdv.example.com") &&
			strings.Contains(h.Stdout, "Built-in rendezvous: rdv.example.com")
		if (rdv != "") != named {
			t.Fatalf("rdv %q: --version %q, --help names it: %v", rdv, v.Stdout, strings.Contains(h.Stdout, "Built-in rendezvous"))
		}
	}
}
