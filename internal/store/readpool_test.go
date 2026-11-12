package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func seedChannel(t *testing.T, st *Store, ch string, n int) {
	t.Helper()
	base := time.Now().Add(-time.Hour)
	for i := 0; i < n; i++ {
		if _, err := st.SaveMessage(domain.Message{ID: fmt.Sprintf("%s-%d", ch, i), ChannelID: ch, Sender: []byte("s"),
			Content: fmt.Sprintf("message %d", i), Sent: base.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
}

// A read the UI is waiting for must not queue behind a write. The writer is a
// single connection, and while a burst of incoming messages was being stored
// (each insert holding it for a disk sync) opening a channel waited its turn.
func TestReadIsNotBlockedByALongWrite(t *testing.T) {
	st, _ := openTestStore(t)
	seedChannel(t, st, "c", 50)

	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	// Take the write lock and hold it, as a slow insert would.
	if _, err := tx.Exec(`UPDATE messages SET pinned = pinned WHERE channel_id = 'c'`); err != nil {
		t.Fatal(err)
	}
	held := 800 * time.Millisecond
	released := make(chan struct{})
	go func() {
		time.Sleep(held)
		_ = tx.Commit()
		close(released)
	}()

	start := time.Now()
	msgs, err := st.Messages("c", 200)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 50 {
		t.Fatalf("read %d messages, want 50", len(msgs))
	}
	if took > held/2 {
		t.Fatalf("a read waited %v behind a write held for %v", took, held)
	}
	<-released
}

// What a caller has been told was written, the next read sees.
func TestReadSeesACommittedWrite(t *testing.T) {
	st, _ := openTestStore(t)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("m%d", i)
		if _, err := st.SaveMessage(domain.Message{ID: id, ChannelID: "c", Sender: []byte("s"), Content: id, Sent: time.Now()}); err != nil {
			t.Fatal(err)
		}
		m, ok, err := st.MessageByID(id)
		if err != nil || !ok || m.Content != id {
			t.Fatalf("message %s not visible right after it was saved: ok=%v err=%v", id, ok, err)
		}
	}
}

// Nothing can be written through the read pool.
func TestReadPoolRefusesWrites(t *testing.T) {
	st, _ := openTestStore(t)
	if st.rdb == nil {
		t.Fatal("no read pool for a file-backed store")
	}
	if _, err := st.rdb.Exec(`DELETE FROM messages`); err == nil {
		t.Fatal("the read pool accepted a write")
	}
}
