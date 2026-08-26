package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
