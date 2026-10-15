package store

import (
	"fmt"
	"testing"
	"time"
)

// seedUnread writes n messages round-robin over chans channels from four
// senders, with every seventh one a game move (quiet), every eleventh deleted
// and every thirteenth a system row, so each exclusion in the predicate has
// rows to exclude. It returns the per-channel cursors used by the tests: the
// first channel from the beginning, the rest from various points in its life.
func seedUnread(tb testing.TB, st *Store, n, chans int) map[string]int64 {
	tb.Helper()
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	tx, err := st.db.Begin()
	if err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < n; i++ {
		kind := ""
		if i%13 == 0 {
			kind = "system"
		}
		quiet, deleted := 0, 0
		if i%7 == 0 {
			quiet = 1
		}
		if i%11 == 0 {
			deleted = 1
		}
		if _, err := tx.Exec(`INSERT INTO messages (id, channel_id, sender, kind, deleted, quiet, content_enc, nonce, sent) VALUES (?,?,?,?,?,?,?,?,?)`,
			fmt.Sprintf("m%07d", i), fmt.Sprintf("c%03d", i%chans), []byte{byte(i % 4)}, kind, deleted, quiet,
			[]byte("x"), []byte("n"), base.Add(time.Duration(i)*time.Second).UnixNano()); err != nil {
			tb.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatal(err)
	}
	since := map[string]int64{}
	for c := 0; c < chans; c++ {
		// Most channels read up to somewhere near the end; a few never opened.
		var at int64
		if c%9 != 0 {
			at = base.Add(time.Duration(n-(c*37)%(n/2+1)) * time.Second).UnixNano()
		}
		since[fmt.Sprintf("c%03d", c)] = at
	}
	since["no-such-channel"] = 0
	return since
}

// The batched count must be the per-channel count, channel for channel,
// including channels that have nothing and channels the store has never seen.
func TestUnreadCountsMatchesPerChannelCount(t *testing.T) {
	st := sidebarStore(t)
	since := seedUnread(t, st, 6000, 40)
	got, err := st.UnreadCounts(since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(since) {
		t.Fatalf("answered %d channels, asked about %d", len(got), len(since))
	}
	nonzero := 0
	for ch, at := range since {
		var want int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM messages
			WHERE channel_id = ? AND sent > ? AND deleted = 0 AND quiet = 0 AND kind IN ('', 'guest')`, ch, at).Scan(&want); err != nil {
			t.Fatal(err)
		}
		if got[ch] != want {
			t.Errorf("%s: %d, want %d", ch, got[ch], want)
		}
		if want > 0 {
			nonzero++
		}
	}
	if nonzero < 10 {
		t.Fatalf("only %d channels have unread messages; the fixture is not testing much", nonzero)
	}

	// And the breakdown by sender has to sum to it, channel by channel.
	by, err := st.UnreadBySenderAll(since)
	if err != nil {
		t.Fatal(err)
	}
	for ch, n := range got {
		sum := 0
		for _, sc := range by[ch] {
			sum += sc.N
		}
		if sum != n {
			t.Errorf("%s: senders sum to %d, count is %d", ch, sum, n)
		}
		one, err := st.UnreadBySender(ch, since[ch])
		if err != nil {
			t.Fatal(err)
		}
		if len(one) != len(by[ch]) {
			t.Errorf("%s: %d senders batched, %d alone", ch, len(by[ch]), len(one))
		}
	}
}

func BenchmarkUnreadCounts(b *testing.B) {
	st := sidebarStore(b)
	since := seedUnread(b, st, 60000, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.UnreadCounts(since); err != nil {
			b.Fatal(err)
		}
	}
}
