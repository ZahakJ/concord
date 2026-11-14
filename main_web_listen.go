//go:build !wails

// Listener and secret-file plumbing for the browser build, kept out of
// main_web.go so it can be tested without starting the app.
//
// Three opt-in knobs let the web shell run as a locked-down system service
// (its own uid, reachable only by one other local process) instead of a
// desktop app on loopback TCP:
//
//   - CONCORD_WEB_ADDR=unix:/path/concord.sock listens on a unix socket.
//     Loopback TCP is reachable by every process on the machine and can be
//     sniffed or squatted; a socket's directory permissions decide who may
//     connect at all. The socket is made 0660, so access is whatever the
//     directory and the socket's group allow.
//   - CONCORD_API_TOKEN_FILE=/path writes the per-process bearer token to that
//     file (0600) instead of printing it, so it never lands in a log.
//   - CONCORD_PASSPHRASE_FILE=/path unlocks the identity at start from that
//     file. That replaces an external "wait for the port, scrape the token from
//     the journal, POST Login" hook, and it survives an in-app self-update,
//     which re-execs with the same environment.
//
// None of these change the defaults: unset, the shell binds 127.0.0.1:8787 and
// prints its tokenized URL exactly as before.
package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// unixPrefix marks a CONCORD_WEB_ADDR that is a unix socket path.
const unixPrefix = "unix:"

// socketPath returns the socket path when addr names a unix socket.
func socketPath(addr string) (string, bool) {
	if !strings.HasPrefix(addr, unixPrefix) {
		return "", false
	}
	return strings.TrimPrefix(addr, unixPrefix), true
}

// listen binds addr ("host:port" or "unix:/path"), retrying briefly: after a
// self-update restart the previous process may hold the address for a beat.
func listen(addr string) (net.Listener, error) {
	var ln net.Listener
	var err error
	for i := 0; i < 25; i++ {
		ln, err = listenOnce(addr)
		if err == nil {
			return ln, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, err
}

func listenOnce(addr string) (net.Listener, error) {
	path, ok := socketPath(addr)
	if !ok {
		return net.Listen("tcp", addr)
	}
	if path == "" {
		return nil, errors.New("empty unix socket path")
	}
	if err := clearStaleSocket(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Explicit, so a service UMask of 0077 doesn't lock out the one peer the
	// directory and group were set up for.
	if err := os.Chmod(path, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// clearStaleSocket removes a socket file a crashed process left behind, so the
// bind can succeed. It never removes anything that isn't a socket, and never a
// socket something is still answering on (that's a live instance: the caller
// retries, then fails).
func clearStaleSocket(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket; refusing to replace it", path)
	}
	if c, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		c.Close()
		return fmt.Errorf("%s: another instance is already listening", path)
	}
	return os.Remove(path)
}

// writeTokenFile writes the API token to path, readable by its owner only.
// It goes through a temp file and a rename so a reader never sees a
// half-written token, and a pre-existing file (or a symlink an attacker
// planted at that name) is replaced rather than written through.
func writeTokenFile(path, token string) error {
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil { // OpenFile's mode is filtered by umask
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.WriteString(token + "\n"); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// readPassphraseFile reads a passphrase file, trimming one trailing newline
// (what `echo` and most editors add). Other whitespace is part of the
// passphrase. The file must not be readable by group or others: a world- or
// group-readable passphrase is a misconfiguration worth refusing loudly.
func readPassphraseFile(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is readable by group or others (mode %o); make it 0600", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.TrimSuffix(string(b), "\n")
	s = strings.TrimSuffix(s, "\r")
	if s == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return s, nil
}
