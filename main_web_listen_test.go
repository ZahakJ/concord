//go:build !wails

package main

import (
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSocketPath(t *testing.T) {
	if p, ok := socketPath("unix:/run/x/concord.sock"); !ok || p != "/run/x/concord.sock" {
		t.Fatalf("got %q %v", p, ok)
	}
	if _, ok := socketPath("127.0.0.1:8787"); ok {
		t.Fatal("tcp address taken for a socket")
	}
}

func TestUnixListenServesHTTPAndIs0660(t *testing.T) {
	old := syscall.Umask(0o077) // what a hardened unit runs with
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "c.sock")
	ln, err := listen("unix:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode %v", fi.Mode())
	}
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") }))
	c := http.Client{Transport: &http.Transport{Dial: func(_, _ string) (net.Conn, error) { return net.Dial("unix", path) }}}
	resp, err := c.Get("http://concord/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" {
		t.Fatalf("body %q", b)
	}
}

func TestStaleSocketIsReplacedLiveOneIsNot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.sock")

	// Stale: a socket file nobody listens on (a crashed process's leftover).
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("setup: stale socket missing")
	}
	ln, err := listenOnce("unix:" + path)
	if err != nil {
		t.Fatalf("stale socket not cleared: %v", err)
	}

	// Live: a second instance must not steal the first one's socket.
	if _, err := listenOnce("unix:" + path); err == nil || !strings.Contains(err.Error(), "already listening") {
		t.Fatalf("second bind over a live socket: %v", err)
	}
	ln.Close()

	// Not a socket: never removed.
	reg := filepath.Join(dir, "file")
	os.WriteFile(reg, []byte("keep"), 0o600)
	if _, err := listenOnce("unix:" + reg); err == nil {
		t.Fatal("bound over a regular file")
	}
	if b, _ := os.ReadFile(reg); string(b) != "keep" {
		t.Fatal("regular file was touched")
	}
}

func TestTokenFileModeAndNoWriteThroughSymlink(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	os.WriteFile(victim, []byte("untouched"), 0o600)
	path := filepath.Join(dir, "api.token")
	os.Symlink(victim, path)

	if err := writeTokenFile(path, "abc123"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "untouched" {
		t.Fatal("token written through a symlink")
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v", fi.Mode())
	}
	if b, _ := os.ReadFile(path); string(b) != "abc123\n" {
		t.Fatalf("token file %q", b)
	}
}

func TestPassphraseFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pass")
	os.WriteFile(p, []byte(" spaced pass \n"), 0o600)
	if s, err := readPassphraseFile(p); err != nil || s != " spaced pass " {
		t.Fatalf("got %q %v", s, err)
	}
	os.WriteFile(p, []byte("x"), 0o600)
	os.Chmod(p, 0o640)
	if _, err := readPassphraseFile(p); err == nil {
		t.Fatal("group-readable passphrase accepted")
	}
	os.Chmod(p, 0o400) // systemd credentials arrive 0400
	if s, err := readPassphraseFile(p); err != nil || s != "x" {
		t.Fatalf("0400 file: %q %v", s, err)
	}
	e := filepath.Join(dir, "empty")
	os.WriteFile(e, []byte("\n"), 0o600)
	if _, err := readPassphraseFile(e); err == nil {
		t.Fatal("empty passphrase accepted")
	}
}
