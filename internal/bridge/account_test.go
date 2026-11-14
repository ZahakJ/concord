package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
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

// An unlock that finds the keystore's clear account key naming another
// account than the one it seals tells the user once, through the Login
// result; the next unlock, after the correction, is quiet.
func TestLoginSaysOnceThatTheKeystoreLabelWasAltered(t *testing.T) {
	b, _ := newLockedDevice(t, "pw", "Ada")
	ks, err := appsvc.KeystorePath()
	if err != nil {
		t.Fatal(err)
	}
	// Someone with the device's files makes it name another account.
	raw, _ := os.ReadFile(ks)
	var env map[string]json.RawMessage
	_ = json.Unmarshal(raw, &env)
	real := string(env["account_pub"])
	other, _ := identity.Generate()
	forged, _ := json.Marshal([]byte(other.PublicKey()))
	env["account_pub"] = forged
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
		if string(env["account_pub"]) == real {
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

// lostKeystore makes Ada's device as it is when its keystore has gone missing
// but its data has not: returns the keystore's bytes, to put back later.
func lostKeystore(t *testing.T) (b *Bridge, ks string, saved []byte) {
	t.Helper()
	b, _ = newLockedDevice(t, "pw", "Ada")
	ks, _ = appsvc.KeystorePath()
	saved, err := os.ReadFile(ks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ks); err != nil {
		t.Fatal(err)
	}
	return b, ks, saved
}

// adaIsBack puts the keystore back and checks the account opens with its data.
func adaIsBack(t *testing.T, b *Bridge, ks string, saved []byte) {
	t.Helper()
	_ = os.Remove(ks)
	if err := os.WriteFile(ks, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.Login("pw"); err != nil {
		t.Fatal(err)
	}
	svc, _ := b.service()
	if svc.DisplayName() != "Ada" {
		t.Fatalf("the account came back without its data: %q", svc.DisplayName())
	}
}

// The keystore is lost and the user unlocks before restoring: no account is
// created and nothing is deleted. The data is still there for the recovery
// phrase, or for the keystore when it is found.
func TestUnlockWithTheKeystoreLostDeletesNothing(t *testing.T) {
	b, ks, saved := lostKeystore(t)
	err := b.Login("new")
	if code, _ := codeOf(err); code != CodeDataWithoutKeystore {
		t.Fatalf("unlock with the keystore lost: %v", err)
	}
	// The words match the screen, which offers its choices below and has no
	// "Forgot passphrase" link to follow on the create form.
	if msg := err.Error(); !strings.Contains(msg, "Nothing was deleted") || strings.Contains(msg, "Forgot passphrase") {
		t.Fatalf("the message does not say what happened in the screen's terms: %q", msg)
	}
	if _, err := os.Stat(ks); !os.IsNotExist(err) {
		t.Fatal("an account was created beside the data")
	}
	adaIsBack(t, b, ks, saved)
}

// The keystore is a symlink to a drive that is not mounted: that is not "no
// keystore". Nothing is deleted, and after the drive returns the account
// opens with everything.
func TestUnlockWithTheKeystoreOnAnUnmountedDriveDeletesNothing(t *testing.T) {
	b, ks, saved := lostKeystore(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "unmounted", "keystore.json"), ks); err != nil {
		t.Skip("no symlinks here")
	}
	if err := b.Login("new"); err == nil {
		t.Fatal("unlock through a dangling keystore link succeeded")
	}
	adaIsBack(t, b, ks, saved)
}

// A data directory copied back file by file gets the database before the
// keystore; a service unlocking itself in that window (Login called
// directly) creates nothing, deletes nothing, and says so in the log.
func TestServiceStartingMidCopyDeletesNothing(t *testing.T) {
	b, ks, saved := lostKeystore(t)
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	if err := b.Login("from-a-file"); err == nil {
		t.Fatal("a service came up as a new account beside the copied data")
	}
	if !strings.Contains(logged.String(), "no keystore; not creating") {
		t.Fatalf("the refusal was not logged: %q", logged.String())
	}
	adaIsBack(t, b, ks, saved)
}

// An unlock from a headless path (Login called directly, as the passphrase
// file does) has nobody to return the altered-account notice to: it is
// logged, and still returned by the next Login through the UI.
func TestHeadlessUnlockLogsTheNoticeAndKeepsItForTheUI(t *testing.T) {
	b, _ := newLockedDevice(t, "pw", "Ada")
	ks, _ := appsvc.KeystorePath()
	raw, _ := os.ReadFile(ks)
	var env map[string]json.RawMessage
	_ = json.Unmarshal(raw, &env)
	other, _ := identity.Generate()
	forged, _ := json.Marshal([]byte(other.PublicKey()))
	env["account_pub"] = forged
	out, _ := json.Marshal(env)
	if err := os.WriteFile(ks, out, 0o600); err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	if err := b.Login("pw"); err != nil { // headless
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "had been altered") {
		t.Fatalf("a headless unlock did not log the notice: %q", logged.String())
	}
	notice, err := b.Dispatch("Login", rawArgs("pw")) // the UI attaching later
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := notice.(string); !strings.Contains(s, "altered") {
		t.Fatalf("the UI's Login did not get the pending notice: %q", notice)
	}
}

// The reviewer's probe: keystore removed, a different account restored from
// its phrase. It must not come up with the old account's profile; it is
// asked about first.
func TestRestoreFromPhraseNeverComesUpBesideAnotherAccountsData(t *testing.T) {
	b, ks, saved := lostKeystore(t)
	other, _ := identity.Generate()
	phrase, err := other.Mnemonic()
	if err != nil {
		t.Fatal(err)
	}
	err = b.RestoreFromMnemonic(phrase, "new", "")
	if code, _ := codeOf(err); code != CodeReplaceRefused {
		t.Fatalf("restore of another account over Ada's data: %v, want it asked about", err)
	}
	adaIsBack(t, b, ks, saved)
}

// An erase record (an interim build wrote one when a wipe began) authorises
// nothing: the unlock still deletes nothing and creates nothing, and the
// account opens with its data once its keystore is back.
func TestARecordedEraseAuthorisesNothing(t *testing.T) {
	b, ks, saved := lostKeystore(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(ks), "data-erase-pending"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _ := codeOf(b.Login("new")); code != CodeDataWithoutKeystore {
		t.Fatal("an unlock with an erase record and no keystore did not refuse")
	}
	adaIsBack(t, b, ks, saved)
}
