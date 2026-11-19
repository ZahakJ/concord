//go:build !wails

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What the binary prints at startup goes to the service journal under
// systemd. It must never carry the bearer token, and off a terminal it carries
// no code either: nobody is reading it in the two minutes a code lives.
func TestStartupLineCarriesNoSecretOffATerminal(t *testing.T) {
	minted := 0
	mint := func() string { minted++; return "c0de" }
	line := startupLine("127.0.0.1:8787", "", false, mint)
	if strings.Contains(line, "?t=") || strings.Contains(line, "c0de") || minted != 0 {
		t.Fatalf("journal line %q (minted %d)", line, minted)
	}
	if !strings.Contains(line, "http://127.0.0.1:8787") {
		t.Fatalf("journal line %q does not say where it is", line)
	}
	if line := startupLine("127.0.0.1:8787", "", true, mint); !strings.Contains(line, "/?c=c0de") || strings.Contains(line, "?t=") {
		t.Fatalf("terminal line %q", line)
	}
}

// The startup line says how to get another code, by naming the launch socket
// and the command that asks it, and names no script that does not exist.
func TestStartupLineNamesTheLaunchSocket(t *testing.T) {
	mint := func() string { return "c0de" }
	const sock = "/run/user/1000/concord/launch-127.0.0.1_8787.sock"
	for _, terminal := range []bool{false, true} {
		line := startupLine("127.0.0.1:8787", sock, terminal, mint)
		if !strings.Contains(line, "curl --unix-socket "+sock+" http://launch/code") || strings.Contains(line, ".sh") {
			t.Errorf("terminal=%v: %q", terminal, line)
		}
		if line := startupLine("127.0.0.1:8787", "", terminal, mint); strings.Contains(line, "curl") || strings.Contains(line, ".sh") {
			t.Errorf("terminal=%v, no socket: %q", terminal, line)
		}
	}
}

// The launch socket hands a fresh code to whoever asks on it (a launcher, or
// `curl --unix-socket <sock> http://launch/code`).
// It sits in a directory only this user can enter, the socket itself is
// owner-only, and it goes when the process does.
func TestLaunchSocketMintsCodesForTheOwnerOnly(t *testing.T) {
	run := t.TempDir()
	path, stop, err := serveLaunchSocket(run, "127.0.0.1:8787", func() string { return "fresh" })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if want := filepath.Join(run, "concord", "launch-127.0.0.1_8787.sock"); path != want {
		t.Fatalf("socket at %s, want %s", path, want)
	}
	if fi, _ := os.Stat(filepath.Dir(path)); fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", fi.Mode().Perm())
	}
	c := http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}}
	resp, err := c.Get("http://launch/code")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(body)) != "fresh" {
		t.Fatalf("socket said %q", body)
	}
	// Gone the moment stop returns, not some time after: the process exits
	// right behind it, and a goroutine left to clean up never runs.
	stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket still there after stop: %v", err)
	}
}
