package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

func saveMsg(t *testing.T, s *Store, ch string, sender, body string) domain.Message {
	t.Helper()
	m, err := domain.NewMessage(ch, []byte(sender), body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func contents(t *testing.T, s *Store, id string) []string {
	t.Helper()
	vs, err := s.MessageVersions(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, v := range vs {
		out = append(out, v.Content)
	}
	return out
}

func eq(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }

func TestVersionsKeptInOrder(t *testing.T) {
	s, _ := openTestStore(t)
	m := saveMsg(t, s, "c", "alice", "one")
	if got := contents(t, s, m.ID); len(got) != 0 {
		t.Fatalf("unedited message has versions: %v", got)
	}
	for _, b := range []string{"two", "three"} {
		if ok, err := s.UpdateContent(m.ID, []byte("alice"), b, nil, "", false); err != nil || !ok {
			t.Fatalf("edit: %v %v", ok, err)
		}
	}
	if got := contents(t, s, m.ID); !eq(got, []string{"one", "two", "three"}) {
		t.Fatalf("got %v", got)
	}
	// A rejected edit (not the author) leaves no version.
	if ok, _ := s.UpdateContent(m.ID, []byte("mallory"), "forged", nil, "", false); ok {
		t.Fatal("forged edit applied")
	}
	if got := contents(t, s, m.ID); !eq(got, []string{"one", "two", "three"}) {
		t.Fatalf("forged edit left a version: %v", got)
	}
}

func TestVersionsCapKeepsFirstAndLast(t *testing.T) {
	s, _ := openTestStore(t)
	m := saveMsg(t, s, "c", "alice", "v0")
	for i := 1; i <= 40; i++ {
		if _, err := s.UpdateContent(m.ID, []byte("alice"), fmt.Sprintf("v%d", i), nil, "", false); err != nil {
			t.Fatal(err)
		}
	}
	got := contents(t, s, m.ID)
	if len(got) != maxVersions || got[0] != "v0" || got[len(got)-1] != "v40" {
		t.Fatalf("len=%d first=%s last=%s", len(got), got[0], got[len(got)-1])
	}
	if got[1] != "v22" {
		t.Fatalf("oldest middle not dropped first: %v", got)
	}

	// Byte cap: big edits squeeze the middle but never the ends.
	m2 := saveMsg(t, s, "c", "alice", "first")
	big := strings.Repeat("x", 20<<10)
	for i := 0; i < 5; i++ {
		if _, err := s.UpdateContent(m2.ID, []byte("alice"), fmt.Sprintf("%d%s", i, big), nil, "", false); err != nil {
			t.Fatal(err)
		}
	}
	got = contents(t, s, m2.ID)
	if got[0] != "first" || !strings.HasPrefix(got[len(got)-1], "4x") || len(got) > 4 {
		t.Fatalf("byte cap: n=%d first=%s", len(got), got[0])
	}
}

func TestVersionsGoWithDelete(t *testing.T) {
	s, _ := openTestStore(t)
	m := saveMsg(t, s, "c", "alice", "a")
	_, _ = s.UpdateContent(m.ID, []byte("alice"), "b", nil, "", false)
	if _, ok, err := s.MarkDeleted(m.ID, []byte("alice"), false); err != nil || !ok {
		t.Fatal(err)
	}
	if got := contents(t, s, m.ID); len(got) != 0 {
		t.Fatalf("versions survived delete: %v", got)
	}
}

func TestVersionsGoWithPruneAndChannelDelete(t *testing.T) {
	s, _ := openTestStore(t)
	m := saveMsg(t, s, "old", "alice", "a")
	_, _ = s.UpdateContent(m.ID, []byte("alice"), "b", nil, "", false)
	n, err := s.PruneMessagesBefore([]string{"old"}, m.Sent.UnixNano()+1)
	if err != nil || n != 1 {
		t.Fatalf("prune n=%d err=%v", n, err)
	}
	if got := contents(t, s, m.ID); len(got) != 0 {
		t.Fatalf("versions survived prune: %v", got)
	}
	m2 := saveMsg(t, s, "gone", "alice", "a")
	_, _ = s.UpdateContent(m2.ID, []byte("alice"), "b", nil, "", false)
	if err := s.DeleteChannel("gone"); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, s, m2.ID); len(got) != 0 {
		t.Fatalf("versions survived channel delete: %v", got)
	}
}

func TestVersionsSurviveRestart(t *testing.T) {
	s, path := openTestStore(t)
	m := saveMsg(t, s, "c", "alice", "a")
	_, _ = s.UpdateContent(m.ID, []byte("alice"), "b", nil, "", false)
	_ = s.Close()
	s2, err := Open(path, s.key[:])
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := contents(t, s2, m.ID); !eq(got, []string{"a", "b"}) {
		t.Fatalf("after restart: %v", got)
	}
}
