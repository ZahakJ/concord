package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// Sending moves our read mark up to our own message, but never over a message
// from someone else that we have not read: sending is not reading. With one
// waiting, the mark stops short of it and it still counts as unread.
func TestSendingDoesNotMarkUnseenMessagesRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	g, err := s.CreateGuild("unseen")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID

	// Nothing waiting: the mark reaches our own message.
	first, err := s.SendMessage(ch, "first", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if at, _ := s.store.ReadMark(ch); at < first.Sent.UnixMilli() {
		t.Fatalf("mark %d is before our own message %d", at, first.Sent.UnixMilli())
	}

	// Someone else's message arrives, and nobody looks at it.
	them, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	theirs := domain.Message{
		ID: "their-unseen", ChannelID: ch, Sender: them.PublicKey(), Content: "did you see this?",
		Sent: time.Now().Add(-time.Second),
	}
	if !theirs.Sent.After(first.Sent) {
		time.Sleep(time.Until(first.Sent.Add(1500 * time.Millisecond)))
		theirs.Sent = time.Now().Add(-500 * time.Millisecond)
	}
	if _, err := s.store.SaveMessage(theirs); err != nil {
		t.Fatalf("save theirs: %v", err)
	}

	mine, err := s.SendMessage(ch, "unrelated", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	at, _ := s.store.ReadMark(ch)
	if at*int64(time.Millisecond) >= theirs.Sent.UnixNano() {
		t.Fatalf("sending moved the mark to %d, over their unseen message at %d", at, theirs.Sent.UnixMilli())
	}
	if at < first.Sent.UnixMilli() {
		t.Fatalf("the mark went back to %d, before %d", at, first.Sent.UnixMilli())
	}
	counts, err := s.UnreadCounts(map[string]int64{ch: at * int64(time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	if counts[ch] < 1 {
		t.Fatalf("their message no longer counts as unread (count %d)", counts[ch])
	}

	// Once read, the next send moves the mark to our message again.
	if _, err := s.MarkRead(ch, theirs.Sent.UnixMilli()+1); err != nil {
		t.Fatal(err)
	}
	last, err := s.SendMessage(ch, "after reading", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if at, _ := s.store.ReadMark(ch); at < last.Sent.UnixMilli() || at < mine.Sent.UnixMilli() {
		t.Fatalf("mark %d is before our own message %d", at, last.Sent.UnixMilli())
	}
}
