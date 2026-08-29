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

// TestNotesExistsBeforeAnybodyAsks pins the other half of the same problem.
//
// The account-level read-state wire is the Notes group's meta topic, and the
// only reason two devices ever ended up with two Notes groups is that Notes was
// created by whoever asked for it first. If it is a property of having an
// account rather than of having opened a scratchpad, there is nothing to race:
// the group exists before the first hello goes out, so a device being linked
// has exactly one to adopt.
//
// This test does not call NotesDM(). Calling it would create the very thing
// being tested for.
func TestNotesExistsBeforeAnybodyAsks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	svc := startServiceInDir(t, ctx, dir)

	notes, ok := svc.findNotesDM()
	if !ok {
		t.Fatal("a fresh account has no Notes group: read state has no wire to travel on until somebody opens the scratchpad")
	}
	if notes.Kind != "dm" || notes.Name != notesGuildName {
		t.Fatalf("the Notes group is not a Notes group: kind=%q name=%q", notes.Kind, notes.Name)
	}
	if len(notes.Channels) == 0 || notes.Channels[0].Name != "notes" {
		t.Fatalf("Notes has no notes channel: %+v", notes.Channels)
	}

	// And exactly one, restart after restart. A startup mint that ran every
	// time would grow a new group per launch, which is the two-Notes bug with
	// a different cause.
	count := func(s *Service) int {
		n := 0
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, g := range s.guilds {
			if isNotesGuild(*g, s.PublicKey()) {
				n++
			}
		}
		return n
	}
	if n := count(svc); n != 1 {
		t.Fatalf("a fresh account has %d Notes groups, want 1", n)
	}
	_ = svc.Close()

	again := startServiceInDir(t, ctx, dir)
	if n := count(again); n != 1 {
		t.Fatalf("after a restart the account has %d Notes groups, want 1", n)
	}
	same, ok := again.findNotesDM()
	if !ok || same.ID != notes.ID {
		t.Fatalf("the restart adopted a different Notes group (%s, was %s)", same.ID, notes.ID)
	}
}
