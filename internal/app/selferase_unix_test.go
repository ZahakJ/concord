//go:build !windows

package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ZahakJ/concord/internal/identity"
)

// holdKeystoreLock takes the keystore's advisory lock as another process
// writing it would. The returned release lets it go (once).
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

// The self-erase checks what is on the device once it holds the keystore
// lock, not before: a restore of Bob that another process finishes while the
// erase waits for the lock is not erased.
func TestARevokeWipeLooksAgainOnceItHoldsTheKeystoreLock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := revokedLinkedDevice(t, ctx, dir)
	ks := keystorePathIn(dir)
	bobBytes, bobPub := otherAccountKeystore(t)

	release := holdKeystoreLock(t, ks)
	done := make(chan error, 1)
	go func() { done <- s.wipeSelf() }()
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("setup: the revoke wipe did not wait for the keystore lock: %v", err)
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
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) {
		t.Fatalf("revoke wipe after Bob's keystore landed: %v, want ErrDeviceUnlinkedNotErased", err)
	}
	mustHoldAccount(t, dir, bobPub, "after a revoke wipe that waited for the lock")
}

// When the keystore lock cannot be had at all, the self-erase erases nothing
// and records nothing: without the lock it cannot tell that what it would
// erase is still this device. The device stays closed, says it was not
// erased, and the next start, with the lock free, finishes the erase.
func TestARevokeWipeThatCannotTakeTheKeystoreLockErasesNothingUntilTheNextStart(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the keystore lock (10 s)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := revokedLinkedDevice(t, ctx, dir)
	ks := keystorePathIn(dir)

	release := holdKeystoreLock(t, ks)
	err := s.wipeSelf()
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) || !errors.Is(err, identity.ErrKeystoreBusy) {
		t.Fatalf("revoke wipe with the keystore lock held: %v, want ErrDeviceUnlinkedNotErased and ErrKeystoreBusy", err)
	}
	mustHoldAccount(t, dir, s.id.PublicKey(), "after a revoke wipe that could not take the lock")
	if _, err := os.Lstat(ks + ".erased"); !os.IsNotExist(err) {
		t.Fatalf("an erase was recorded without the lock: %v", err)
	}
	if _, err := os.Stat(deviceMarkerPath(dir)); err != nil {
		t.Fatalf("the device marker went: %v", err)
	}
	release()

	_, err = Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if !errors.Is(err, ErrDeviceUnlinked) {
		t.Fatalf("the next start did not finish the revoke wipe: %v", err)
	}
	if hasAccountData(dir) || HasIdentity(dir) {
		t.Fatal("the revoke wipe at the next start left data behind")
	}
}
