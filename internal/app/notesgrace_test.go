package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// dropNotes takes every Notes group out of the device's view, leaving a device
// that holds none.
func dropNotes(s *Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, g := range s.guilds {
		if isNotesGuild(*g, s.PublicKey()) {
			delete(s.guilds, id)
		}
	}
}

// setNotesGrace changes how long a linked device waits for its siblings'
// Notes, for one test.
func setNotesGrace(t *testing.T, d time.Duration) {
	t.Helper()
	was := notesGrace
	notesGrace = d
	t.Cleanup(func() { notesGrace = was })
}

// NotesFirst puts the codes for groups named Notes first and keeps the rest in
// their order, so one unreachable guild cannot hold up the Notes join.
func TestNotesFirst(t *testing.T) {
	code := func(id, name string) string {
		return encodeInviteCode(inviteCode{GuildID: id, GuildName: name, OwnerID: "x"})
	}
	a, n, b, m := code("a", "Shared"), code("n", "Notes"), code("b", "Games"), code("m", "Notes")
	got := NotesFirst([]string{a, n, b, "not a code", m})
	want := []string{n, m, a, b, "not a code"}
	if !slices.Equal(got, want) {
		t.Fatalf("NotesFirst order wrong:\n got %q\nwant %q", got, want)
	}
}

// A guild the user merely called "Notes" (a server, or a group DM renamed to
// it) is offered by a sibling like any other group. It is not a Notes group,
// and it must not stand in for one: a device holding no Notes still gets one.
func TestAGuildNamedNotesDoesNotBlockNotes(t *testing.T) {
	for _, linked := range []bool{true, false} {
		name := "original device"
		if linked {
			name = "linked device"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := startServiceInDir(t, ctx, t.TempDir())
			srv, err := s.CreateGuild("Notes")
			if err != nil {
				t.Fatal(err)
			}
			code, err := s.InviteCode(srv.ID)
			if err != nil {
				t.Fatal(err)
			}
			ic, err := decodeInviteCode(code)
			if err != nil {
				t.Fatal(err)
			}
			dropNotes(s)
			s.linkedDevice = linked
			// What a sibling's hello does when it offers this guild and no
			// Notes group.
			s.noteNotesOffer(code, ic)
			s.joiningMu.Lock()
			s.siblingOffered = true
			s.joiningMu.Unlock()
			for i := range 3 {
				g, err := s.NotesDM()
				if err != nil {
					t.Fatalf("NotesDM attempt %d, after a sibling offered its groups and none was a Notes group: %v", i+1, err)
				}
				if !isNotesGuild(g, s.PublicKey()) || g.ID == srv.ID {
					t.Fatalf("NotesDM attempt %d returned %s (%q), not a Notes group", i+1, g.ID, g.Name)
				}
			}
		})
	}
}

// The other copy is listed as a conversation of its own, and a conversation
// can be closed. Closing it must not put its notes out of reach: opening Notes
// lists every copy that holds notes again.
func TestClosedOtherCopyComesBackWhenNotesIsOpened(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	first, ok := s.findNotesDM()
	if !ok {
		t.Fatal("no Notes")
	}
	// Mint a second, real Notes group: mask the first for the one call.
	s.mu.Lock()
	s.guilds[first.ID].Name = "masked"
	s.mu.Unlock()
	second, err := s.ensureNotesDM()
	s.mu.Lock()
	s.guilds[first.ID].Name = notesGuildName
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || !s.NotesCopy(second.ID) || s.NotesCopy(first.ID) {
		t.Fatalf("setup: want %s canonical and %s the copy", first.ID, second.ID)
	}
	if _, err := s.SendMessage(second.Channels[0].ID, "a note in the other copy", "", ""); err != nil {
		t.Fatal(err)
	}
	listed := func() bool {
		return slices.ContainsFunc(s.Guilds(), func(g domain.Guild) bool { return g.ID == second.ID })
	}
	if !listed() {
		t.Fatal("setup: the copy holding a note is not listed")
	}
	if err := s.LeaveGuild(second.ID); err != nil { // "Close" on the row
		t.Fatal(err)
	}
	if listed() {
		t.Fatal("setup: closing did not hide the copy")
	}
	if g, err := s.NotesDM(); err != nil || g.ID != first.ID {
		t.Fatalf("NotesDM: %v %v", g.ID, err)
	}
	if !listed() {
		t.Errorf("the closed copy %s still holds a note and opening Notes does not list it again", second.ID)
	}
}

// A linked device whose siblings never offer anything (the laptop is closed,
// lost, or unlinked) waits for the account's Notes only for a while, and then
// gets one of its own. The account's, if it ever arrives, is older and wins.
func TestLinkedDeviceWithNoSiblingGetsNotesAfterAWhile(t *testing.T) {
	setNotesGrace(t, 2*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	dropNotes(s)
	s.linkedDevice = true
	// At first it waits: the account's may be one hello away.
	if _, err := s.NotesDM(); !errors.Is(err, ErrNotesArriving) {
		t.Fatalf("NotesDM straight after start: err=%v, want ErrNotesArriving", err)
	}
	var err error
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = s.NotesDM(); err == nil {
			break
		}
		if !errors.Is(err, ErrNotesArriving) {
			t.Fatalf("NotesDM: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("30s after start, with no sibling anywhere, Notes still will not open: %v", err)
	}
	if ids := notesIDs(s); len(ids) != 1 {
		t.Fatalf("want one Notes, have %v", ids)
	}
}

// A Notes offered by a sibling whose join fails (its device went away) does
// not keep the device from having Notes.
func TestAFailedNotesOfferDoesNotBlockNotes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gone := startServiceInDir(t, ctx, t.TempDir())
	gn, ok := gone.findNotesDM()
	if !ok {
		t.Fatal("no Notes")
	}
	code, err := gone.InviteCode(gn.ID)
	if err != nil {
		t.Fatal(err)
	}
	ic, err := decodeInviteCode(code)
	if err != nil {
		t.Fatal(err)
	}
	_ = gone.Close()

	s := startServiceInDir(t, ctx, t.TempDir())
	dropNotes(s)
	s.linkedDevice = true
	s.noteNotesOffer(code, ic)
	s.joiningMu.Lock()
	s.siblingOffered = true
	s.joiningMu.Unlock()
	start := time.Now()
	g, err := s.NotesDM()
	if err != nil {
		t.Fatalf("NotesDM after the offered Notes could not be joined (%s): %v", time.Since(start).Round(time.Millisecond), err)
	}
	if !isNotesGuild(g, s.PublicKey()) {
		t.Fatalf("NotesDM returned %q", g.Name)
	}
}

// An archive's notes land in Notes on a linked device that is still waiting
// for the account's: the import does not skip them because of the wait.
func TestArchiveNotesLandOnALinkedDeviceStillWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := startServiceInDir(t, ctx, t.TempDir())
	writeNote(t, src, "kept in the archive")
	sealed, _, err := src.ExportArchive("archive-pass", false)
	if err != nil {
		t.Fatal(err)
	}
	s := startServiceInDir(t, ctx, t.TempDir())
	dropNotes(s)
	s.linkedDevice = true
	if _, err := s.ImportArchive(sealed, "archive-pass"); err != nil {
		t.Fatalf("import: %v", err)
	}
	if !notesOn(s, "kept in the archive") {
		t.Fatal("the archive's note was skipped because this device was waiting for the account's Notes")
	}
}

// A sibling that holds no groups at all still tells a linked device that the
// account has no Notes to hand it, so the device need not sit out the grace.
func TestASiblingWithNoGroupsReleasesTheWait(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	// Long enough that only the sibling's hello can release it.
	setNotesGrace(t, 10*time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk := startServiceOn(t, ctx, t.TempDir(), boot)
	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	phoneDir := t.TempDir()
	if _, err := RedeemLink(ctx, phoneDir, code, "test-pass", ""); err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	// The desktop holds nothing (its Notes is gone), so its hello offers
	// nothing.
	if n, ok := desk.findNotesDM(); ok {
		_ = desk.store.DeleteGuild(n.ID)
	}
	dropNotes(desk)
	phone := startServiceOn(t, ctx, phoneDir, boot)
	start := time.Now()
	var got error
	for time.Since(start) < 10*time.Second {
		if _, got = phone.NotesDM(); got == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if got != nil {
		t.Fatalf("10s after start, with the desktop's hello in hand and no Notes anywhere, Notes will not open: %v", got)
	}
}

// Notes reaches a linked phone by its sibling's hello alone (the handover's
// codes are never redeemed here), and opening Notes before it lands ends in
// the account's Notes, not a new one.
func TestNotesArrivesByHelloAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk := startServiceOn(t, ctx, t.TempDir(), boot)
	account, ok := desk.findNotesDM()
	if !ok {
		t.Fatal("no Notes")
	}
	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	phoneDir := t.TempDir()
	if _, err := RedeemLink(ctx, phoneDir, code, "test-pass", ""); err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := startServiceOn(t, ctx, phoneDir, boot)
	if g, err := phone.NotesDM(); err == nil && g.ID != account.ID {
		t.Fatalf("opened before the hello landed, Notes is %s, not the account's %s", g.ID, account.ID)
	}
	waitUntil(t, 60*time.Second, func() bool {
		g, err := phone.NotesDM()
		return err == nil && g.ID == account.ID
	}, "the phone never opened the account's Notes")
	if ids := notesIDs(phone); len(ids) != 1 {
		t.Fatalf("the phone holds %v, want only %s", ids, account.ID)
	}
}
