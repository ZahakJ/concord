package store

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// The inbox and search scans page through history 256 rows at a time on a
// (sent, id) keyset. Rewriting that condition so the planner can walk the
// index must not change a single row of the answer, and the place it could is
// a run of identical timestamps straddling a page boundary — which imported
// history, stamped to the second, produces in quantity. So this compares the
// scans against the answer worked out the slow way, over a history built
// to have exactly that.
func TestNewestFirstScansMatchTheReferenceOrder(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "c.db"), bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	self, other := []byte("me"), []byte("them")
	type row struct {
		id, body string
		sent     int64
		sender   []byte
		replyTo  string
	}
	var all []row
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	for i := 0; i < 1500; i++ {
		// Three long runs of the same instant, the middle one 600 rows wide —
		// more than two pages — and ordinary distinct times around them.
		sent := base + int64(i)*int64(time.Second)
		switch {
		case i >= 100 && i < 400:
			sent = base + 100*int64(time.Second)
		case i >= 500 && i < 1100:
			sent = base + 500*int64(time.Second)
		case i >= 1200 && i < 1300:
			sent = base + 1200*int64(time.Second)
		}
		r := row{id: fmt.Sprintf("id-%04d", (i*7919)%1500), sent: sent, sender: other, body: fmt.Sprintf("chatter %d", i)}
		switch {
		case i%5 == 0:
			r.body = fmt.Sprintf("hey @ada, item %d", i)
		case i%11 == 0:
			r.sender = self
			r.body = fmt.Sprintf("my own @ada %d", i)
		case i%13 == 0:
			r.replyTo = fmt.Sprintf("id-%04d", ((i-2)*7919)%1500)
		}
		all = append(all, r)
		if _, err := st.SaveMessage(domain.Message{ID: r.id, ChannelID: fmt.Sprintf("c%d", i%3), Sender: r.sender,
			Content: r.body, ReplyTo: r.replyTo, Sent: time.Unix(0, r.sent)}); err != nil {
			t.Fatal(err)
		}
	}
	byID := map[string]row{}
	for _, r := range all {
		byID[r.id] = r
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].sent != all[j].sent {
			return all[i].sent > all[j].sent
		}
		return all[i].id < all[j].id
	})

	t.Run("inbox", func(t *testing.T) {
		var want []string
		for _, r := range all {
			if bytes.Equal(r.sender, self) {
				continue
			}
			parent, isReply := byID[r.replyTo]
			if strings.Contains(r.body, "@ada") || (isReply && bytes.Equal(parent.sender, self)) {
				want = append(want, r.id)
			}
		}
		for _, limit := range []int{7, 150, 100000} {
			hits, err := st.InboxCandidates(self, []string{"@ada"}, 0, 0, limit)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, h := range hits {
				got = append(got, h.ID)
			}
			w := want
			if len(w) > limit {
				w = w[:limit]
			}
			if strings.Join(got, ",") != strings.Join(w, ",") {
				t.Fatalf("limit %d: got %d hits, want %d; first difference at %d", limit, len(got), len(w), firstDiff(got, w))
			}
		}
	})

	t.Run("search", func(t *testing.T) {
		var want []string
		for _, r := range all {
			if strings.Contains(r.body, "chatter") {
				want = append(want, r.id)
			}
		}
		hits, err := st.SearchMessages("chatter", 100000)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, h := range hits {
			got = append(got, h.ID)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("got %d hits, want %d; first difference at %d", len(got), len(want), firstDiff(got, want))
		}
	})
}

func firstDiff(a, b []string) int {
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			return i
		}
	}
	return len(a)
}
