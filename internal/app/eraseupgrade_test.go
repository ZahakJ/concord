package app

import (
	"context"
	"errors"
	"os"
	"testing"
)

// A linked device whose erase removed its device marker and stopped there
// (what an erase of earlier builds left) still holds the stored revocation of
// its own device key: the next start finishes the erase.
func TestARevokedDeviceWithItsMarkerGoneIsErasedAtStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, _, _ := unlinkedDeviceWithData(t, ctx, dir)
	_ = s.Close()
	if err := os.Remove(deviceMarkerPath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true}); !errors.Is(err, ErrDeviceUnlinked) {
		t.Fatalf("the start opened the revoked device: %v", err)
	}
	onlyLockLeft(t, dir, "after the start")
}

// A never-linked install holding a revocation of a key that is not its own
// device key opens as before.
func TestARevocationOfAnotherDeviceDoesNotEraseAnInstallWithNoMarker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s, _, _ := unlinkedDeviceWithData(t, ctx, dir)
	other := make([]byte, 32)
	other[0] = 1
	rev := s.id.RevokeDevice(other, 1)
	s.saveRevocations(append(s.revocations()[:0:0], rev))
	_ = s.Close()
	if err := os.Remove(deviceMarkerPath(dir)); err != nil {
		t.Fatal(err)
	}
	s2, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = s2.Close()
	if !HasIdentity(dir) {
		t.Fatal("the install was erased")
	}
}

// A recovery-phrase restore onto what an unlink's erase left brings none of
// the revoked device's data back: an erase with its record is finished
// first, and the leftovers of one without a record are erased by the
// restore it was confirmed for.
func TestARestoreDoesNotBringBackARevokedDevicesData(t *testing.T) {
	for _, recorded := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		dir := t.TempDir()
		s, ch, _ := unlinkedDeviceWithData(t, ctx, dir)
		phrase, err := s.Mnemonic()
		if err != nil {
			t.Fatal(err)
		}
		fp := s.Fingerprint()
		if recorded {
			stopEraseAt(t, 3) // record, keystore, database
			if err := s.wipeSelf(); !errors.Is(err, errStopHere) {
				t.Fatal(err)
			}
			eraseStep = func(string) error { return nil }
		} else {
			_ = s.Close()
			_ = os.Remove(keystorePathIn(dir))
		}
		err = RestoreOverExisting(dir, phrase, "restored-pass", "")
		if err != nil {
			err = RestoreOverExisting(dir, phrase, "restored-pass", fp)
		}
		if err != nil {
			t.Fatalf("recorded=%v: restore: %v", recorded, err)
		}
		s2, err := Start(ctx, Config{DataDir: dir, Passphrase: "restored-pass", DisableMDNS: true})
		if err != nil {
			t.Fatalf("recorded=%v: start after restore: %v", recorded, err)
		}
		msgs, _ := s2.Messages(ch, 10)
		_ = s2.Close()
		if len(msgs) != 0 || erasePendingIn(dir) {
			t.Fatalf("recorded=%v: the revoked device's messages came back: %d", recorded, len(msgs))
		}
		if _, err := os.Lstat(deviceMarkerPath(dir)); err == nil {
			t.Fatalf("recorded=%v: the revoked device's marker came back", recorded)
		}
		cancel()
	}
}
