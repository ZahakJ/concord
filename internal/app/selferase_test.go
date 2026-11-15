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
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) || !errors.Is(err, ErrDeviceUnlinkedReplaced) {
		t.Fatalf("revoke wipe over another account's keystore: %v, want ErrDeviceUnlinkedNotErased and ErrDeviceUnlinkedReplaced", err)
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
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) || !errors.Is(err, ErrDeviceUnlinkedReplaced) {
		t.Fatalf("revoke wipe of a device linked again: %v, want ErrDeviceUnlinkedNotErased and ErrDeviceUnlinkedReplaced", err)
	}
	mustHoldAccount(t, dir, s.id.PublicKey(), "after a revoke wipe for the old device key")
	if _, err := os.Stat(deviceMarkerPath(dir)); err != nil {
		t.Fatalf("the new device marker went: %v", err)
	}
}

// A keystore that cannot be parsed names no account, so it cannot be another
// account's: the device marker decides, and a revoked device is erased.
func TestARevokeWipeErasesADamagedKeystore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := revokedLinkedDevice(t, ctx, dir)
	if err := os.WriteFile(keystorePathIn(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.wipeSelf(); err != nil {
		t.Fatalf("revoke wipe over a damaged keystore: %v", err)
	}
	if hasAccountData(dir) {
		t.Fatal("a revoked device kept its data because its keystore could not be parsed")
	}
	if _, err := os.Lstat(keystorePathIn(dir)); !os.IsNotExist(err) {
		t.Fatalf("the damaged keystore is still here: %v", err)
	}
}

// A device marker that cannot be read at the moment (a scanner holding it,
// its permissions changed) says nothing either way. The erase is not refused
// as "another account": it is tried again, and goes through once the marker
// can be read.
func TestARevokeWipeWithAnUnreadableMarkerTriesAgain(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	fastSelfEraseRetry(t)
	s := revokedLinkedDevice(t, ctx, dir)
	told := make(chan error, 4)
	s.OnUnlinked(func(err error) { told <- err })
	if err := os.Chmod(deviceMarkerPath(dir), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(deviceMarkerPath(dir), 0o600) })

	err := s.wipeSelf()
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) || errors.Is(err, ErrDeviceUnlinkedReplaced) {
		t.Fatalf("revoke wipe with an unreadable marker: %v, want not erased and not refused as another account", err)
	}
	if <-told == nil {
		t.Fatal("the shell was told the device was erased")
	}
	mustHoldAccount(t, dir, s.id.PublicKey(), "while the marker cannot be read")

	if err := os.Chmod(deviceMarkerPath(dir), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-told:
		if err != nil {
			t.Fatalf("the retry once the marker could be read: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the erase was not tried again once the marker could be read")
	}
	if HasIdentity(dir) || hasAccountData(dir) {
		t.Fatal("the retry reported the device erased, and it is not")
	}
}

// At the next start, a revocation of this device with a marker that cannot be
// read is not "not linked": Concord cannot tell whether the revocation is for
// this install, and does not open the account.
func TestARevokedDeviceWithAnUnreadableMarkerDoesNotOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := revokedLinkedDevice(t, ctx, dir)
	_ = s.Close()
	if err := os.Chmod(deviceMarkerPath(dir), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(deviceMarkerPath(dir), 0o600) })

	again, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err == nil {
		_ = again.Close()
		t.Fatal("a revoked device whose marker cannot be read opened its account")
	}
	if !errors.Is(err, ErrDeviceUnlinkedNotErased) {
		t.Fatalf("start of a revoked device with an unreadable marker: %v, want ErrDeviceUnlinkedNotErased", err)
	}
	if !HasIdentity(dir) {
		t.Fatal("the keystore was erased while the marker could not be read")
	}
}

// A self-erase is checked: it refuses to erase without one, rather than
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
