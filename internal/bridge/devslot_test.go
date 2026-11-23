package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
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

func clone(p []byte) []byte { return append([]byte(nil), p...) }

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
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
		t.Fatalf("right after a passphrase unlock: %v", err)
	}
	slotID2, u2 := newSecret()
	if err := b.EnrolDeviceSecret(slotID2, clone(u2), ""); err == nil {
		t.Fatal("the unlock's proof was used twice")
	}
	if err := b.EnrolDeviceSecret(slotID2, clone(u2), "not it"); err == nil {
		t.Fatal("a wrong passphrase enrolled")
	}
	if err := b.EnrolDeviceSecret(slotID2, clone(u2), "ada-pass-1"); err != nil {
		t.Fatalf("with the passphrase typed: %v", err)
	}
	b.mu.Lock()
	b.enrolUntil = time.Now().Add(-time.Second)
	b.mu.Unlock()
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err == nil {
		t.Fatal("an expired proof enrolled")
	}
}

func TestFingerprintUnlocksAndNeverCreates(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
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
	if err := b.UseDeviceSecret(slotID, clone(u)); err != nil {
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
	if err := b2.UseDeviceSecret(slotID, clone(u)); err == nil {
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
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
		t.Fatal(err)
	}
	var events []bool
	b.OnAppLock = func(g bool) { events = append(events, g) }
	if !b.EngageAppLock() {
		t.Fatal("App Lock is on by default once enrolled, but the gate did not close")
	}
	for _, m := range []string{"Guilds", "Identity", "SetAppLock", "AppLock", "Mnemonic", "Messages", "SendMessage", "ChangePassphrase"} {
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
	if err := b.EnrolDeviceSecret(slotID, clone(u), "ada-pass-1"); !errors.Is(err, ErrAppLocked) {
		t.Fatalf("enrol behind the gate: %v", err)
	}
	bad := append([]byte(nil), u...)
	bad[0] ^= 1
	if err := b.UseDeviceSecret(slotID, bad); err == nil || !b.Gated() {
		t.Fatal("a wrong U opened the gate")
	}
	if err := b.UseDeviceSecret(slotID, clone(u)); err != nil || b.Gated() {
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
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
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

// U never reaches a log: not in hex, not in base64, not raw.
func TestTheDeviceSecretIsNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
		t.Fatal(err)
	}
	b.EngageAppLock()
	bad := append([]byte(nil), u...)
	bad[1] ^= 1
	_ = b.UseDeviceSecret(slotID, bad)
	_ = b.UseDeviceSecret(slotID, clone(u))
	_ = b.Logout()
	b.SetDeviceWrapper(nil)
	_ = b.UseDeviceSecret(slotID, clone(u))
	_ = b.Login("ada-pass-1")
	out := buf.String()
	for _, enc := range []string{hex.EncodeToString(u), hex.EncodeToString(bad), base64.StdEncoding.EncodeToString(u), string(u)} {
		if strings.Contains(out, enc) {
			t.Fatal("U is in the log")
		}
	}
}

// The core zeroes U once it is done with it, on every path.
func TestTheCoreZeroesTheDeviceSecret(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	given := clone(u)
	if err := b.EnrolDeviceSecret(slotID, given, ""); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(given, make([]byte, len(given))) {
		t.Fatal("enrol left U in the caller's buffer")
	}
	_ = b.Logout()
	given = clone(u)
	if err := b.UseDeviceSecret(slotID, given); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(given, make([]byte, len(given))) {
		t.Fatal("unlock left U in the caller's buffer")
	}
	bad := clone(u)
	bad[0] ^= 1
	_ = b.UseDeviceSecret(slotID, bad)
	if !bytes.Equal(bad, make([]byte, len(bad))) {
		t.Fatal("a failed unlock left U in the caller's buffer")
	}
}

// The gate is never closed with no session: not by an EngageAppLock racing a
// Logout, and not left over into the next unlock.
func TestNoGateWithoutASession(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			b.EngageAppLock()
		}
	}()
	_ = b.Logout()
	<-done
	if b.Gated() {
		t.Fatal("the gate is closed with no session open")
	}
	// Even a gate closed by hand is opened by the next unlock that starts a
	// session in the foreground.
	b.gated.Store(true)
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	if b.Gated() {
		t.Fatal("a passphrase unlock in the foreground left the gate closed")
	}
}

// A session that opens while the app is off screen is gated at once: onStop
// came before there was anything to gate.
func TestASessionOpenedInTheBackgroundIsGated(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
		t.Fatal(err)
	}
	_ = b.Logout()
	_ = b.SetForeground(false)
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	if !b.Gated() {
		t.Fatal("a passphrase unlock finishing in the background left the session open")
	}
	_ = b.Logout()
	if err := b.UseDeviceSecret(slotID, clone(u)); err != nil {
		t.Fatal(err)
	}
	if !b.Gated() {
		t.Fatal("a fingerprint unlock finishing in the background left the session open")
	}
	if err := b.UseDeviceSecret(slotID, clone(u)); err != nil || !b.Gated() {
		t.Fatalf("the gate opened in the background: %v", err)
	}
	_ = b.SetForeground(true)
	if err := b.UseDeviceSecret(slotID, clone(u)); err != nil || b.Gated() {
		t.Fatalf("the gate did not open in the foreground: %v", err)
	}
	// With App Lock off, a background unlock is not gated.
	if err := b.SetAppLock(false); err != nil {
		t.Fatal(err)
	}
	_ = b.Logout()
	_ = b.SetForeground(false)
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	if b.Gated() {
		t.Fatal("gated with App Lock off")
	}
}

// The passphrase unlock's proof for enrolment does not survive the gate
// closing, a fingerprint unlock, or signing out.
func TestTheEnrolProofEndsWithTheGateAndTheSession(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, clone(u), "ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	expired := func(what string) {
		t.Helper()
		b.mu.Lock()
		left := b.enrolUntil
		b.mu.Unlock()
		if !left.IsZero() {
			t.Fatalf("the enrol proof outlived %s", what)
		}
	}
	b.EngageAppLock()
	expired("the gate closing")
	if err := b.UseDeviceSecret(slotID, clone(u)); err != nil {
		t.Fatal(err)
	}
	_ = b.Logout()
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	if err := b.UseDeviceSecret(slotID, clone(u)); err != nil {
		t.Fatal(err)
	}
	expired("a fingerprint check")
	_ = b.Logout()
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	_ = b.Logout()
	expired("signing out")
}

// Behind the gate a call's signalling passes only while this node is in a
// call, and Mark as read runs but answers nothing.
func TestTheGateLetsACallThroughOnlyWhileInOne(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
		t.Fatal(err)
	}
	b.EngageAppLock()
	for _, m := range []string{"SignalCall", "RelaySignal", "CallIceServers"} {
		if _, err := b.Dispatch(m, nil); !errors.Is(err, ErrAppLocked) {
			t.Errorf("%s behind the gate with no call: %v", m, err)
		}
	}
	for _, ev := range []string{"voice-signal", "voice-presence", "message"} {
		if b.GateEventAllowed(ev) {
			t.Errorf("%s reaches the page behind the gate with no call", ev)
		}
	}
	if !b.GateEventAllowed("applock") {
		t.Error("the gate's own event is held back")
	}
	b.noteVoiceOwner("chan-1", "client-1")
	for _, m := range []string{"SignalCall", "RelaySignal", "CallIceServers"} {
		if _, err := b.Dispatch(m, nil); errors.Is(err, ErrAppLocked) {
			t.Errorf("%s refused behind the gate during a call", m)
		}
	}
	if !b.GateEventAllowed("voice-signal") || b.GateEventAllowed("message") {
		t.Error("during a call: the signalling must pass, and nothing else")
	}
	b.forgetVoiceOwner("chan-1")
	if v, err := b.Dispatch("MarkRead", nil); errors.Is(err, ErrAppLocked) || v != nil {
		t.Fatalf("MarkRead behind the gate: %v, %v", v, err)
	}
}

// The notifier's context is answered behind the gate, in process, with only
// the self fingerprint and the DM names.
func TestTheNotifierHasItsContextBehindTheGate(t *testing.T) {
	b := slotBridge(t)
	slotID, u := newSecret()
	if err := b.EnrolDeviceSecret(slotID, clone(u), ""); err != nil {
		t.Fatal(err)
	}
	id, err := b.Identity()
	if err != nil {
		t.Fatal(err)
	}
	b.EngageAppLock()
	if _, err := b.Dispatch("Identity", nil); !errors.Is(err, ErrAppLocked) {
		t.Fatal("Identity is open behind the gate")
	}
	nc, err := b.NotifyContext()
	if err != nil || nc.Self != id.Fingerprint || nc.DMs == nil {
		t.Fatalf("notify context behind the gate: %+v, %v", nc, err)
	}
	if _, err := b.Dispatch("NotifyContext", nil); err == nil {
		t.Fatal("the notifier's context is reachable by RPC")
	}
	_ = b.Logout()
	if _, err := b.NotifyContext(); err == nil {
		t.Fatal("a notify context with no session")
	}
}
