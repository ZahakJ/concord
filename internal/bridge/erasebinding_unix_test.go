//go:build !windows

package bridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

// Start over is confirmed for the device as the screen showed it. Taking the
// keystore lock can wait on another process that is writing: a restore or a
// link confirmed in another window. What that process leaves behind was never
// shown to this confirmation, so the erase must look again once it holds the
// lock, and leave it.
//
// The other process is an advisory lock on the keystore's lock file (what a
// keystore write holds) and a rename of Bob's keystore into place (what the
// write ends with).
func TestStartOverLooksAgainOnceItHoldsTheKeystoreLock(t *testing.T) {
	b, _ := newLockedDevice(t, "ada-pass-1", "Ada")
	dir, err := appsvc.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	ks := filepath.Join(dir, "keystore.json")

	bob, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(t.TempDir(), "keystore.json")
	if err := identity.CreateKeystore(scratch, "bob-pass-1", bob); err != nil {
		t.Fatal(err)
	}
	bobBytes, err := os.ReadFile(scratch)
	if err != nil {
		t.Fatal(err)
	}

	view, err := b.BeginStartOver() // the screen shows Ada's device
	if err != nil {
		t.Fatal(err)
	}

	lf, err := os.OpenFile(ks+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	if err := unix.Flock(int(lf.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- b.ResetIdentity(view.Ticket, StartOverWord) }()
	time.Sleep(500 * time.Millisecond) // past its first look, waiting for the lock
	select {
	case err := <-done:
		t.Fatalf("setup: start over did not wait for the keystore lock: %v", err)
	default:
	}

	// The other process finishes: Bob's keystore is in place.
	tmp := ks + ".other"
	if err := os.WriteFile(tmp, bobBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, ks); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lf.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}

	err = <-done
	if code, _ := codeOf(err); code != CodeConfirmStale {
		t.Fatalf("start over after the device changed under it: %v, want it refused as stale", err)
	}
	pub, rerr := identity.ReadAccountPub(ks)
	if rerr != nil || string(pub) != string(bob.PublicKey()) {
		t.Fatalf("an ERASE typed for Ada's device deleted the keystore another process had just written for Bob (%v)", rerr)
	}
	if _, err := os.Stat(filepath.Join(dir, "concord.db")); err != nil {
		t.Fatalf("and the data went with it: %v", err)
	}
}
