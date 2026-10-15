package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// syncedHistory is what one channel's catch-up looks like on the wire: n
// messages, a third of them carrying reactions, landing in a channel that
// already holds `held` messages with reactions of their own.
func syncedHistory(tb testing.TB, st *Store, n, held int) []domain.Message {
	tb.Helper()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < held; i++ {
		id := fmt.Sprintf("held-%05d", i)
		if _, err := st.SaveMessage(domain.Message{ID: id, ChannelID: "ch", Sender: []byte("a"), Content: "old", Sent: base.Add(time.Duration(i) * time.Second)}); err != nil {
			tb.Fatal(err)
		}
		if _, err := st.ToggleReaction(id, "fp-other", "👍"); err != nil {
			tb.Fatal(err)
		}
	}
	out := make([]domain.Message, 0, n)
	for i := 0; i < n; i++ {
		m := domain.Message{ID: fmt.Sprintf("sync-%05d", i), ChannelID: "ch", Sender: []byte("b"), Name: "bea",
			Content: fmt.Sprintf("caught up %d", i), Sent: base.Add(time.Duration(held+i) * time.Second)}
		if i%3 == 0 {
			m.Reactions = map[string][]string{"🎉": {"fp-x", "fp-y"}}
			m.Updated = m.Sent
		}
		out = append(out, m)
	}
	return out
}

// applyOneByOne is what the sync applier does with a channel's rows today.
func applyOneByOne(st *Store, msgs []domain.Message, self string, trusted bool) []domain.Message {
	var out []domain.Message
	for _, m := range msgs {
		changed, err := st.UpsertSyncedMessage(m, self, trusted)
		if err != nil || !changed {
			continue
		}
		if full, ok, err := st.MessageByID(m.ID); err == nil && ok {
			out = append(out, full)
		}
	}
	return out
}

func BenchmarkSyncApplyOneByOne(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := sidebarStore(b)
		msgs := syncedHistory(b, st, 1000, 1000)
		b.StartTimer()
		if got := applyOneByOne(st, msgs, "fp-self", false); len(got) != len(msgs) {
			b.Fatalf("applied %d of %d", len(got), len(msgs))
		}
	}
}

// MessageByID reads one message's reactions rather than the channel's, and
// must hand back exactly what the channel read gives for that message —
// including none at all for a message nobody reacted to.
func TestMessageByIDReactionsMatchTheChannelRead(t *testing.T) {
	st := sidebarStore(t)
	msgs := syncedHistory(t, st, 30, 20)
	applyOneByOne(st, msgs, "fp-self", false)
	if _, err := st.ToggleReaction("held-00003", "fp-z", "🔥"); err != nil {
		t.Fatal(err)
	}
	all, err := st.Messages("ch", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range all {
		got, ok, err := st.MessageByID(want.ID)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", want.ID, ok, err)
		}
		if fmt.Sprint(got.Reactions) != fmt.Sprint(want.Reactions) {
			t.Fatalf("%s: reactions %v, channel read says %v", want.ID, got.Reactions, want.Reactions)
		}
		if (got.Reactions == nil) != (want.Reactions == nil) {
			t.Fatalf("%s: nil-ness differs: %v vs %v", want.ID, got.Reactions, want.Reactions)
		}
	}
}
