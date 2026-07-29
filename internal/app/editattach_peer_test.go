package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// Editing a meme replaces the message body with a token pointing at a BRAND NEW
// blob. That is fine on the device that made it — it already holds the bytes —
// but the whole point is that the other side sees the change, and the other side
// has never seen that blob. It has to notice the edit and then fetch ciphertext
// it has no copy of, from the editor, off an edited body.
//
// Every check of this feature so far was one node talking to itself in its own
// Notes DM, which cannot exercise any of that. This is the two-peer case.
func TestEditedAttachmentReachesTheOtherPeer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	dataURL := func() string {
		plain := make([]byte, 4096)
		if _, err := rand.Read(plain); err != nil {
			t.Fatal(err)
		}
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(plain)
	}

	// A sends the original meme.
	first, err := a.SendAttachment(channel, dataURL(), 320, 240, "", false, "", "")
	if err != nil {
		t.Fatalf("SendAttachment: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		for _, m := range mustMessages(t, b, channel) {
			if m.ID == first.ID {
				return true
			}
		}
		return false
	}, "B never received the original meme")

	// A edits it: new bytes, new blob, same message.
	newBlob, err := a.EditAttachment(channel, first.ID, dataURL(), 320, 240)
	if err != nil {
		t.Fatalf("EditAttachment: %v", err)
	}
	if newBlob == "" {
		t.Fatal("EditAttachment returned no blob id")
	}

	// B must see the SAME message now carrying the NEW blob — not a second
	// message, and not the original bytes.
	waitUntil(t, 25*time.Second, func() bool {
		for _, m := range mustMessages(t, b, channel) {
			if m.ID == first.ID && strings.Contains(m.Content, newBlob) {
				return true
			}
		}
		return false
	}, "B never saw the edit point at the new blob")

	var count int
	for _, m := range mustMessages(t, b, channel) {
		if strings.Contains(m.Content, "concord://attach/") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("B sees %d attachment messages, want exactly 1 — an edit must not post a second", count)
	}

	// And the real test: B can actually FETCH ciphertext it has never held,
	// from a message body that arrived as an edit.
	keys := ""
	for _, m := range mustMessages(t, b, channel) {
		if m.ID == first.ID {
			for _, tok := range parseAttachTokensForTest(m.Content) {
				keys = tok
			}
		}
	}
	if keys == "" {
		t.Fatal("could not read the edited token back off B's copy")
	}
	if _, err := b.FetchAttachment(channel, newBlob, keys, "png"); err != nil {
		t.Fatalf("B could not fetch the edited attachment's blob: %v", err)
	}
}

// mustMessages is a tiny helper so the assertions above read as intent.
func mustMessages(t *testing.T, s *Service, channelID string) []domainMessage {
	t.Helper()
	msgs, err := s.Messages(channelID, 100)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	out := make([]domainMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, domainMessage{ID: m.ID, Content: m.Content})
	}
	return out
}

type domainMessage struct {
	ID      string
	Content string
}

// parseAttachTokensForTest pulls the keys field out of every image token.
func parseAttachTokensForTest(content string) []string {
	var out []string
	for _, part := range strings.Split(content, "concord://attach/v1/") {
		if !strings.Contains(part, "/") {
			continue
		}
		fields := strings.Split(strings.SplitN(part, ")", 2)[0], "/")
		if len(fields) >= 2 && len(fields[1]) == 75 {
			out = append(out, fields[1])
		}
	}
	return out
}
