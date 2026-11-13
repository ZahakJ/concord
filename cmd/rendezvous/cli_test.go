package main

import (
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/cli/clitest"
)

func TestMain(m *testing.M) {
	clitest.Main("rendezvous", main)
	os.Exit(m.Run())
}

// The node took no flags and started on any command line: `rendezvous
// --version` brought up a public DHT server and relay on port 4001. The test
// points PORT at a port it holds, so a node that starts anyway cannot bind
// it and fails instead of serving.
func TestCommandLineAnswersWithoutStartingTheNode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	wantVersion := "concord dev (rendezvous node, " + runtime.GOOS + "/" + runtime.GOARCH + ")\n"
	cases := []struct {
		args      []string
		code      int
		stdout    string
		stderrHas string
	}{
		{args: []string{"--version"}, code: 0, stdout: wantVersion},
		{args: []string{"-version"}, code: 0, stdout: wantVersion},
		{args: []string{"--help"}, code: 0, stdout: usage},
		{args: []string{"-h"}, code: 0, stdout: usage},
		{args: []string{"--port", "4001"}, code: 2, stderrHas: "flag provided but not defined: -port"},
		{args: []string{"serve"}, code: 2, stderrHas: `unexpected argument "serve"`},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			sb := clitest.NewSandbox(t)
			sb.Extra = []string{"PORT=" + port, "CONCORD_GUEST_PORT=" + port}
			r := sb.Run(t, 20*time.Second, c.args...)
			if r.Code != c.code {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.Code, c.code, r.Stdout, r.Stderr)
			}
			if r.Stdout != c.stdout {
				t.Errorf("stdout %q, want %q", r.Stdout, c.stdout)
			}
			if c.stderrHas != "" && !(strings.Contains(r.Stderr, c.stderrHas) && strings.Contains(r.Stderr, "Usage: rendezvous")) {
				t.Errorf("stderr lacks %q and the usage:\n%s", c.stderrHas, r.Stderr)
			}
			if c.code == 0 && r.Stderr != "" {
				t.Errorf("unexpected stderr: %s", r.Stderr)
			}
		})
	}
}
