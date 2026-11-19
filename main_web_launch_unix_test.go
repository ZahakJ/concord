//go:build !wails && unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The launch socket goes only into a runtime directory nobody else can write
// to, and only into a <run>/concord that is a real directory this user owns.
// Anything else is refused before the directory is touched: a symlink planted
// at <run>/concord would otherwise have its target chmodded and the socket
// placed in it.
func TestLaunchSocketRefusesAnUnsafeDirectory(t *testing.T) {
	mint := func() string { return "fresh" }
	refused := func(t *testing.T, run string) {
		t.Helper()
		path, stop, err := serveLaunchSocket(run, "127.0.0.1:8787", mint)
		if err == nil {
			stop()
			t.Fatalf("served at %s", path)
		}
		// Refused by the checks, not by a bind that failed for another reason.
		if strings.Contains(err.Error(), "bind") || strings.Contains(err.Error(), "listen") {
			t.Fatalf("refused for the wrong reason: %v", err)
		}
	}
	// t.TempDir's names are long, and a socket path must fit in 108 bytes.
	shortDir := func(t *testing.T) string {
		d, err := os.MkdirTemp("", "lr")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(d) })
		return d
	}

	t.Run("a safe directory is served", func(t *testing.T) {
		_, stop, err := serveLaunchSocket(shortDir(t), "127.0.0.1:8787", mint)
		if err != nil {
			t.Fatal(err)
		}
		stop()
	})

	t.Run("world-writable runtime dir", func(t *testing.T) {
		run := shortDir(t)
		if err := os.Chmod(run, 0o777); err != nil {
			t.Fatal(err)
		}
		refused(t, run)
		if _, err := os.Lstat(filepath.Join(run, "concord")); !os.IsNotExist(err) {
			t.Fatalf("made %s/concord anyway: %v", run, err)
		}
	})
	t.Run("group-writable runtime dir", func(t *testing.T) {
		run := shortDir(t)
		if err := os.Chmod(run, 0o770); err != nil {
			t.Fatal(err)
		}
		refused(t, run)
	})
	t.Run("symlink", func(t *testing.T) {
		run, target := shortDir(t), shortDir(t)
		if err := os.Chmod(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(run, "concord")); err != nil {
			t.Fatal(err)
		}
		refused(t, run)
		if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o755 {
			t.Fatalf("the symlink's target was chmodded to %v", fi.Mode().Perm())
		}
		if m, _ := filepath.Glob(filepath.Join(target, "*")); len(m) != 0 {
			t.Fatalf("something was put in the symlink's target: %v", m)
		}
	})
	t.Run("regular file", func(t *testing.T) {
		run := shortDir(t)
		if err := os.WriteFile(filepath.Join(run, "concord"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		refused(t, run)
	})
	t.Run("another user's directory", func(t *testing.T) {
		run := shortDir(t)
		if err := os.Mkdir(filepath.Join(run, "concord"), 0o755); err != nil {
			t.Fatal(err)
		}
		me := launchOwnerUID
		launchOwnerUID = func() int { return os.Getuid() + 1 }
		defer func() { launchOwnerUID = me }()
		refused(t, run)
		if fi, _ := os.Stat(filepath.Join(run, "concord")); fi.Mode().Perm() != 0o755 {
			t.Fatalf("another user's directory was chmodded to %v", fi.Mode().Perm())
		}
	})
}
