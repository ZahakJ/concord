package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// notesIDs is every group this device holds that is a Notes group by the one
// predicate that defines one, sorted.
func notesIDs(s *Service) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	for _, g := range s.guilds {
		if isNotesGuild(*g, s.PublicKey()) {
			ids = append(ids, g.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// shownAsNotes counts the Notes groups this device presents as Notes: every
// copy but the canonical one is shown, if at all, under another name.
func shownAsNotes(s *Service) int {
	n := 0
	for _, id := range notesIDs(s) {
		if !s.NotesCopy(id) {
			n++
		}
	}
	return n
}

// notesOn reports whether content is stored in any Notes group on s.
func notesOn(s *Service, content string) bool {
	for _, id := range notesIDs(s) {
		s.mu.RLock()
		g, ok := s.guilds[id]
		var ch string
		if ok && len(g.Channels) > 0 {
			ch = g.Channels[0].ID
		}
		s.mu.RUnlock()
		if ch == "" {
			continue
		}
		msgs, _ := s.Messages(ch, 500)
		for _, m := range msgs {
			if m.Content == content {
				return true
			}
		}
	}
	return false
}

// writeNote writes content into the group NotesDM names on s.
func writeNote(t *testing.T, s *Service, content string) string {
	t.Helper()
	g, err := s.NotesDM()
	if err != nil {
		t.Fatalf("NotesDM: %v", err)
	}
	if _, err := s.SendMessage(g.Channels[0].ID, content, "", ""); err != nil {
		t.Fatalf("write a note: %v", err)
	}
	return g.ID
}

// copyTree copies a closed data directory, as a backup does. SQLite may still
// be removing its shared-memory file just after Close returns; a pass that
// saw a file vanish is taken again, so the copy is of a settled directory.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	var err error
	for range 20 {
		vanished := false
		err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					vanished = true
					return nil
				}
				return err
			}
			rel, _ := filepath.Rel(src, p)
			to := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(to, 0o700)
			}
			if !d.Type().IsRegular() {
				return nil
			}
			b, err := os.ReadFile(p)
			if os.IsNotExist(err) {
				vanished = true
				return nil
			}
			if err != nil {
				return err
			}
			return os.WriteFile(to, b, 0o600)
		})
		if err == nil && !vanished {
			return
		}
		time.Sleep(200 * time.Millisecond)
		_ = os.RemoveAll(dst)
	}
	t.Fatalf("copy %s: %v (or files kept vanishing)", src, err)
}

// restoreTree replaces dst with the snapshot in src.
func restoreTree(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	copyTree(t, src, dst)
}

// sameNotesEverywhere waits until every service names the same group when
// asked for Notes, asking each many times: an account holding two Notes groups
// answered from an unordered map, so one agreeing answer proves nothing.
func sameNotesEverywhere(t *testing.T, within time.Duration, svcs ...*Service) string {
	t.Helper()
	var last []string
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		ids := map[string]bool{}
		for _, s := range svcs {
			for range 25 {
				g, err := s.NotesDM()
				if err == nil {
					ids[g.ID] = true
				} else {
					ids["error: "+err.Error()] = true
				}
			}
		}
		if len(ids) == 1 {
			for id := range ids {
				if !strings.HasPrefix(id, "error: ") {
					return id
				}
			}
		}
		last = last[:0]
		for id := range ids {
			last = append(last, id)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("the account's devices do not agree on one Notes: asked repeatedly, they named %v", last)
	return ""
}

// A phone opens Notes before the handover's invites have been joined (the
// Messages tab lands on Notes when there is no other DM). It must not mint a
// Notes of its own while the account's is on its way.
func TestNotesOpenedBeforeTheHandoverIsAdopted(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)

	desk := startServiceOn(t, ctx, t.TempDir(), boot)
	accountNotes, ok := desk.findNotesDM()
	if !ok {
		t.Fatal("the desktop has no Notes")
	}
	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	phoneDir := t.TempDir()
	res, err := RedeemLink(ctx, phoneDir, code, "test-pass", "")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := startServiceOn(t, ctx, phoneDir, boot)

	// The user taps Messages before anything has been joined.
	early, earlyErr := phone.NotesDM()
	if earlyErr == nil && early.ID != accountNotes.ID {
		t.Logf("the phone minted %s while the account's Notes %s was being handed over", early.ID, accountNotes.ID)
	}
	for _, ic := range res.GuildInvites {
		phone.JoinLinkedInvite(ic)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return slices.Contains(notesIDs(phone), accountNotes.ID)
	}, "the phone never adopted the account's Notes")
	// Let the hello offers run both ways.
	time.Sleep(5 * time.Second)

	if got := notesIDs(phone); len(got) != 1 {
		t.Errorf("the phone holds %d Notes groups %v, want only the account's %s", len(got), got, accountNotes.ID)
	}
	if got := notesIDs(desk); len(got) != 1 {
		t.Errorf("the desktop holds %d Notes groups %v, want only %s", len(got), got, accountNotes.ID)
	}
	if id := sameNotesEverywhere(t, 30*time.Second, desk, phone); id != accountNotes.ID {
		t.Errorf("the account settled on %s, not its original Notes %s", id, accountNotes.ID)
	}
}

// A linked phone that joined some groups but not yet Notes, and was then
// restarted (Android kills background apps freely), must not mint a Notes at
// startup: the account has one, and the next hello brings it.
func TestLinkedDeviceRestartedBeforeNotesArrivedAdoptsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)

	deskDir := t.TempDir()
	desk := startServiceOn(t, ctx, deskDir, boot)
	accountNotes, ok := desk.findNotesDM()
	if !ok {
		t.Fatal("the desktop has no Notes")
	}
	alice := startServiceOn(t, ctx, t.TempDir(), boot)
	ag, err := alice.CreateGuild("Alice's")
	if err != nil {
		t.Fatal(err)
	}
	aliceCode, err := alice.InviteCode(ag.ID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	phoneDir := t.TempDir()
	if _, err := RedeemLink(ctx, phoneDir, code, "test-pass", ""); err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	// The desktop goes away before the phone has joined Notes.
	_ = desk.Close()

	phone := startServiceOn(t, ctx, phoneDir, boot)
	if _, err := phone.JoinViaInvite(aliceCode); err != nil {
		t.Fatalf("the phone could not join Alice's guild: %v", err)
	}
	_ = phone.Close()
	phone = startServiceOn(t, ctx, phoneDir, boot)
	if got := notesIDs(phone); len(got) != 0 {
		t.Errorf("the restarted phone minted a Notes of its own (%v) while the account's %s was one hello away", got, accountNotes.ID)
	}

	desk = startServiceOn(t, ctx, deskDir, boot)
	waitUntil(t, 90*time.Second, func() bool {
		return slices.Contains(notesIDs(phone), accountNotes.ID)
	}, "the phone never adopted the account's Notes")
	time.Sleep(5 * time.Second)
	if got := notesIDs(phone); len(got) != 1 {
		t.Errorf("the phone holds %d Notes groups %v, want only %s", len(got), got, accountNotes.ID)
	}
	if id := sameNotesEverywhere(t, 30*time.Second, desk, phone); id != accountNotes.ID {
		t.Errorf("the account settled on %s, not its original Notes %s", id, accountNotes.ID)
	}
}

// The desktop is restored from a backup taken before it had a Notes (an
// install from before Notes existed from the start), after a phone was linked
// and adopted the Notes the link created. The restored desktop mints a Notes
// at startup, because nothing on it says the account has one. Both devices
// must still agree on one, and no note may be lost.
func TestDesktopRestoredFromBackupWithoutNotesConvergesOnOne(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)

	deskDir, backup := t.TempDir(), t.TempDir()
	desk := startServiceOn(t, ctx, deskDir, boot)
	if _, err := desk.CreateGuild("Shared"); err != nil {
		t.Fatal(err)
	}
	// S1: no Notes on this desktop yet.
	if n, ok := desk.findNotesDM(); ok {
		if err := desk.store.DeleteGuild(n.ID); err != nil {
			t.Fatal(err)
		}
	}
	_ = desk.Close()
	copyTree(t, deskDir, backup)

	// S2: the desktop runs again (and gets its Notes), and links a phone.
	desk = startServiceOn(t, ctx, deskDir, boot)
	n1, ok := desk.findNotesDM()
	if !ok {
		t.Fatal("the desktop did not backfill a Notes")
	}
	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	phoneDir := t.TempDir()
	res, err := RedeemLink(ctx, phoneDir, code, "test-pass", "")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := startServiceOn(t, ctx, phoneDir, boot)
	for _, ic := range res.GuildInvites {
		phone.JoinLinkedInvite(ic)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return slices.Contains(notesIDs(phone), n1.ID)
	}, "the phone never adopted the desktop's Notes")
	writeNote(t, phone, "written on the phone before the restore")

	// The desktop is put back to S1.
	_ = desk.Close()
	restoreTree(t, backup, deskDir)
	time.Sleep(1100 * time.Millisecond) // a later creation second than n1
	desk = startServiceOn(t, ctx, deskDir, boot)
	if got := notesIDs(desk); len(got) == 1 && got[0] != n1.ID {
		t.Logf("the restored desktop minted %s", got[0])
	}
	deskNote := writeNote(t, desk, "written on the restored desktop")
	t.Logf("restored desktop wrote into %s (the account's first Notes is %s)", deskNote, n1.ID)

	waitUntil(t, 90*time.Second, func() bool {
		return slices.Equal(notesIDs(desk), notesIDs(phone)) && len(notesIDs(desk)) == 2
	}, "the two devices never came to hold the same Notes groups")
	if id := sameNotesEverywhere(t, 30*time.Second, desk, phone); id != n1.ID {
		t.Errorf("the account settled on %s, not the earliest Notes %s", id, n1.ID)
	}
	for name, svc := range map[string]*Service{"desktop": desk, "phone": phone} {
		if n := shownAsNotes(svc); n != 1 {
			t.Errorf("the %s shows %d chats as Notes, want 1", name, n)
		}
	}
	// No note is lost: each is still on the device that wrote it.
	if !notesOn(phone, "written on the phone before the restore") {
		t.Error("the phone lost its note")
	}
	if !notesOn(desk, "written on the restored desktop") {
		t.Error("the desktop lost its note")
	}
	t.Logf("desk sees the phone's note: %v; phone sees the desk's note: %v",
		notesOn(desk, "written on the phone before the restore"), notesOn(phone, "written on the restored desktop"))
}

// The desktop already had Notes at S1; the phone linked at S2 joined it; the
// desktop is put back to S1, whose group state predates the phone's leaf.
func TestDesktopRestoredFromBackupWithNotesKeepsOne(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)

	deskDir, backup := t.TempDir(), t.TempDir()
	desk := startServiceOn(t, ctx, deskDir, boot)
	n1, ok := desk.findNotesDM()
	if !ok {
		t.Fatal("no Notes")
	}
	writeNote(t, desk, "desk at S1")
	_ = desk.Close()
	copyTree(t, deskDir, backup)

	desk = startServiceOn(t, ctx, deskDir, boot)
	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	phoneDir := t.TempDir()
	res, err := RedeemLink(ctx, phoneDir, code, "test-pass", "")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := startServiceOn(t, ctx, phoneDir, boot)
	for _, ic := range res.GuildInvites {
		phone.JoinLinkedInvite(ic)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return slices.Contains(notesIDs(phone), n1.ID)
	}, "the phone never adopted the desktop's Notes")
	writeNote(t, phone, "phone at S2")

	_ = desk.Close()
	restoreTree(t, backup, deskDir)
	desk = startServiceOn(t, ctx, deskDir, boot)
	time.Sleep(10 * time.Second)
	writeNote(t, desk, "desk after restore")
	writeNote(t, phone, "phone after restore")
	// The restored desktop's tree holds its own leaf only, and its own lost
	// commit added the phone, so it cannot follow the phone's log. It must
	// meet the phone, see it ahead, and be re-added by it: both devices end at
	// one epoch, and every note reaches both.
	waitUntil(t, 240*time.Second, func() bool {
		return notesOn(desk, "phone at S2") && notesOn(desk, "phone after restore") &&
			notesOn(phone, "desk after restore")
	}, "the restored desktop and the phone never exchanged their notes")
	waitUntil(t, 60*time.Second, func() bool {
		de, err1 := desk.mls.Epoch(desk.ctx, rhGroup(desk, n1.ID))
		pe, err2 := phone.mls.Epoch(phone.ctx, rhGroup(phone, n1.ID))
		return err1 == nil && err2 == nil && de == pe
	}, "the restored desktop is not at the phone's epoch")
	if desk.OutOfSync(n1.ID) || desk.behindLatched(n1.ID) {
		t.Error("the desktop caught up but still flies the banner or holds the latch")
	}
	if len(notesIDs(desk)) != 1 || len(notesIDs(phone)) != 1 {
		t.Errorf("more than one Notes: desk %v phone %v", notesIDs(desk), notesIDs(phone))
	}
	for c, svc := range map[string]*Service{"desk at S1": desk, "phone at S2": phone, "desk after restore": desk, "phone after restore": phone} {
		if !notesOn(svc, c) {
			t.Errorf("the note %q is gone from the device that wrote it", c)
		}
	}
}

// Which of two Notes groups is THE Notes is decided by the groups alone: the
// one created first, then the smaller id. Asked many times, from a map that
// iterates in a different order each time, the answer never changes, and the
// other is a copy.
func TestCanonicalNotesIsTheEarliest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	first, ok := s.findNotesDM()
	if !ok {
		t.Fatal("no Notes")
	}
	// A later copy, as a second device's mint would arrive.
	later := domain.NewGuild(notesGuildName, []byte("other-group"), s.PublicKey())
	later.Kind = "dm"
	later.Channels[0].Name = "notes"
	later.Created = first.Created.Add(time.Hour)
	// And one created in the same nanosecond, to exercise the id tie-break.
	tie := domain.NewGuild(notesGuildName, []byte("tie-group"), s.PublicKey())
	tie.Kind = "dm"
	tie.Channels[0].Name = "notes"
	tie.Created = first.Created
	s.mu.Lock()
	s.guilds[later.ID] = &later
	s.guilds[tie.ID] = &tie
	s.mu.Unlock()

	want := first.ID
	if tie.ID < want {
		want = tie.ID
	}
	for range 200 {
		g, ok := s.findNotesDM()
		if !ok || g.ID != want {
			t.Fatalf("the canonical Notes is %s, want %s", g.ID, want)
		}
	}
	for _, id := range []string{first.ID, later.ID, tie.ID} {
		if got := s.NotesCopy(id); got != (id != want) {
			t.Errorf("NotesCopy(%s) = %v", id, got)
		}
	}
	if n := shownAsNotes(s); n != 1 {
		t.Errorf("%d groups shown as Notes, want 1", n)
	}
}

// A linked device holding no Notes, which has heard nothing from its sibling
// devices yet, does not mint one when Notes is opened: the account's is on
// its way. Once a sibling has offered its groups and none was a Notes, it may.
func TestLinkedDeviceWaitsForTheAccountsNotes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	if n, ok := s.findNotesDM(); ok {
		s.mu.Lock()
		delete(s.guilds, n.ID)
		s.mu.Unlock()
	}
	s.linkedDevice = true
	if _, err := s.NotesDM(); !errors.Is(err, ErrNotesArriving) {
		t.Fatalf("NotesDM on a linked device that has heard from no sibling: err=%v, want ErrNotesArriving", err)
	}
	if ids := notesIDs(s); len(ids) != 0 {
		t.Fatalf("it minted a Notes anyway: %v", ids)
	}
	s.joiningMu.Lock()
	s.siblingOffered = true
	s.joiningMu.Unlock()
	if _, err := s.NotesDM(); err != nil {
		t.Fatalf("NotesDM after a sibling offered no Notes: %v", err)
	}
	if ids := notesIDs(s); len(ids) != 1 {
		t.Fatalf("want one Notes, have %v", ids)
	}
}
