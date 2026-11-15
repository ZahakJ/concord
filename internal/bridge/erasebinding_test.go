package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

// adaWithAWipeOpen is a device holding Ada, unlocked, with the Settings wipe
// dialog open: "Type Ada to delete everything".
func adaWithAWipeOpen(t *testing.T) (b *Bridge, view WipeView, ks string) {
	t.Helper()
	b, _ = newLockedDevice(t, "ada-pass-1", "Ada")
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	view, err := b.BeginWipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dropWipeTicket)
	if view.Phrase != "Ada" {
		t.Fatalf("setup: the dialog asks for %q", view.Phrase)
	}
	ks, err = appsvc.KeystorePath()
	if err != nil {
		t.Fatal(err)
	}
	return b, view, ks
}

func mustStillHold(t *testing.T, ks string, account []byte, what string) {
	t.Helper()
	pub, err := identity.ReadAccountPub(ks)
	if err != nil {
		t.Fatalf("%s: the keystore is gone or unreadable: %v", what, err)
	}
	if string(pub) != string(account) {
		t.Fatalf("%s: the keystore no longer holds the account it held", what)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(ks), "concord.db")); err != nil {
		t.Fatalf("%s: the database is gone: %v", what, err)
	}
}

// A wipe confirmation belongs to the session whose dialog asked for it. After
// signing out it erases nothing: a locked device is erased through start over,
// with its own confirmation.
func TestAWipeConfirmationEndsWithItsSession(t *testing.T) {
	b, view, ks := adaWithAWipeOpen(t)
	ada, err := identity.ReadAccountPub(ks)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Logout(); err != nil {
		t.Fatal(err)
	}
	if err := b.ConfirmWipe(view.Ticket, "Ada"); err == nil {
		t.Fatal("a wipe confirmed after signing out went through")
	}
	mustStillHold(t, ks, ada, "after a wipe confirmed on a locked device")
}

// The dialog said "Ada". Since then another window signed out, restored Bob
// from his phrase (confirming there that Ada's data goes) and unlocked. Typing
// "Ada" into the first window must not erase Bob.
func TestAWipeConfirmationIsNotForTheNextAccount(t *testing.T) {
	b, view, ks := adaWithAWipeOpen(t)
	if err := b.Logout(); err != nil {
		t.Fatal(err)
	}
	bob, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	phrase, err := bob.Mnemonic()
	if err != nil {
		t.Fatal(err)
	}
	chk, err := b.Dispatch("CheckRecoveryPhrase", rawArgs(phrase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Dispatch("RestoreOverExisting", rawArgs(phrase, "bob-pass-1", chk.(PhraseCheck).Confirm)); err != nil {
		t.Fatal(err)
	}
	if err := b.Login("bob-pass-1"); err != nil {
		t.Fatal(err)
	}
	if err := b.ConfirmWipe(view.Ticket, "Ada"); err == nil {
		t.Fatal("a wipe confirmed for Ada erased the device that now holds Bob")
	}
	mustStillHold(t, ks, bob.PublicKey(), "after Ada's confirmation was used on Bob's device")
	if svc, err := b.service(); err != nil || svc.Fingerprint() != bob.Fingerprint() {
		t.Fatalf("Bob's session did not survive Ada's stale confirmation: %v", err)
	}
}

// Another process on the same data directory (the desktop shell beside the web
// build) replaces the keystore while this one is unlocked as Ada: a confirmed
// restore of Bob. The wipe confirmed in Ada's session must leave Bob's
// keystore alone.
func TestAWipeLeavesAKeystoreAnotherProcessWrote(t *testing.T) {
	b, view, ks := adaWithAWipeOpen(t)
	snap, err := identity.ReadKeystore(ks)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.ReplaceKeystore(ks, "bob-pass-1", bob, snap); err != nil {
		t.Fatal(err)
	}
	err = b.ConfirmWipe(view.Ticket, "Ada")
	if code, _ := codeOf(err); code != CodeConfirmStale {
		t.Fatalf("ConfirmWipe over a keystore written since: %v, want it refused as stale", err)
	}
	mustStillHold(t, ks, bob.PublicKey(), "after a wipe over a keystore another process wrote")
}

// The same account unlocked again is a new session: the old session's
// confirmation erases nothing in it, and it keeps running.
func TestAWipeConfirmationIsNotForTheSameAccountUnlockedAgain(t *testing.T) {
	b, view, ks := adaWithAWipeOpen(t)
	ada, err := identity.ReadAccountPub(ks)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Logout(); err != nil {
		t.Fatal(err)
	}
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	err = b.ConfirmWipe(view.Ticket, "Ada")
	if code, _ := codeOf(err); code != CodeConfirmStale {
		t.Fatalf("ConfirmWipe in a new session of the same account: %v, want CodeConfirmStale", err)
	}
	mustStillHold(t, ks, ada, "after the old session's confirmation was used in a new one")
	if _, err := b.service(); err != nil {
		t.Fatalf("the new session did not survive: %v", err)
	}
}

// A passphrase changed in the same session after the dialog opened rewrites
// the keystore, so the confirmation no longer matches it and nothing is
// erased. The refusal says why, and the user is still signed in: nothing
// happened to the session, and they can open the dialog again.
func TestAWipeAfterAPassphraseChangeKeepsTheSession(t *testing.T) {
	b, view, ks := adaWithAWipeOpen(t)
	ada, err := identity.ReadAccountPub(ks)
	if err != nil {
		t.Fatal(err)
	}
	pokes := 0
	b.OnPresence = func() { pokes++ }
	if err := b.ChangePassphrase("ada-pass-1", "ada-pass-2"); err != nil {
		t.Fatal(err)
	}
	err = b.ConfirmWipe(view.Ticket, "Ada")
	if code, _ := codeOf(err); code != CodeConfirmStale {
		t.Fatalf("ConfirmWipe after a passphrase change: %v, want CodeConfirmStale", err)
	}
	for _, says := range []string{"passphrase", "Nothing was deleted", "still signed in"} {
		if !strings.Contains(err.Error(), says) {
			t.Errorf("the refusal %q does not say %q", err, says)
		}
	}
	mustStillHold(t, ks, ada, "after a refused wipe")
	if _, err := b.service(); err != nil {
		t.Fatalf("a refused wipe signed the user out: %v", err)
	}
	if pokes != 0 {
		t.Fatalf("a refused wipe told the shell the session changed (%d)", pokes)
	}

	view, err = b.BeginWipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ConfirmWipe(view.Ticket, "Ada"); err != nil {
		t.Fatalf("the wipe opened again after the passphrase change: %v", err)
	}
	if _, err := os.Stat(ks); !os.IsNotExist(err) {
		t.Fatalf("the keystore is still here: %v", err)
	}
}
