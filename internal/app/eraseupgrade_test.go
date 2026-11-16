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
