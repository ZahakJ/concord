package bridge

import (
	"context"
	"errors"
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

// A restore over another account erases it, so its yes takes the typed word
// in the core, as starting over does, whoever calls it: without the word
// nothing is erased, and the account on the device still unlocks.
func TestReplaceNeedsTheTypedWordInTheCore(t *testing.T) {
	b, _ := newLockedDevice(t, "bea-pass", "Bea")
	bea := appsvc.LockedAccount(mustDataDir(t)).Fingerprint
	other, _ := identity.Generate()
	otherPhrase, _ := other.Mnemonic()

	chk, err := b.Dispatch("CheckRecoveryPhrase", rawArgs(otherPhrase))
	if err != nil {
		t.Fatal(err)
	}
	ticket := chk.(PhraseCheck).Confirm
	if ticket == "" {
		t.Fatal("no ticket for a restore that would erase the account here")
	}
	for _, typed := range []string{"", "Bea", "ERAS", "delete"} {
		_, err := b.Dispatch("RestoreOverExisting", rawArgs(otherPhrase, "new-pass", ticket, typed))
		if code, _ := codeOf(err); code != CodeConfirmWord {
			t.Fatalf("a yes with %q typed: %v", typed, err)
		}
	}
	// Looked at without unlocking: an unlock rewrites the device, and the
	// ticket is for the device as it was shown.
	dir, _ := appsvc.DataDir()
	if got := appsvc.LockedAccount(dir); got.Fingerprint != bea || got.Name != "Bea" {
		t.Fatalf("a yes without the word changed the account here: %+v", got)
	}

	// The word does not spend a ticket it never reached: the same yes, now
	// with the word, goes through.
	if _, err := b.Dispatch("RestoreOverExisting", rawArgs(otherPhrase, "new-pass", ticket, " erase ")); err != nil {
		t.Fatalf("the yes with the word: %v", err)
	}
	if err := b.Login("new-pass"); err != nil {
		t.Fatal(err)
	}
	svc, _ := b.service()
	if svc.Fingerprint() != other.Fingerprint() {
		t.Fatal("the confirmed restore did not put the phrase's account here")
	}
}

// The screen's yes is a ticket, not the app's token: the token is derived
// from what is on the device, the same every time, and anyone who can read
// the data directory can compute it. Sent as the yes, it erases nothing.
func TestReplaceRefusesTheComputableToken(t *testing.T) {
	b, _ := newLockedDevice(t, "bea-pass", "Bea")
	other, _ := identity.Generate()
	otherPhrase, _ := other.Mnemonic()
	dir, _ := appsvc.DataDir()
	token := appsvc.ConfirmToken(dir, other.Fingerprint())

	_, err := b.Dispatch("RestoreOverExisting", rawArgs(otherPhrase, "new-pass", token, "ERASE"))
	if code, _ := codeOf(err); code != CodeReplaceRefused {
		t.Fatalf("the computed token was taken as a yes: %v", err)
	}
	if err := b.Login("bea-pass"); err != nil {
		t.Fatalf("the computed token erased the account here: %v", err)
	}
}

// A ticket is used once. A link that fails after the yes (the other device
// went away) has spent it: trying again is no yes, the app asks again with a
// new ticket, and nothing is erased on the strength of the old one.
func TestReplaceTicketIsSingleUse(t *testing.T) {
	t.Setenv("CONCORD_HOME", t.TempDir())
	orig := redeemLink
	t.Cleanup(func() { redeemLink = orig })
	var got []string
	redeemLink = func(_ context.Context, _, _, _, confirm string, _ func(appsvc.LinkWaiting)) (appsvc.LinkResult, error) {
		got = append(got, confirm)
		if confirm == "" {
			return appsvc.LinkResult{}, &appsvc.ReplaceRefusedError{Match: appsvc.MatchDifferent, Confirm: "app-token"}
		}
		return appsvc.LinkResult{}, errors.New("the other device went away")
	}
	b := &Bridge{}

	_, err := b.RedeemLinkCode("code", "pw", "", "")
	code, detail := codeOf(err)
	if code != CodeReplaceRefused {
		t.Fatalf("first try: %v", err)
	}
	ticket := detail.(ReplaceRefusal).Confirm
	if ticket == "" || ticket == "app-token" {
		t.Fatalf("the screen was given %q, not a ticket of its own", ticket)
	}

	if _, err := b.RedeemLinkCode("code", "pw", ticket, "ERASE"); err == nil || errors.Is(err, errLinkInProgress) {
		t.Fatalf("the yes: %v", err)
	}
	_, err = b.RedeemLinkCode("code", "pw", ticket, "ERASE")
	code, detail = codeOf(err)
	if code != CodeReplaceRefused {
		t.Fatalf("the spent ticket was not asked about again: %v", err)
	}
	if again := detail.(ReplaceRefusal).Confirm; again == "" || again == ticket {
		t.Fatalf("asked again with ticket %q (first %q)", again, ticket)
	}
	want := []string{"", "app-token", ""}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("the app was given %q, want %q: a ticket counted twice", got, want)
	}
}

func mustDataDir(t *testing.T) string {
	t.Helper()
	dir, err := appsvc.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
