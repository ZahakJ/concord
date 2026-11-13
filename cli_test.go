package main

import (
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/cli"
	"github.com/ZahakJ/concord/internal/cli/clitest"
)

func TestMain(m *testing.M) {
	clitest.Main("concord", main)
	os.Exit(m.Run())
}

// holdWebPort takes the address the web build would serve on and keeps it, so
// a child that wrongly starts the app fails to bind (exit 1 after its retries)
// instead of quietly serving: answering --version must not even try to listen.
func holdWebPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

// The app binaries used to ignore their arguments: `--version` opened the
// default profile, bound the default port and (desktop) relocated the binary
// into the install home. Every case here must answer and exit having written
// nothing and opened nothing.
func TestCommandLineAnswersWithoutStartingTheApp(t *testing.T) {
	wantVersion := "concord dev (" + flavour + ", " + runtime.GOOS + "/" + runtime.GOARCH + ")\n"
	cases := []struct {
		args       []string
		code       int
		stdout     string // exact, when set
		stderrHas  []string
		stdoutHelp bool
	}{
		{args: []string{"--version"}, code: 0, stdout: wantVersion},
		{args: []string{"-version"}, code: 0, stdout: wantVersion},
		{args: []string{"--help"}, code: 0, stdoutHelp: true},
		{args: []string{"-help"}, code: 0, stdoutHelp: true},
		{args: []string{"-h"}, code: 0, stdoutHelp: true},
		{args: []string{"--no-such-flag"}, code: 2, stderrHas: []string{"flag provided but not defined: -no-such-flag", "Usage: concord"}},
		{args: []string{"-v"}, code: 2, stderrHas: []string{"flag provided but not defined: -v", "Usage: concord"}},
		{args: []string{"version"}, code: 2, stderrHas: []string{`unexpected argument "version"`, "Usage: concord"}},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			sb := clitest.NewSandbox(t)
			sb.Extra = []string{"CONCORD_WEB_ADDR=" + holdWebPort(t)}
			r := sb.Run(t, 20*time.Second, c.args...)
			if r.Code != c.code {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.Code, c.code, r.Stdout, r.Stderr)
			}
			if c.stdout != "" && r.Stdout != c.stdout {
				t.Errorf("stdout %q, want %q", r.Stdout, c.stdout)
			}
			if c.stdoutHelp && r.Stdout != usage {
				t.Errorf("stdout is not the usage text:\n%s", r.Stdout)
			}
			if c.code == 0 && r.Stderr != "" {
				t.Errorf("unexpected stderr: %s", r.Stderr)
			}
			for _, s := range c.stderrHas {
				if !strings.Contains(r.Stderr, s) {
					t.Errorf("stderr lacks %q:\n%s", s, r.Stderr)
				}
			}
			if c.code != 0 && r.Stdout != "" {
				t.Errorf("a usage error printed to stdout: %s", r.Stdout)
			}
		})
	}
}

// The help text is the only documentation most people running the binary
// see: each build lists the variables it reads and not the other build's.
func TestUsageListsTheVariablesThisBuildReads(t *testing.T) {
	for _, v := range []string{"CONCORD_WEB_ADDR", "CONCORD_NO_OPEN", "CONCORD_API_TOKEN"} {
		if flavour == "desktop" && strings.Contains(usage, v) {
			t.Errorf("desktop help lists the web build's %s", v)
		}
		if flavour == "web" && !strings.Contains(usage, v) {
			t.Errorf("web help lacks %s", v)
		}
	}
	for _, v := range []string{"CONCORD_HOME", "CONCORD_BOOTSTRAP", "CONCORD_DISABLE_MDNS", "CONCORD_GUEST_BASE"} {
		if !strings.Contains(usage, v) {
			t.Errorf("help lacks %s", v)
		}
	}
	if !strings.HasPrefix(cli.VersionLine(flavour), "concord ") {
		t.Errorf("version line %q", cli.VersionLine(flavour))
	}
}
