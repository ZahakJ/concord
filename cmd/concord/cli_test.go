package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/cli/clitest"
)

func TestMain(m *testing.M) {
	clitest.Main("concord", main)
	os.Exit(m.Run())
}

// --version was an unknown flag here, and a stray word fell through to the
// default --status mode, which creates the profile directory and asks for a
// passphrase. Each case must answer and leave the throwaway home empty.
func TestCommandLineAnswersWithoutTouchingTheProfile(t *testing.T) {
	wantVersion := "concord dev (headless, " + runtime.GOOS + "/" + runtime.GOARCH + ")\n"
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
		{args: []string{"--no-such-flag"}, code: 2, stderrHas: "flag provided but not defined: -no-such-flag"},
		{args: []string{"version"}, code: 2, stderrHas: `unexpected argument "version"`},
		{args: []string{"--serve", "extra"}, code: 2, stderrHas: `unexpected argument "extra"`},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			r := clitest.NewSandbox(t).Run(t, 20*time.Second, c.args...)
			if r.Code != c.code {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.Code, c.code, r.Stdout, r.Stderr)
			}
			if r.Stdout != c.stdout {
				t.Errorf("stdout %q, want %q", r.Stdout, c.stdout)
			}
			if c.stderrHas != "" && !(strings.Contains(r.Stderr, c.stderrHas) && strings.Contains(r.Stderr, "Usage: concord")) {
				t.Errorf("stderr lacks %q and the usage:\n%s", c.stderrHas, r.Stderr)
			}
			if c.code == 0 && r.Stderr != "" {
				t.Errorf("unexpected stderr: %s", r.Stderr)
			}
		})
	}
}

// Every mode flag the binary defines is described in its help.
func TestUsageDescribesEveryFlag(t *testing.T) {
	for _, f := range []string{"--status", "--serve", "--bot", "--version", "--help", "CONCORD_PASSPHRASE", "CONCORD_HOME"} {
		if !strings.Contains(usage, f) {
			t.Errorf("help lacks %s", f)
		}
	}
}
