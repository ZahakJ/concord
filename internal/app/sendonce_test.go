package app

import (
	"context"
	"testing"
	"time"
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
