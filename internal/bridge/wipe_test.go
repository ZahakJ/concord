package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

// armTicket plants an outstanding confirmation without needing a live session,
// and guarantees the package-level state is clean afterwards.
func armTicket(t *testing.T, ticket, phrase string, issued time.Time) {
	t.Helper()
	wipeMu.Lock()
	wipeTicket, wipePhrase, wipeIssued = ticket, phrase, issued
	wipeMu.Unlock()
	t.Cleanup(func() {
		wipeMu.Lock()
		wipeTicket, wipePhrase = "", ""
		wipeMu.Unlock()
	})
}

// TestWipeTicketIsSingleUse: the whole reason the erase is two calls is that
// one call would be a one-liner against the loopback API. A ticket that could
// be replayed would give that back.
func TestWipeTicketIsSingleUse(t *testing.T) {
	armTicket(t, "abc123", "euclid", time.Now())

	phrase, ok := takeWipeTicket("abc123")
	if !ok || phrase != "euclid" {
		t.Fatalf("first redeem: phrase=%q ok=%v, want \"euclid\" true", phrase, ok)
	}
	if _, ok := takeWipeTicket("abc123"); ok {
		t.Fatal("the same ticket was accepted twice")
	}
}

func TestWipeTicketRejectsWrongAndMissing(t *testing.T) {
	armTicket(t, "abc123", "euclid", time.Now())
	if _, ok := takeWipeTicket("nope"); ok {
		t.Fatal("a ticket nobody minted was accepted")
	}
	if _, ok := takeWipeTicket(""); ok {
		t.Fatal("an empty ticket was accepted")
	}
	// A wrong guess must not have consumed the real one — the dialog is still
	// open and the user has not typed anything yet.
	if _, ok := takeWipeTicket("abc123"); !ok {
		t.Fatal("a wrong guess invalidated the outstanding ticket")
	}

	// With nothing outstanding, an empty ticket must not match empty state.
	if _, ok := takeWipeTicket(""); ok {
		t.Fatal("an empty ticket matched an empty slot")
	}
}

func TestWipeTicketExpires(t *testing.T) {
	armTicket(t, "abc123", "euclid", time.Now().Add(-wipeTicketTTL-time.Second))
	if _, ok := takeWipeTicket("abc123"); ok {
		t.Fatal("an expired ticket was accepted")
	}
}

// TestConfirmWipeSpendsTheTicketOnAMismatch pins the ordering that makes a
// mistyped confirmation safe AND non-repeatable: the phrase is checked after
// the ticket is spent, so a wrong answer costs a fresh trip through the dialog
// rather than turning into an unlimited guessing loop against a live ticket.
// Both branches return before the bridge needs a session, which is why this can
// run against a bare Bridge.
func TestConfirmWipeSpendsTheTicketOnAMismatch(t *testing.T) {
	b := &Bridge{}
	armTicket(t, "abc123", "euclid", time.Now())

	err := b.ConfirmWipe("abc123", "eucild")
	if err == nil {
		t.Fatal("a mistyped confirmation was accepted")
	}
	if !strings.Contains(err.Error(), "didn't match") {
		t.Fatalf("mismatch error = %q, want the didn't-match wording", err)
	}
	// Now the right phrase, same ticket: it must be gone.
	err = b.ConfirmWipe("abc123", "euclid")
	if err == nil {
		t.Fatal("the spent ticket still erased the device")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("replay error = %q, want the expired wording", err)
	}
}

// TestConfirmWipeErasesTheDataDir runs the real thing against a temporary
// CONCORD_HOME: a correctly typed confirmation must leave nothing behind that
// HasIdentity would still recognise. The phrase is a display name being copied
// by hand, so it is matched case-insensitively and trimmed — refusing
// "  Euclid " would be pedantry, not security; the security is that it had to
// be read off their own screen at all.
func TestConfirmWipeErasesTheDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONCORD_HOME", dir)

	// Stand in for a real install: the two files ResetIdentity is defined by,
	// plus the MLS directory and the plaintext peer cache that outlive it.
	for _, name := range []string{"keystore.json", "concord.db", "concord.db-wal", "peers.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "mls"), 0o700); err != nil {
		t.Fatalf("seed mls dir: %v", err)
	}
	if got, err := (&Bridge{}).HasIdentity(); err != nil || !got {
		t.Fatalf("setup: HasIdentity = %v, %v — want true", got, err)
	}

	b := &Bridge{}
	armTicket(t, "abc123", "Euclid", time.Now())
	if err := b.ConfirmWipe("abc123", "  euclid "); err != nil {
		t.Fatalf("ConfirmWipe: %v", err)
	}

	if got, err := b.HasIdentity(); err != nil || got {
		t.Fatalf("after the wipe HasIdentity = %v, %v — want false", got, err)
	}
	for _, name := range []string{"keystore.json", "concord.db", "concord.db-wal", "peers.json", "mls"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s survived the wipe (stat err = %v)", name, err)
		}
	}
}

// While a link is running (it waits on the network without the bridge lock,
// then replaces the identity), nothing else may unlock, restore, reset or
// start a second link from another window.
func TestIdentityChangesWaitForALinkInProgress(t *testing.T) {
	t.Setenv("CONCORD_HOME", t.TempDir())
	b := &Bridge{linking: true}
	for name, call := range map[string]func() error{
		"Login":               func() error { return b.Login("pw") },
		"RestoreFromMnemonic": func() error { return b.RestoreFromMnemonic("x", "pw", "") },
		"RestoreOverExisting": func() error { return b.RestoreOverExisting("x", "pw", "") },
		"ResetIdentity":       func() error { return b.ResetIdentity("", StartOverWord) },
		"RedeemLinkCode":      func() error { _, err := b.RedeemLinkCode("x", "pw", ""); return err },
	} {
		if err := call(); !errors.Is(err, errLinkInProgress) {
			t.Errorf("%s during a link: %v, want it refused", name, err)
		}
	}
}

// The wipe sets why the session ended and erases under the bridge lock, so a
// window asking for the session state, or unlocking, while it runs sees
// either the session before the wipe or the wiped device, never a half of
// each. Run under -race: the reason used to be written a second time
// without the lock.
func TestConfirmWipeIsOrderedAgainstTheSessionState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONCORD_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "keystore.json"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{}
	armTicket(t, "abc123", "Euclid", time.Now())
	stop := make(chan struct{})
	seen := make(chan string, 1)
	go func() {
		last := ""
		for {
			select {
			case <-stop:
				seen <- last
				return
			default:
				last = b.SessionState().Reason
			}
		}
	}()
	if err := b.ConfirmWipe("abc123", "euclid"); err != nil {
		t.Fatal(err)
	}
	close(stop)
	if got := <-seen; got != "" && got != LockWiped {
		t.Fatalf("session state during the wipe: %q", got)
	}
	if got := b.SessionState(); got.Reason != LockWiped || got.HasIdentity {
		t.Fatalf("after the wipe: %+v", got)
	}
}

// A link that panics (the HTTP server recovers it and keeps the process
// alive) must not leave the bridge believing a link is still running, which
// would refuse every unlock, restore and link until a restart.
func TestPanickingLinkDoesNotLockTheBridgeOut(t *testing.T) {
	t.Setenv("CONCORD_HOME", t.TempDir())
	orig := redeemLink
	t.Cleanup(func() { redeemLink = orig })
	redeemLink = func(context.Context, string, string, string, string) (appsvc.LinkResult, error) {
		panic("link exploded")
	}
	b := &Bridge{}
	func() {
		defer func() { _ = recover() }()
		_, _ = b.RedeemLinkCode("code", "pw", "")
	}()
	redeemLink = func(context.Context, string, string, string, string) (appsvc.LinkResult, error) {
		return appsvc.LinkResult{}, errors.New("no issuer")
	}
	if _, err := b.RedeemLinkCode("code", "pw", ""); errors.Is(err, errLinkInProgress) {
		t.Fatal("after a link panicked, the next link is refused as one still running")
	}
}

// A wipe whose keystore removal is deferred is reported as done with one
// file to go, under its own code, never as a failure.
func TestDeferredEraseIsReportedAsPending(t *testing.T) {
	err := erasePendingError(fmt.Errorf("x: %w", identity.ErrEraseDeferred))
	if code, _ := codeOf(err); code != CodeErasePending {
		t.Fatalf("deferred erase reported as %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "fail") {
		t.Fatalf("a deferred erase reads as a failure: %q", err)
	}
	if erasePendingError(nil) != nil {
		t.Fatal("a complete wipe reported something")
	}
}

// A deferred keystore erase with data left over is not reported as done.
func TestDeferredEraseWithDataLeftIsNotReportedDone(t *testing.T) {
	err := erasePendingError(errors.Join(identity.ErrEraseDeferred, fmt.Errorf("%w: x", appsvc.ErrDataNotErased)))
	if code, _ := codeOf(err); code == CodeErasePending || err == nil {
		t.Fatalf("reported as done with one file to go: %v", err)
	}
}

// An erase that could not remove everything says exactly what is still here,
// and does not promise that anything will remove it later on its own.
func TestPartialEraseSaysWhatIsLeft(t *testing.T) {
	err := erasePendingError(fmt.Errorf("%w: still here: concord.db, mls", appsvc.ErrDataNotErased))
	if err == nil || !strings.Contains(err.Error(), "concord.db, mls") {
		t.Fatalf("the partial erase does not name what is left: %v", err)
	}
	if strings.Contains(err.Error(), "removes it before") {
		t.Fatalf("the message promises a later delete: %v", err)
	}
}
