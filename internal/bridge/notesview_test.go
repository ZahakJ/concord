package bridge

import (
	"context"
	"testing"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/domain"
)

// How a Notes group, another copy of it, and an empty copy read in the views.
// The copy is never Notes (not pinned, not what opening Notes opens) and
// never a person's DM; only a copy that holds notes is listed.
func TestNotesViewOf(t *testing.T) {
	made := time.Date(2026, time.November, 12, 9, 30, 0, 0, time.Local)
	notes := domain.Guild{Kind: "dm", Name: "Notes", Created: made}
	peerDM := domain.Guild{Kind: "dm", Name: "Direct message"}
	guild := domain.Guild{Kind: "", Name: "Notes"}
	cases := []struct {
		name        string
		g           domain.Guild
		isNotes     bool
		isCopy, has bool
		want        notesView
	}{
		{"the Notes", notes, true, false, true, notesView{notes: true}},
		{"the Notes, empty", notes, true, false, false, notesView{notes: true}},
		{"a copy holding notes", notes, true, true, true, notesView{copy: true, name: "Notes (copy from Nov 12, 09:30)"}},
		{"an empty copy", notes, true, true, false, notesView{copy: true, copyEmpty: true, name: "Notes (copy from Nov 12, 09:30)"}},
		{"a Notes record on a group with another account in it", notes, false, false, true, notesView{}},
		{"a DM with a person", peerDM, false, false, true, notesView{}},
		{"a guild named Notes", guild, false, false, true, notesView{}},
	}
	for _, c := range cases {
		if got := notesViewOf(c.g, c.isNotes, c.isCopy, c.has); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
	// Two copies made on different days read differently, and neither reads
	// as the Notes.
	other := notes
	other.Created = made.AddDate(0, 0, 2)
	a, b := notesViewOf(notes, true, true, true).name, notesViewOf(other, true, true, true).name
	if a == b || a == "Notes" || b == "Notes" {
		t.Errorf("copies are not told apart: %q and %q", a, b)
	}
	if got := notesCopyName(time.Time{}); got != "Notes (other copy)" {
		t.Errorf("a copy with no creation time is named %q", got)
	}
}

// And through the real view: a fresh account's Notes is the Notes, with no
// copy flags.
func TestNotesViewOfAFreshAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a full node")
	}
	svc, err := appsvc.Start(context.Background(), appsvc.Config{DataDir: t.TempDir(), Passphrase: "notes-view", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	notes, err := svc.NotesDM()
	if err != nil {
		t.Fatal(err)
	}
	br := &Bridge{ctx: context.Background(), svc: svc}
	views, err := br.Guilds()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.ID == notes.ID {
			if !v.DMNotes || v.DMNotesCopy || v.DMNotesCopyEmpty || v.Name != "Notes" {
				t.Fatalf("the Notes reads as %+v", v)
			}
			return
		}
	}
	t.Fatal("the Notes is not listed")
}
