// Package clitest runs a Concord entry point's main() in a child process with
// a throwaway home, so a test can prove what a command line does to the
// machine: its exit status, its output, and that it wrote nothing.
//
// The child is the package's own test binary, re-entered through TestMain
// (see Main), so no separate build is needed and the check runs under
// `go test -short` with everything else.
package clitest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const argsEnv = "CONCORD_CLITEST_ARGS"

// Main is called first thing in a package's TestMain. In the child process it
// replaces os.Args with the command line under test and runs main; a main
// that returns instead of exiting is reported as status 0, like the real
// binary. In the test process itself it returns and the tests run normally.
func Main(name string, main func()) {
	raw, ok := os.LookupEnv(argsEnv)
	if !ok {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		os.Stderr.WriteString("clitest: bad " + argsEnv + ": " + err.Error() + "\n")
		os.Exit(99)
	}
	os.Args = append([]string{name}, args...)
	main()
	os.Exit(0)
}

// Result is what one run of the entry point did.
type Result struct {
	Code           int
	Stdout, Stderr string
}

// Sandbox is a throwaway home for the child: every directory Concord could
// write to — the profile, the XDG data dir its desktop build installs into,
// the Windows app-data roots — points inside Root, which starts empty.
type Sandbox struct {
	Root  string
	Extra []string // more KEY=VALUE pairs for the child
}

// NewSandbox makes an empty Root under t.TempDir().
func NewSandbox(t *testing.T) *Sandbox {
	t.Helper()
	return &Sandbox{Root: t.TempDir()}
}

func (s *Sandbox) env() []string {
	in := func(p string) string { return filepath.Join(s.Root, p) }
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + in("home"),
		"USERPROFILE=" + in("home"),
		"XDG_CONFIG_HOME=" + in("config"),
		"XDG_DATA_HOME=" + in("data"),
		"XDG_CACHE_HOME=" + in("cache"),
		"XDG_STATE_HOME=" + in("state"),
		"APPDATA=" + in("appdata"),
		"LOCALAPPDATA=" + in("localappdata"),
		"CONCORD_HOME=" + in("concord"),
		"TMPDIR=" + in("tmp"),
		// A child that wrongly started the app must not reach a browser, a
		// display or the session bus.
		"CONCORD_NO_OPEN=1",
	}
	return append(env, s.Extra...)
}

// Run executes the test binary as the entry point with args and fails the
// test if the child outlives timeout or left anything under Root.
func (s *Sandbox) Run(t *testing.T, timeout time.Duration, args ...string) Result {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(args)
	return s.run(t, timeout, args, exe, []string{"-test.run=^$"}, argsEnv+"="+string(raw))
}

// RunBinary is Run for a separately built binary at bin.
func (s *Sandbox) RunBinary(t *testing.T, timeout time.Duration, bin string, args ...string) Result {
	t.Helper()
	return s.run(t, timeout, args, bin, args)
}

func (s *Sandbox) run(t *testing.T, timeout time.Duration, args []string, exe string, argv []string, env ...string) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, argv...)
	cmd.Env = append(s.env(), env...)
	cmd.Dir = s.Root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		t.Fatalf("%q was still running after %s — it started the program instead of answering\nstdout: %s\nstderr: %s",
			args, timeout, res.Stdout, res.Stderr)
	case errors.As(err, &exitErr):
		res.Code = exitErr.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	s.assertEmpty(t, args)
	return res
}

func (s *Sandbox) assertEmpty(t *testing.T, args []string) {
	t.Helper()
	_ = filepath.WalkDir(s.Root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && p != s.Root {
			t.Errorf("%q created %s", args, p)
		}
		return nil
	})
}
