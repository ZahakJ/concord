package app

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestDisappearingMessageExpiresWithoutAnyView pins the core sweeper: a message
// stamped to disappear is erased on a device that never loaded the channel, so
// search, the saved list and pins have nothing to give back. The clock is passed
// in, so the five minutes are not waited out.
func TestDisappearingMessageExpiresWithoutAnyView(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("Night Shift")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID
	now := time.Now()
	at := now.Add(5 * time.Minute).Unix()
	m, err := s.SendMessage(ch, fmt.Sprintf("[eph](concord://eph/v1/%d)this message should vanish", at), "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if err := s.BookmarkMessage(m.ID, ch); err != nil {
		t.Fatalf("BookmarkMessage: %v", err)
	}
	if n := s.sweepEphemeral(now.Add(4 * time.Minute)); n != 0 {
		t.Fatalf("swept %d messages a minute before their timer", n)
	}
	if hits, _ := s.SearchMessages("should vanish", 10); len(hits) != 1 {
		t.Fatalf("before the timer search found %d, want 1", len(hits))
	}
	if n := s.sweepEphemeral(now.Add(6 * time.Minute)); n != 1 {
		t.Fatalf("swept %d messages after their timer, want 1", n)
	}
	if hits, _ := s.SearchMessages("should vanish", 10); len(hits) != 0 {
		t.Fatalf("search still returns a disappeared message: %+v", hits)
	}
	msgs, _ := s.Messages(ch, 50)
	for _, x := range msgs {
		if x.ID == m.ID && (!x.Expired || x.Content != "") {
			t.Fatalf("the row survived its timer: expired=%v content=%q", x.Expired, x.Content)
		}
	}
	if s.store.MessageIsBookmarked(m.ID) {
		t.Fatal("a disappeared message is still in the saved list")
	}
	if n := s.sweepEphemeral(now.Add(7 * time.Minute)); n != 0 {
		t.Fatalf("a second sweep erased %d more, want 0", n)
	}
}
