package app

import (
	"testing"
	"time"
)

// A read mark stamped from a clock running hours fast used to be stored as is,
// and since a mark only moves forward it made everything said in the channel
// for those hours read on every device. These pin the cap at every way in.

func TestMarkReadCapsAFutureMark(t *testing.T) {
	s, _ := inboxService(t)
	now := time.Now()
	got, err := s.MarkRead("ch", now.Add(6*time.Hour).UnixMilli())
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if lim := now.Add(readSkew + time.Second).UnixMilli(); got > lim {
		t.Fatalf("a mark six hours ahead was kept: %v ahead of now", time.Duration(got-now.UnixMilli())*time.Millisecond)
	}
	// A message said a minute from now is still unread afterwards.
	saveMention(t, s, "ch", "later", now.Add(readSkew+time.Minute))
	if page := inboxOf(t, s); page.Unread != 1 {
		t.Fatalf("a message past the capped mark must be unread, got %d", page.Unread)
	}
	// And the answer is what the channel holds, so a caller asking for less
	// than is already read learns the real mark.
	if again, _ := s.MarkRead("ch", now.Add(-time.Hour).UnixMilli()); again != got {
		t.Fatalf("an older mark must not move it back: got %d, want %d", again, got)
	}
}

func TestRemoteReadMarkerIsCapped(t *testing.T) {
	s, _ := inboxService(t)
	now := time.Now()
	s.applyRemoteReadMarker("ch", now.Add(6*time.Hour).UnixMilli())
	at, _ := s.store.ReadMark("ch")
	if at > now.Add(readSkew+time.Second).UnixMilli() {
		t.Fatal("a future marker from another device was stored as is")
	}
}

func TestStoredFutureMarksAreRepairedAtStart(t *testing.T) {
	s, _ := inboxService(t)
	now := time.Now()
	future := now.Add(6 * time.Hour).UnixMilli()
	ok := now.Add(-time.Hour).UnixMilli()
	if _, err := s.store.AdvanceReadState("poisoned", future); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.AdvanceReadState("fine", ok); err != nil {
		t.Fatal(err)
	}
	capStoredReadMarks(s.store, now)
	if at, _ := s.store.ReadMark("poisoned"); at != now.UnixMilli() {
		t.Fatalf("a mark six hours ahead must come back to now, got %d", at-now.UnixMilli())
	}
	if at, _ := s.store.ReadMark("fine"); at != ok {
		t.Fatal("an ordinary mark must not move")
	}
}

func TestMarkReadLatestUsesTheNewestMessage(t *testing.T) {
	s, _ := inboxService(t)
	newest := time.Now().Add(-10 * time.Minute).Truncate(time.Millisecond)
	saveMention(t, s, "ch", "a", newest.Add(-time.Hour))
	saveMention(t, s, "ch", "b", newest)
	got, err := s.MarkReadLatest([]string{"ch", "empty"})
	if err != nil {
		t.Fatalf("MarkReadLatest: %v", err)
	}
	if got["ch"] != newest.UnixMilli() {
		t.Fatalf("mark %d, want the newest message %d", got["ch"], newest.UnixMilli())
	}
	if _, ok := got["empty"]; ok {
		t.Fatal("a channel with no messages has nothing to mark through")
	}
}
