package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// seedInboxHistory writes n messages from other people across a handful of
// channels, one in every mentionEvery naming "ada" — the shape of a busy
// account where the inbox is mostly quiet.
func seedInboxHistory(tb testing.TB, s *Service, n, mentionEvery int) {
	tb.Helper()
	other := mustID(tb)
	base := time.Now().Add(-time.Duration(n) * time.Second)
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("message %d about the usual things, nothing for anyone in particular", i)
		if mentionEvery > 0 && i%mentionEvery == 0 {
			body = fmt.Sprintf("@ada have a look at %d", i)
		}
		if _, err := s.store.SaveMessage(domain.Message{
			ID: fmt.Sprintf("m%06d", i), ChannelID: fmt.Sprintf("ch%d", i%12),
			Sender: other.PublicKey(), Name: "rowan", Content: body,
			Sent: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			tb.Fatalf("SaveMessage: %v", err)
		}
	}
}

// BenchmarkInboxRefresh is the call the frontend makes after every arriving
// message: newest page, all entries, the user's alert words.
func BenchmarkInboxRefresh(b *testing.B) {
	s, _ := inboxService(b)
	seedInboxHistory(b, s, 30000, 1000)
	words := []string{"deploy", "outage"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Inbox(words, 0, 50, false); err != nil {
			b.Fatal(err)
		}
	}
}
