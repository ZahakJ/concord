package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

func rawArgs(vals ...string) []json.RawMessage {
	out := make([]json.RawMessage, len(vals))
	for i, v := range vals {
		out[i], _ = json.Marshal(v)
	}
	return out
}

// newLockedDevice gives a bridge over a temp data dir holding one account,
// created and then signed out of, the way a real lock screen finds it.
func newLockedDevice(t *testing.T, pass, name string) (*Bridge, string) {
	t.Helper()
	t.Setenv("CONCORD_HOME", t.TempDir())
	t.Setenv("CONCORD_DISABLE_MDNS", "1")
	t.Setenv("CONCORD_BOOTSTRAP", "")
	b := New(context.Background())
	t.Cleanup(b.Close)
	if err := b.Login(pass); err != nil {
		t.Fatal(err)
	}
	svc, _ := b.service()
	if err := svc.SetDisplayName(name); err != nil {
		t.Fatal(err)
	}
	phrase, err := b.RevealMnemonicWith(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Logout(); err != nil {
		t.Fatal(err)
	}
	return b, phrase
}

func codeOf(err error) (string, any) {
	var c *CodedError
	if errors.As(err, &c) {
		return c.Code, c.Detail
	}
	return "", nil
}

// The forgot-passphrase restore through the RPC surface: a phrase for another
// account comes back as a coded refusal naming both accounts and changes
// nothing; the confirmation the UI sends after asking makes it go through.
func TestBridgeRestoreOverExistingBranches(t *testing.T) {
	b, ownPhrase := newLockedDevice(t, "bea-pass", "Bea")

	other, _ := identity.Generate()
	otherPhrase, _ := other.Mnemonic()

	chk, err := b.Dispatch("CheckRecoveryPhrase", rawArgs(otherPhrase))
	if err != nil {
		t.Fatal(err)
	}
	if pc := chk.(PhraseCheck); pc.Match != "different" || pc.KeepsData || pc.Device.Name != "Bea" || pc.Incoming != other.Fingerprint() {
		t.Fatalf("check for another account's phrase: %+v", pc)
	}

	_, err = b.Dispatch("RestoreOverExisting", rawArgs(otherPhrase, "new-pass", ""))
	code, detail := codeOf(err)
	if code != CodeReplaceRefused {
		t.Fatalf("unconfirmed restore over another account: %v", err)
	}
	if r := detail.(ReplaceRefusal); r.Device.Name != "Bea" || r.Incoming != other.Fingerprint() {
		t.Fatalf("refusal detail: %+v", r)
	}
	if err := b.Login("bea-pass"); err != nil {
		t.Fatalf("refused restore broke the account on the device: %v", err)
	}
	_ = b.Logout()

	// Same account, never linked: no confirmation, data kept.
	chk, _ = b.Dispatch("CheckRecoveryPhrase", rawArgs(ownPhrase))
	if pc := chk.(PhraseCheck); pc.Match != "same" || !pc.KeepsData {
		t.Fatalf("check for own phrase: %+v", pc)
	}
	if _, err := b.Dispatch("RestoreOverExisting", rawArgs(ownPhrase, "fresh-pass", "")); err != nil {
		t.Fatalf("same-account restore: %v", err)
	}
	if err := b.Login("fresh-pass"); err != nil {
		t.Fatal(err)
	}
	svc, _ := b.service()
	if svc.DisplayName() != "Bea" {
		t.Fatalf("same-account restore lost the profile: %q", svc.DisplayName())
	}
	_ = b.Logout()

	// Confirmed, with the token the check gave the screen: the other account
	// replaces Bea.
	chk, _ = b.Dispatch("CheckRecoveryPhrase", rawArgs(otherPhrase))
	if _, err := b.Dispatch("RestoreOverExisting", rawArgs(otherPhrase, "new-pass", chk.(PhraseCheck).Confirm)); err != nil {
		t.Fatalf("confirmed restore: %v", err)
	}
	if err := b.Login("new-pass"); err != nil {
		t.Fatal(err)
	}
	svc, _ = b.service()
	if svc.Fingerprint() != other.Fingerprint() || svc.DisplayName() == "Bea" {
		t.Fatalf("after confirmed restore: %s %q", svc.Fingerprint(), svc.DisplayName())
	}
}

// A locked core says WHY it is locked, so a window can tell a restart from an
// unlink without reading an error message.
func TestBridgeSessionStateReasons(t *testing.T) {
	b, _ := newLockedDevice(t, "pw", "Ada")
	if v := b.SessionState(); v.Unlocked || v.Reason != LockSignedOut || !v.HasIdentity {
		t.Fatalf("after sign-out: %+v", v)
	}
	// A fresh process over the same data: nothing here ended a session.
	fresh := New(context.Background())
	defer fresh.Close()
	if v := fresh.SessionState(); v.Reason != LockRestarted || !v.HasIdentity {
		t.Fatalf("fresh core: %+v", v)
	}
	_, err := fresh.Dispatch("Guilds", nil)
	if code, _ := codeOf(err); code != CodeLocked {
		t.Fatalf("locked call: %v (code %q)", err, code)
	}
	if err := fresh.Login("pw"); err != nil {
		t.Fatal(err)
	}
	if v := fresh.SessionState(); !v.Unlocked || v.Reason != "" {
		t.Fatalf("unlocked: %+v", v)
	}
}

// Changing the passphrase needs the current one; afterwards only the new one
// opens the device.
func TestBridgeChangePassphrase(t *testing.T) {
	b, _ := newLockedDevice(t, "old-pass", "Ada")
	if _, err := b.Dispatch("ChangePassphrase", rawArgs("old-pass", "new-pass")); err == nil {
		t.Fatal("changed the passphrase while locked")
	}
	if err := b.Login("old-pass"); err != nil {
		t.Fatal(err)
	}
	_, err := b.Dispatch("ChangePassphrase", rawArgs("wrong", "new-pass"))
	if code, _ := codeOf(err); code != CodeWrongPassphrase {
		t.Fatalf("wrong current passphrase: %v", err)
	}
	if _, err := b.Dispatch("ChangePassphrase", rawArgs("old-pass", "new-pass")); err != nil {
		t.Fatal(err)
	}
	_ = b.Logout()
	if code, _ := codeOf(b.Login("old-pass")); code != CodeWrongPassphrase {
		t.Fatal("old passphrase still unlocks after a change")
	}
	if err := b.Login("new-pass"); err != nil {
		t.Fatalf("new passphrase: %v", err)
	}
	// The recovery phrase needs the passphrase too.
	if _, err := b.Dispatch("RevealMnemonic", rawArgs("old-pass")); err == nil {
		t.Fatal("revealed the recovery phrase with a stale passphrase")
	}
	if p, err := b.Dispatch("RevealMnemonic", rawArgs("new-pass")); err != nil || p.(string) == "" {
		t.Fatalf("reveal with the passphrase: %v", err)
	}
}

// An unlock that finds the keystore's clear fields altered tells the user
// once, through the Login result; the next unlock, after the correction, is
// quiet.
func TestLoginSaysOnceThatTheKeystoreLabelWasAltered(t *testing.T) {
	b, _ := newLockedDevice(t, "pw", "Ada")
	ks, err := appsvc.KeystorePath()
	if err != nil {
		t.Fatal(err)
	}
	// Someone with the device's files changes a clear field.
	raw, _ := os.ReadFile(ks)
	var env map[string]json.RawMessage
	_ = json.Unmarshal(raw, &env)
	env["version"] = json.RawMessage("1")
	out, _ := json.Marshal(env)
	if err := os.WriteFile(ks, out, 0o600); err != nil {
		t.Fatal(err)
	}
	notice, err := b.Dispatch("Login", rawArgs("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := notice.(string); !strings.Contains(s, "altered") {
		t.Fatalf("the unlock said nothing about the altered keystore: %q", notice)
	}
	// Wait for the correction behind the unlock.
	deadline := time.Now().Add(20 * time.Second)
	for {
		raw, _ = os.ReadFile(ks)
		_ = json.Unmarshal(raw, &env)
		if string(env["version"]) == "2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the altered field was never corrected")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = b.Logout()
	if notice, err := b.Dispatch("Login", rawArgs("pw")); err != nil || notice != "" {
		t.Fatalf("second unlock: %q, %v; want no notice", notice, err)
	}
}
