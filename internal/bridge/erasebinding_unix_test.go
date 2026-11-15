//go:build !windows

package bridge

import (
	"os"
	"path/filepath"
	"strings"
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

// otherKeystore is Bob's keystore file and account key, as a restore or link
// in another process writes it.
func otherKeystore(t *testing.T) ([]byte, []byte) {
	t.Helper()
	bob, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(t.TempDir(), "keystore.json")
	if err := identity.CreateKeystore(scratch, "bob-pass-1", bob); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(scratch)
	if err != nil {
		t.Fatal(err)
	}
	return raw, bob.PublicKey()
}

// holdKeystoreLock takes the keystore's advisory lock as another process
// writing the keystore would. The returned release lets it go (once).
func holdKeystoreLock(t *testing.T, ks string) (release func()) {
	t.Helper()
	lf, err := os.OpenFile(ks+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lf.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	done := false
	release = func() {
		if !done {
			done = true
			_ = unix.Flock(int(lf.Fd()), unix.LOCK_UN)
			_ = lf.Close()
		}
	}
	t.Cleanup(release)
	return release
}

// The Settings wipe compares the keystore with the one its dialog was opened
// over once it holds the keystore lock, not before: a keystore another
// process renames in while the wipe waits for the lock is left.
func TestAWipeLooksAgainOnceItHoldsTheKeystoreLock(t *testing.T) {
	b, view, ks := adaWithAWipeOpen(t)
	bobBytes, bobPub := otherKeystore(t)
	release := holdKeystoreLock(t, ks)
	done := make(chan error, 1)
	go func() { done <- b.ConfirmWipe(view.Ticket, "Ada") }()
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("setup: the wipe did not wait for the keystore lock: %v", err)
	default:
	}
	tmp := ks + ".other"
	if err := os.WriteFile(tmp, bobBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, ks); err != nil {
		t.Fatal(err)
	}
	release()
	err := <-done
	if code, _ := codeOf(err); code != CodeConfirmStale {
		t.Fatalf("wipe after the keystore changed under it: %v, want CodeConfirmStale", err)
	}
	mustStillHold(t, ks, bobPub, "after a wipe that waited for the lock")
}

// A wipe that cannot take the keystore lock deletes nothing and records no
// erase: its check means nothing without the lock. The user is told another
// window or process has the keystore, is still signed in, and a wipe opened
// again once the lock is free goes through.
func TestAWipeThatCannotTakeTheKeystoreLockDeletesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the keystore lock (10 s)")
	}
	b, view, ks := adaWithAWipeOpen(t)
	ada, err := identity.ReadAccountPub(ks)
	if err != nil {
		t.Fatal(err)
	}
	release := holdKeystoreLock(t, ks)
	err = b.ConfirmWipe(view.Ticket, "Ada")
	if code, _ := codeOf(err); code != CodeKeystoreBusy {
		t.Fatalf("wipe with the keystore lock held: %v, want CodeKeystoreBusy", err)
	}
	for _, says := range []string{"Another Concord window or process", "Nothing was deleted", "Try again"} {
		if !strings.Contains(err.Error(), says) {
			t.Errorf("the refusal %q does not say %q", err, says)
		}
	}
	mustStillHold(t, ks, ada, "after a wipe that could not take the lock")
	if _, err := os.Lstat(ks + ".erased"); !os.IsNotExist(err) {
		t.Fatalf("an erase was recorded without the lock: %v", err)
	}
	if _, err := b.service(); err != nil {
		t.Fatalf("a wipe that deleted nothing signed the user out: %v", err)
	}
	release()

	view, err = b.BeginWipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ConfirmWipe(view.Ticket, "Ada"); err != nil {
		t.Fatalf("the wipe once the lock was free: %v", err)
	}
	if _, err := os.Stat(ks); !os.IsNotExist(err) {
		t.Fatalf("the keystore is still here: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(ks), "concord.db")); !os.IsNotExist(err) {
		t.Fatalf("the database is still here: %v", err)
	}
}

// Start over likewise: with the keystore lock held throughout, nothing is
// deleted, and the same confirmation goes through once the lock is free.
func TestStartOverThatCannotTakeTheKeystoreLockDeletesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the keystore lock (10 s)")
	}
	b, _ := newLockedDevice(t, "ada-pass-1", "Ada")
	ks, err := appsvc.KeystorePath()
	if err != nil {
		t.Fatal(err)
	}
	ada, err := identity.ReadAccountPub(ks)
	if err != nil {
		t.Fatal(err)
	}
	view, err := b.BeginStartOver()
	if err != nil {
		t.Fatal(err)
	}
	release := holdKeystoreLock(t, ks)
	err = b.ResetIdentity(view.Ticket, StartOverWord)
	if code, _ := codeOf(err); code != CodeKeystoreBusy {
		t.Fatalf("start over with the keystore lock held: %v, want CodeKeystoreBusy", err)
	}
	for _, says := range []string{"Another Concord window or process", "Nothing was deleted", "Try again"} {
		if !strings.Contains(err.Error(), says) {
			t.Errorf("the refusal %q does not say %q", err, says)
		}
	}
	mustStillHold(t, ks, ada, "after a start over that could not take the lock")
	if _, err := os.Lstat(ks + ".erased"); !os.IsNotExist(err) {
		t.Fatalf("an erase was recorded without the lock: %v", err)
	}
	release()

	if err := b.ResetIdentity(view.Ticket, StartOverWord); err != nil {
		t.Fatalf("start over once the lock was free: %v", err)
	}
	if appsvc.HasIdentity(filepath.Dir(ks)) {
		t.Fatal("the keystore is still here")
	}
}
