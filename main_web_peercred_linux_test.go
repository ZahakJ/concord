//go:build !wails && linux

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// On Linux the launch socket checks who is calling (SO_PEERCRED) and answers
// only its own user, whatever the directory and file modes say. Here the
// "own user" is made someone else, so this process is the stranger.
func TestLaunchSocketAnswersOnlyItsOwnUser(t *testing.T) {
	get := func(path string) (string, error) {
		c := http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		}}}
		resp, err := c.Get("http://launch/code")
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return strings.TrimSpace(string(b)), err
	}

	me := launchPeerUID
	launchPeerUID = func() int { return os.Getuid() + 1 }
	path, stop, err := serveLaunchSocket(t.TempDir(), "127.0.0.1:8787", func() string { return "fresh" })
	launchPeerUID = me
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if got, err := get(path); err == nil || got == "fresh" {
		t.Fatalf("a caller of another uid got %q (%v)", got, err)
	}

	path2, stop2, err := serveLaunchSocket(t.TempDir(), "127.0.0.1:8788", func() string { return "fresh" })
	if err != nil {
		t.Fatal(err)
	}
	defer stop2()
	if got, err := get(path2); err != nil || got != "fresh" {
		t.Fatalf("its own user got %q (%v)", got, err)
	}
}
