package bridge

import (
	"context"
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/domain"
)

// How a Notes group, another copy of it, and an empty copy read in the views.
// The copy is never Notes (not pinned, not what opening Notes opens) and
// never a person's DM; only a copy that holds notes is listed.
func TestNotesViewOf(t *testing.T) {
	notes := domain.Guild{Kind: "dm", Name: "Notes"}
	peerDM := domain.Guild{Kind: "dm", Name: "Direct message"}
	guild := domain.Guild{Kind: "", Name: "Notes"}
	cases := []struct {
		name        string
		g           domain.Guild
		isCopy, has bool
		want        notesView
	}{
		{"the Notes", notes, false, true, notesView{notes: true}},
		{"the Notes, empty", notes, false, false, notesView{notes: true}},
		{"a copy holding notes", notes, true, true, notesView{copy: true}},
		{"an empty copy", notes, true, false, notesView{copy: true, copyEmpty: true}},
		{"a DM with a person", peerDM, false, true, notesView{}},
		{"a guild named Notes", guild, false, true, notesView{}},
	}
	for _, c := range cases {
		if got := notesViewOf(c.g, c.isCopy, c.has); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
	if notesCopyName == "Notes" {
		t.Error("a copy is named exactly like the Notes")
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
