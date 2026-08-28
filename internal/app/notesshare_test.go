package app

import (
	"context"
	"testing"
	"time"
)

// Read state is account-level and travels on exactly one wire: the meta topic
// of the Notes self-group, whose only members are this account's own devices.
// That makes Notes load-bearing for a feature nothing else implements — and
// Notes is created LAZILY, the first time anything asks for it. For most
// accounts that is after the second device exists.
//
// Before the handover minted it, the sequence "link a phone, then open Notes on
// each" left the account holding two groups both called Notes, in neither of
// which the other device was a member. Nothing merged them. Notes written on
// the desktop never reached the phone, and read state stopped converging
// altogether: each device published markers into a group the other could not
// decrypt, forever, with no error anywhere.
func TestLinkedDevicesShareOneNotesGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	deskNotes, err := desk.NotesDM()
	if err != nil {
		t.Fatalf("desk notes: %v", err)
	}
	phoneNotes, err := phone.NotesDM()
	if err != nil {
		t.Fatalf("phone notes: %v", err)
	}
	if deskNotes.ID != phoneNotes.ID {
		t.Fatalf("the account minted two Notes groups (%s on the desktop, %s on the phone): notes do not sync and read state cannot converge", deskNotes.ID, phoneNotes.ID)
	}

	// And the wire that rides on it works: a read marker set on one device
	// reaches the other.
	at := time.Now().UnixMilli()
	if err := desk.MarkRead(textCh, at); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		rs, err := phone.ReadState()
		return err == nil && rs[textCh] >= at
	}, "the read marker never reached the other device")
}
