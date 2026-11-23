package bridge

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

func slotBridge(t *testing.T) *Bridge {
	t.Helper()
	t.Setenv("CONCORD_HOME", t.TempDir())
	t.Setenv("CONCORD_DISABLE_MDNS", "1")
	t.Setenv("CONCORD_BOOTSTRAP", "")
	b := New(context.Background())
	t.Cleanup(b.Close)
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	return b
}

func newSecret() (slotID, u []byte) {
	slotID, u = make([]byte, identity.DeviceSlotIDSize), make([]byte, identity.DeviceSecretSize)
	_, _ = rand.Read(slotID)
	_, _ = rand.Read(u)
	return
}

// Turning fingerprint unlock on takes the unlock a moment ago as proof of
// the passphrase, once; after that, only the passphrase itself.
func TestEnrolmentNeedsAFreshPassphrase(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, u, ""); err != nil {
		t.Fatalf("right after a passphrase unlock: %v", err)
	}
	slotID2, u2 := newSecret()
	if err := b.EnrolDeviceSecret(slotID2, u2, ""); err == nil {
		t.Fatal("the unlock's proof was used twice")
	}
	if err := b.EnrolDeviceSecret(slotID2, u2, "not it"); err == nil {
		t.Fatal("a wrong passphrase enrolled")
	}
	if err := b.EnrolDeviceSecret(slotID2, u2, "ada-pass-1"); err != nil {
		t.Fatalf("with the passphrase typed: %v", err)
	}
	b.mu.Lock()
	b.enrolUntil = time.Now().Add(-time.Second)
	b.mu.Unlock()
	if err := b.EnrolDeviceSecret(slotID, u, ""); err == nil {
		t.Fatal("an expired proof enrolled")
	}
}

func TestFingerprintUnlocksAndNeverCreates(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, u, ""); err != nil {
		t.Fatal(err)
	}
	if err := b.Logout(); err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), u...)
	bad[3] ^= 1
	if err := b.UseDeviceSecret(slotID, bad); !errors.Is(err, identity.ErrDeviceSlotMismatch) || errors.Is(err, identity.ErrWrongPassphrase) {
		t.Fatalf("wrong U: %v", err)
	}
	if err := b.UseDeviceSecret(slotID, u); err != nil {
		t.Fatalf("fingerprint unlock: %v", err)
	}
	if !b.SessionState().Unlocked {
		t.Fatal("not unlocked")
	}
	// A passphrase unlock still works, and a changed passphrase keeps the slot.
	_ = b.Logout()
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}

	// On a device with no account, the fingerprint opens nothing and makes
	// nothing.
	t.Setenv("CONCORD_HOME", t.TempDir())
	b2 := New(context.Background())
	t.Cleanup(b2.Close)
	if err := b2.UseDeviceSecret(slotID, u); err == nil {
		t.Fatal("a fingerprint unlock with no keystore succeeded")
	}
	if has, _ := b2.HasIdentity(); has {
		t.Fatal("a fingerprint unlock created an account")
	}
}

// Behind the gate the page may ask only whether it is gated, and only the
// in-process fingerprint call opens it. No RPC method reaches U's paths.
func TestTheAppLockGateHoldsEveryRPC(t *testing.T) {
	b := slotBridge(t)
	if b.EngageAppLock() {
		t.Fatal("the gate closed with fingerprint unlock off")
	}
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, u, ""); err != nil {
		t.Fatal(err)
	}
	var events []bool
	b.OnAppLock = func(g bool) { events = append(events, g) }
	if !b.EngageAppLock() {
		t.Fatal("App Lock is on by default once enrolled, but the gate did not close")
	}
	for _, m := range []string{"Guilds", "Identity", "SetAppLock", "Logout", "AppLock", "Mnemonic"} {
		if _, err := b.Dispatch(m, nil); !errors.Is(err, ErrAppLocked) {
			t.Errorf("%s behind the gate: %v", m, err)
		}
	}
	if !b.SessionState().Gated {
		t.Fatal("SessionState does not say gated")
	}
	if _, err := b.Dispatch("SessionState", nil); err != nil {
		t.Fatal(err)
	}
	if err := b.EnrolDeviceSecret(slotID, u, "ada-pass-1"); !errors.Is(err, ErrAppLocked) {
		t.Fatalf("enrol behind the gate: %v", err)
	}
	bad := append([]byte(nil), u...)
	bad[0] ^= 1
	if err := b.UseDeviceSecret(slotID, bad); err == nil || !b.Gated() {
		t.Fatal("a wrong U opened the gate")
	}
	if err := b.UseDeviceSecret(slotID, u); err != nil || b.Gated() {
		t.Fatalf("the right U did not open the gate: %v", err)
	}
	if len(events) != 2 || !events[0] || events[1] {
		t.Fatalf("gate events %v, want [true false]", events)
	}
	// Off by the user's choice: the gate stays open.
	if err := b.SetAppLock(false); err != nil {
		t.Fatal(err)
	}
	if b.EngageAppLock() {
		t.Fatal("the gate closed with App Lock off")
	}
	// None of U's ways in is an RPC method.
	for _, m := range []string{"UseDeviceSecret", "VerifyDeviceSecret", "EnrolDeviceSecret", "DropDeviceSlot", "SetDeviceWrapper", "EngageAppLock"} {
		if _, err := b.Dispatch(m, nil); err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Errorf("%s is reachable by RPC: %v", m, err)
		}
	}
}

// A slot the phone holds no wrapper for is dropped at the next passphrase
// unlock, not while locked.
func TestAnOrphanSlotIsDroppedAtThePassphraseUnlock(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, u, ""); err != nil {
		t.Fatal(err)
	}
	_ = b.Logout()
	b.SetDeviceWrapper(nil) // the phone lost its half
	dir, _ := appsvc.DataDir()
	if appsvc.DeviceSlotID(dir) == nil {
		t.Fatal("the keystore was written while locked")
	}
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	if appsvc.DeviceSlotID(dir) != nil {
		t.Fatal("the orphan slot outlived a passphrase unlock")
	}
}
