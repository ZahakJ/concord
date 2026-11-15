package app

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
)

// revokedLinkedDevice is a running linked install whose device the account
// has revoked, with the revocation stored: what a self-erase acts on.
func revokedLinkedDevice(t *testing.T, ctx context.Context, dir string) *Service {
	t.Helper()
	s := startServiceInDir(t, ctx, dir)
	dev := s.id.DevicePublicKey()
	if err := saveDeviceMarker(dir, s.id.IssueDeviceCertFor(dev, "phone", time.Now().Add(-time.Hour).Unix())); err != nil {
		t.Fatal(err)
	}
	s.saveRevocations([]*identity.DeviceRevocation{s.id.RevokeDevice(dev, time.Now().Unix())})
	return s
}

// otherAccountKeystore is the keystore file of a new account, Bob's, and his
// account key: what a restore or link in another window writes.
func otherAccountKeystore(t *testing.T) ([]byte, ed25519.PublicKey) {
	t.Helper()
	bob, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(t.TempDir(), "keystore.json")
	if err := identity.CreateKeystore(scratch, "bob-pass", bob); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(scratch)
	if err != nil {
		t.Fatal(err)
	}
	return raw, bob.PublicKey()
}

func mustHoldAccount(t *testing.T, dir string, account ed25519.PublicKey, what string) {
	t.Helper()
	pub, err := identity.ReadAccountPub(keystorePathIn(dir))
	if err != nil || !ed25519Equal(pub, account) {
		t.Fatalf("%s: the keystore no longer holds the account it held (%v)", what, err)
	}
	if _, err := os.Stat(dbPathIn(dir)); err != nil {
		t.Fatalf("%s: the database is gone: %v", what, err)
	}
}

// A revocation names one account's device. If, by the time the self-erase
// runs, another window has put another account's keystore on this device (a
// restore of Bob confirmed there), the erase is not for it: nothing is
// erased, and the device says it was not.
func TestARevokeWipeLeavesAnotherAccountsKeystore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := revokedLinkedDevice(t, ctx, dir)
	bobBytes, bobPub := otherAccountKeystore(t)
	if err := os.WriteFile(keystorePathIn(dir), bobBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	err := s.wipeSelf()
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) {
		t.Fatalf("revoke wipe over another account's keystore: %v, want ErrDeviceUnlinkedNotErased", err)
	}
	mustHoldAccount(t, dir, bobPub, "after a revoke wipe for Ada's device")
}

// The same for a device linked again since: its marker now certifies another
// device key, and the revocation was for the old one.
func TestARevokeWipeLeavesADeviceLinkedAgain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := revokedLinkedDevice(t, ctx, dir)
	newDev := make([]byte, ed25519.PublicKeySize)
	newDev[0] = 1
	if err := saveDeviceMarker(dir, s.id.IssueDeviceCertFor(newDev, "phone", time.Now().Unix())); err != nil {
		t.Fatal(err)
	}

	err := s.wipeSelf()
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) {
		t.Fatalf("revoke wipe of a device linked again: %v, want ErrDeviceUnlinkedNotErased", err)
	}
	mustHoldAccount(t, dir, s.id.PublicKey(), "after a revoke wipe for the old device key")
	if _, err := os.Stat(deviceMarkerPath(dir)); err != nil {
		t.Fatalf("the new device marker went: %v", err)
	}
}

// An erase is checked: without a check it erases nothing, rather than
// erasing whatever is there.
func TestResetIdentityIfRefusesWithoutACheck(t *testing.T) {
	dir := t.TempDir()
	seedDevice(t, dir, "pw", "Bea", false)
	if err := ResetIdentityIf(dir, nil, nil); err == nil {
		t.Fatal("an erase with no check went through")
	}
	if !HasIdentity(dir) || !hasAccountData(dir) {
		t.Fatal("an erase with no check deleted something")
	}
}
