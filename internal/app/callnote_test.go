package app

import (
	"context"
	"testing"
)

// A call record is one of two shapes the call machine writes; anything else
// would be free text sitting in a conversation dressed as a call.
func TestValidCallNote(t *testing.T) {
	for note, want := range map[string]bool{
		"ended:0":        true,
		"ended:125":      true,
		"missed:abc123":  true,
		"ended:-1":       false,
		"ended:":         false,
		"ended:2m":       false,
		"missed:":        false,
		"missed:a b":     false,
		"hello":          false,
		"":               false,
		"Missed call":    false,
		"missed:a\nb":    false,
		"ended:99999999": true,
	} {
		if got := validCallNote(note); got != want {
			t.Errorf("validCallNote(%q) = %v, want %v", note, got, want)
		}
	}
}

// The DM list words a call line from the reader's side. The caller's row used
// to read "You: Missed call" for a call they placed.
func TestCallPreview(t *testing.T) {
	cases := []struct {
		kind, content string
		mine          bool
		want          string
	}{
		{"call-missed", "Missed call", true, "You called — no answer"},
		{"call-missed", "Missed call", false, "Missed call"},
		{"call-missed", "Declined", true, "Call declined"},
		{"call-missed", "Declined", false, "You declined a call"},
		{"call", "ended:45", true, "Call ended · 45s"},
		{"call", "ended:125", true, "Call ended · 2m"},
		{"call", "ended:3900", true, "Call ended · 1h 5m"},
		{"call", "missed:abc", true, "Missed call"},
	}
	for _, c := range cases {
		got, ok := callPreview(c.kind, c.content, c.mine)
		if !ok || got != c.want {
			t.Errorf("callPreview(%q, %q, %v) = %q, %v; want %q", c.kind, c.content, c.mine, got, ok, c.want)
		}
	}
	if _, ok := callPreview("", "hi", true); ok {
		t.Error("an ordinary message is not a call line")
	}
}

// NoteCall stores the record here, as kind "call", and the DM list reads it as
// a call rather than as "You: ended:125".
func TestNoteCallIsLocalAndPreviewed(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a node")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("calls")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID
	if _, err := s.NoteCall(ch, "free text"); err == nil {
		t.Fatal("NoteCall accepted free text")
	}
	if _, err := s.NoteCall("nope", "ended:3"); err == nil {
		t.Fatal("NoteCall accepted an unknown channel")
	}
	if _, err := s.NoteCall(ch, "ended:125"); err != nil {
		t.Fatalf("NoteCall: %v", err)
	}
	if n := countKind(t, s, ch, "call"); n != 1 {
		t.Fatalf("stored %d call records, want 1", n)
	}
	text, mine, _ := s.LastSaid(ch)
	if text != "Call ended · 2m" || mine {
		t.Fatalf("LastSaid = %q, mine=%v", text, mine)
	}
}
