package store

import (
	"testing"
	"time"
)

func TestChatPrefSetAndGet(t *testing.T) {
	s, path := openTestStore(t)
	now := time.Now()
	at := now.UnixMilli()
	if _, ch, err := s.MergeChatPref("c1", ChatPref{Pinned: true, PinKey: 3, PinAt: at, Archived: true, ArchAt: at}, now); err != nil || !ch {
		t.Fatalf("merge: changed=%v err=%v", ch, err)
	}
	m, err := s.ChatPrefs()
	if err != nil || m["c1"] != (ChatPref{Pinned: true, PinKey: 3, PinAt: at, Archived: true, ArchAt: at}) {
		t.Fatalf("got %+v err=%v", m, err)
	}
	// Survives a reopen.
	_ = s.Close()
	s2, err := Open(path, s2key())
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if m, _ := s2.ChatPrefs(); !m["c1"].Pinned || !m["c1"].Archived {
		t.Fatalf("lost across restart: %+v", m)
	}
}

func s2key() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = 0x42
	}
	return k
}

func TestChatPrefLastWriterWinsPerFact(t *testing.T) {
	s, _ := openTestStore(t)
	now := time.Now()
	t0 := now.UnixMilli()
	s.MergeChatPref("c", ChatPref{Pinned: true, PinKey: 1, PinAt: t0 + 10}, now)
	// An older unpin arriving late loses.
	if _, ch, _ := s.MergeChatPref("c", ChatPref{Pinned: false, PinAt: t0 + 5}, now); ch {
		t.Fatal("older unpin changed the record")
	}
	// A newer unpin wins, and an older pin arriving after it loses.
	s.MergeChatPref("c", ChatPref{Pinned: false, PinAt: t0 + 20}, now)
	s.MergeChatPref("c", ChatPref{Pinned: true, PinKey: 9, PinAt: t0 + 15}, now)
	m, _ := s.ChatPrefs()
	if m["c"].Pinned || m["c"].PinAt != t0+20 {
		t.Fatalf("unpin did not stick: %+v", m["c"])
	}
	// Archive and pin are independent facts.
	s.MergeChatPref("c", ChatPref{Archived: true, ArchAt: t0 + 1}, now)
	m, _ = s.ChatPrefs()
	if !m["c"].Archived || m["c"].PinAt != t0+20 {
		t.Fatalf("facts not independent: %+v", m["c"])
	}
	// Equal stamps converge regardless of arrival order.
	a, _ := openTestStore(t)
	b, _ := openTestStore(t)
	x := ChatPref{Pinned: true, PinKey: 1, PinAt: t0}
	y := ChatPref{Pinned: false, PinAt: t0}
	a.MergeChatPref("c", x, now)
	a.MergeChatPref("c", y, now)
	b.MergeChatPref("c", y, now)
	b.MergeChatPref("c", x, now)
	am, _ := a.ChatPrefs()
	bm, _ := b.ChatPrefs()
	if am["c"] != bm["c"] {
		t.Fatalf("diverged: %+v vs %+v", am["c"], bm["c"])
	}
}

func TestChatPrefSkewIsCapped(t *testing.T) {
	s, _ := openTestStore(t)
	now := time.Now()
	far := now.Add(6 * time.Hour).UnixMilli()
	got, _, _ := s.MergeChatPref("c", ChatPref{Pinned: true, PinAt: far, Archived: true, ArchAt: far}, now)
	lim := now.Add(ChatPrefSkew).UnixMilli()
	if got.PinAt != lim || got.ArchAt != lim {
		t.Fatalf("stamps not capped: %+v want %d", got, lim)
	}
	// A real change a minute later still wins over the capped one.
	later := now.Add(3 * time.Minute)
	got, ch, _ := s.MergeChatPref("c", ChatPref{Pinned: false, PinAt: later.UnixMilli()}, later)
	if !ch || got.Pinned {
		t.Fatalf("future-stamped record blocks later changes: %+v", got)
	}
}

func TestChatPrefPrune(t *testing.T) {
	s, _ := openTestStore(t)
	now := time.Now()
	for _, id := range []string{"a", "b", "c"} {
		s.MergeChatPref(id, ChatPref{Pinned: true, PinAt: now.UnixMilli()}, now)
	}
	gone, err := s.PruneChatPrefs(map[string]bool{"b": true})
	if err != nil || len(gone) != 2 {
		t.Fatalf("pruned %v err=%v", gone, err)
	}
	if m, _ := s.ChatPrefs(); len(m) != 1 || !m["b"].Pinned {
		t.Fatalf("left %+v", m)
	}
}
