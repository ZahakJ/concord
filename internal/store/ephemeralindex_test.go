package store

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// The sweeper runs every 15 seconds on the connection every UI call shares, so
// "which rows are due?" has to be answered from an index, never by walking the
// whole messages table.
func TestDueEphemeralUsesAnIndex(t *testing.T) {
	st := sidebarStore(t)
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN
		SELECT id, channel_id FROM messages WHERE expires > 0 AND expires <= ? AND deleted = 0`, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	got := strings.Join(plan, " | ")
	if !strings.Contains(got, "idx_messages_expires") || strings.Contains(got, "SCAN messages") {
		t.Fatalf("due-row lookup does not use the expiry index: %s", got)
	}
}

// The index must not change the answer: due rows are found, rows with a
// later deadline or none are not, and an erased row is not due again.
func TestDueEphemeralFindsOnlyDueRows(t *testing.T) {
	st := sidebarStore(t)
	now := time.Now()
	tok := func(at time.Time) string { return fmt.Sprintf("hi [eph](concord://eph/v1/%d)", at.Unix()) }
	msgs := []domain.Message{
		{ID: "due", ChannelID: "c", Content: tok(now.Add(-time.Minute))},
		{ID: "later", ChannelID: "c", Content: tok(now.Add(time.Hour))},
		{ID: "plain", ChannelID: "c", Content: "nothing to see"},
	}
	for i, m := range msgs {
		m.Sender = []byte("s")
		m.Sent = now.Add(time.Duration(i) * time.Second)
		if _, err := st.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	due, err := st.DueEphemeral(now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != "due" {
		t.Fatalf("due = %+v, want just the expired row", due)
	}
}

// BenchmarkDueEphemeral is the sweeper's tick on a 5k-message store with a
// handful of disappearing messages.
func BenchmarkDueEphemeral(b *testing.B) {
	st := sidebarStore(b)
	now := time.Now()
	for i := 0; i < 5000; i++ {
		c := "an ordinary message with a bit of length to it, like most are"
		if i%500 == 0 {
			c = fmt.Sprintf("bye [eph](concord://eph/v1/%d)", now.Add(time.Hour).Unix())
		}
		if _, err := st.SaveMessage(domain.Message{ID: fmt.Sprintf("m%d", i), ChannelID: fmt.Sprintf("c%d", i%15),
			Sender: []byte("s"), Content: c, Sent: now.Add(time.Duration(i) * time.Millisecond)}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.DueEphemeral(now.Unix()); err != nil {
			b.Fatal(err)
		}
	}
}

// The deadline backfill of an upgraded store runs after Open, a chunk at a
// time, and still finds every disappearing message written before the column
// existed; a row that already carries its own deadline is left alone.
func TestExpiresBackfillRunsInChunksAfterOpen(t *testing.T) {
	st, path := openTestStore(t)
	now := time.Now()
	for i := 0; i < 7; i++ {
		c := "plain"
		if i%2 == 0 {
			c = fmt.Sprintf("bye [eph](concord://eph/v1/%d)", now.Add(time.Duration(i+1)*time.Hour).Unix())
		}
		if _, err := st.SaveMessage(domain.Message{ID: fmt.Sprintf("m%d", i), ChannelID: "c", Sender: []byte("s"), Content: c, Sent: now.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	// As written by a build that could not see deadlines: none recorded.
	if _, err := st.db.Exec(`UPDATE messages SET expires = 0`); err != nil {
		t.Fatal(err)
	}
	// One row stored since carries its own, which the backfill must keep.
	if _, err := st.db.Exec(`UPDATE messages SET expires = 42 WHERE id = 'm6'`); err != nil {
		t.Fatal(err)
	}
	from := int64(0)
	for steps := 0; ; steps++ {
		next, done, err := st.backfillExpiresChunk(from, 2)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if steps > 10 {
			t.Fatal("backfill does not advance")
		}
		from = next
	}
	want := map[string]bool{"m0": true, "m2": true, "m4": true}
	rows, err := st.db.Query(`SELECT id, expires FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		var at int64
		_ = rows.Scan(&id, &at)
		switch {
		case id == "m6":
			if at != 42 {
				t.Errorf("m6 expires = %d, its own deadline was overwritten", at)
			}
		case want[id] && at == 0:
			t.Errorf("%s has no deadline after the backfill", id)
		case !want[id] && at != 0:
			t.Errorf("%s got a deadline it does not carry", id)
		}
	}
	rows.Close()

	// The marker is what Open resumes from, and the background pass clears it.
	old := expiresBackfillDelay
	expiresBackfillDelay = 0
	defer func() { expiresBackfillDelay = old }()
	if err := st.SetSetting(expiresBackfillKey, "0"); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	st2, err := Open(path, bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v, _ := st2.GetSetting(expiresBackfillKey)
		if v == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the backfill marker was never cleared after Open")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
