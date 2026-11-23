package app

import (
	"context"
	"encoding/base64"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func countByID(t *testing.T, s *Service, channelID, id string) int {
	t.Helper()
	msgs, err := s.Messages(channelID, 500)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	n := 0
	for _, m := range msgs {
		if m.ID == id {
			n++
		}
	}
	return n
}

func countBody(t *testing.T, s *Service, channelID, body string) int {
	t.Helper()
	msgs, err := s.Messages(channelID, 500)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	n := 0
	for _, m := range msgs {
		if m.Content == body {
			n++
		}
	}
	return n
}

// A send repeated with its send id is one message everywhere: the owner's
// report was a link that showed "failed", was retried, and arrived three
// times. Here the same message is attempted again after it went out, published
// a second time (an answer lost after the publish), and attempted once more
// after the receiver went away and came back; sender and receiver each hold
// exactly one.
func TestSendOnceIsOneMessageAcrossRetriesAndReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	alice := startServiceOn(t, ctx, t.TempDir(), boot)
	bobDir := t.TempDir()
	bob := startServiceOn(t, ctx, bobDir, boot)
	g, err := alice.CreateGuild("once")
	if err != nil {
		t.Fatal(err)
	}
	code, err := alice.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 2, alice, bob)
	ch := g.Channels[0].ID
	const body = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	const sid = "3f0c2b1e-send-once"

	first, err := alice.SendMessageOnce(ch, body, "", "", sid)
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return countByID(t, bob, ch, first.ID) == 1 }, "Bob to get the message")

	// Retry: the same send id answers with the message already sent.
	again, err := alice.SendMessageOnce(ch, body, "", "", sid)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("a retry made message %s, the first was %s", again.ID, first.ID)
	}
	// A publish whose answer was lost: the same id goes out a second time.
	if _, err := alice.sendCore(ch, body, "", "", "", "", nil, true, first.ID); err != nil {
		t.Fatal(err)
	}

	// Bob goes away and comes back; the flush on Alice's side tries again.
	_ = bob.Close()
	bob = startServiceOn(t, ctx, bobDir, boot)
	if _, err := alice.SendMessageOnce(ch, body+" ", "", "", sid); err != nil {
		t.Fatal(err)
	}
	// Something else after it, so "nothing more arrived" is observed, not assumed.
	marker, err := alice.SendMessage(ch, "after", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 60*time.Second, func() bool { return countByID(t, bob, ch, marker.ID) == 1 }, "Bob to catch up after his restart")
	time.Sleep(2 * time.Second)

	for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
		if n := countBody(t, s, ch, body); n != 1 {
			t.Fatalf("%s holds the link %d times, want once", name, n)
		}
		if n := countBody(t, s, ch, body+" "); n != 0 {
			t.Fatalf("%s holds the reworded retry: a retry sent a new message", name)
		}
	}

	// The same send id in another channel, or without one, is a new message.
	other, err := alice.CreateChannel(g.ID, "two", "text", "")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := alice.SendMessageOnce(other.ID, body, "", "", sid)
	if err != nil {
		t.Fatal(err)
	}
	if m2.ID == first.ID {
		t.Fatal("a send id named the same message in another channel")
	}
}

// The owner's second report: the disk failed while a message was being sent,
// the composer offered Retry, and the retry put the message up twice on both
// sides. A message that went out is "sent" whatever the store says after it:
// no error reaches the composer, the row is written when the disk recovers,
// and a retry of the same send finds it and publishes nothing.
func TestASaveThatFailsAfterPublishIsStillOneMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	alice := startServiceOn(t, ctx, t.TempDir(), boot)
	bob := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := alice.CreateGuild("disk")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := alice.InviteCode(g.ID)
	if _, err := bob.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 2, alice, bob)
	ch := g.Channels[0].ID
	const body = "sent while the disk was failing"

	var failing atomic.Bool
	failing.Store(true)
	alice.store.InjectSaveFault(func(m domain.Message) error {
		if failing.Load() && m.Content == body {
			return errors.New("disk I/O error (10)")
		}
		return nil
	})
	defer alice.store.InjectSaveFault(nil)

	var shown atomic.Int32
	alice.OnMessage(func(m domain.Message) {
		if m.Content == body {
			shown.Add(1)
		}
	})
	first, err := alice.SendMessageOnce(ch, body, "", "", "disk-1")
	if err != nil {
		t.Fatalf("a message that went out was reported as failed: %v", err)
	}
	if shown.Load() != 1 {
		t.Fatalf("the sent message was shown %d times here, want once", shown.Load())
	}
	waitUntil(t, 30*time.Second, func() bool { return countByID(t, bob, ch, first.ID) == 1 }, "Bob to get the message")

	// The composer retries anyway: nothing new goes out.
	var seen atomic.Int32
	bob.OnMessage(func(m domain.Message) {
		if m.Content == body {
			seen.Add(1)
		}
	})
	again, err := alice.SendMessageOnce(ch, body, "", "", "disk-1")
	if err != nil || again.ID != first.ID {
		t.Fatalf("retry: %v, id %s want %s", err, again.ID, first.ID)
	}

	// The disk recovers; the background save writes the row.
	failing.Store(false)
	waitUntil(t, 10*time.Second, func() bool { return countByID(t, alice, ch, first.ID) == 1 }, "the row to be written once the disk recovered")
	if _, ok := alice.unsavedMessage(first.ID); ok {
		t.Fatal("the message is still marked unsaved")
	}
	// And a retry after that still sends nothing.
	if m, err := alice.SendMessageOnce(ch, body, "", "", "disk-1"); err != nil || m.ID != first.ID {
		t.Fatalf("retry after the save: %v %s", err, m.ID)
	}
	marker, err := alice.SendMessage(ch, "after", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return countByID(t, bob, ch, marker.ID) == 1 }, "the marker")
	time.Sleep(time.Second)
	if n := seen.Load(); n != 0 {
		t.Fatalf("a retry reached Bob %d more times", n)
	}
	for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
		if n := countBody(t, s, ch, body); n != 1 {
			t.Fatalf("%s holds the message %d times, want once", name, n)
		}
	}

	// A picture and a file take a send id too: a retry is the same message,
	// not sealed or sent again.
	pic := "data:image/png;base64," + base64.StdEncoding.EncodeToString(picOf(t, 2048))
	p1, err := alice.SendAttachment(ch, pic, 23, 23, "", false, "", "", "", "", "pic-1")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := alice.SendAttachment(ch, pic, 23, 23, "", false, "", "", "", "", "pic-1")
	if err != nil || p2.ID != p1.ID || p2.Content != p1.Content {
		t.Fatalf("a retried picture: %v, %s vs %s", err, p2.ID, p1.ID)
	}
	f1, err := alice.SendFile(ch, "data:text/plain;base64,aGk=", "hi.txt", "", "", "", "file-1")
	if err != nil {
		t.Fatal(err)
	}
	if f2, err := alice.SendFile(ch, "data:text/plain;base64,aGk=", "hi.txt", "", "", "", "file-1"); err != nil || f2.ID != f1.ID {
		t.Fatalf("a retried file: %v, %s vs %s", err, f2.ID, f1.ID)
	}
	waitUntil(t, 30*time.Second, func() bool { return countByID(t, bob, ch, f1.ID) == 1 }, "Bob to get the file")
	for name, s := range map[string]*Service{"Alice": alice, "Bob": bob} {
		if n := countBody(t, s, ch, p1.Content); n != 1 {
			t.Fatalf("%s holds the picture %d times", name, n)
		}
		if n := countBody(t, s, ch, f1.Content); n != 1 {
			t.Fatalf("%s holds the file %d times", name, n)
		}
	}
}
