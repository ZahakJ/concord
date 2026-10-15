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

func BenchmarkSyncApplyBatched(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := sidebarStore(b)
		msgs := syncedHistory(b, st, 1000, 1000)
		b.StartTimer()
		got, err := st.UpsertSyncedMessages(msgs, "fp-self", false)
		if err != nil || len(got) != len(msgs) {
			b.Fatalf("applied %d of %d: %v", len(got), len(msgs), err)
		}
	}
}

// The batched applier must do exactly what the per-row one does: the same
// rows reported changed, read back the same way, leaving the same history —
// for new rows, for a trusted source's edits and tombstones of rows we hold,
// for an untrusted source trying the same, and for a payload that repeats
// itself, more than one transaction's worth of rows long.
func TestUpsertSyncedMessagesMatchesOneByOne(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprintf("trusted=%v", trusted), func(t *testing.T) {
			one, batch := sidebarStore(t), sidebarStore(t)
			payload := syncedHistory(t, one, 400, 300)
			syncedHistory(t, batch, 400, 300)
			later := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
			for i := 0; i < 300; i += 4 {
				m := domain.Message{ID: fmt.Sprintf("held-%05d", i), ChannelID: "ch", Sender: []byte("a"),
					Content: fmt.Sprintf("rewritten %d", i), Edited: true, Updated: later,
					Reactions: map[string][]string{"👀": {"fp-q"}}}
				if i%8 == 0 {
					m.Deleted = true
				}
				payload = append(payload, m)
			}
			payload = append(payload, payload[:50]...) // a responder that repeats rows

			want := applyOneByOne(one, payload, "fp-self", trusted)
			got, err := batch.UpsertSyncedMessages(payload, "fp-self", trusted)
			if err != nil {
				t.Fatal(err)
			}
			strip := func(ms []domain.Message) string {
				var s string
				for _, m := range ms {
					s += fmt.Sprintf("%s|%s|%v|%v|%v;", m.ID, m.Content, m.Deleted, m.Edited, m.Reactions)
				}
				return s
			}
			if len(got) != len(want) || strip(got) != strip(want) {
				t.Fatalf("batched reported %d changed rows, one-by-one %d", len(got), len(want))
			}
			a, _ := one.Messages("ch", 0)
			b, _ := batch.Messages("ch", 0)
			if strip(a) != strip(b) {
				t.Fatal("the two appliers left different histories")
			}
			if len(want) < 400 {
				t.Fatalf("only %d rows changed; the fixture is not exercising much", len(want))
			}
		})
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
