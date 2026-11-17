package store

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// sealedLeft counts how many of the given sealed bodies still appear, by
// their first 16 bytes (the secretbox tag), in the database file or its log.
func sealedLeft(t *testing.T, path string, sealed [][]byte) int {
	t.Helper()
	want := map[[16]byte]bool{}
	for _, b := range sealed {
		var k [16]byte
		copy(k[:], b)
		want[k] = true
	}
	found := map[[16]byte]bool{}
	for _, f := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(f)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		var k [16]byte
		for i := 0; i+16 <= len(b); i++ {
			copy(k[:], b[i:i+16])
			if want[k] {
				found[k] = true
			}
		}
	}
	return len(found)
}

func walSize(path string) int64 {
	st, err := os.Stat(path + "-wal")
	if err != nil {
		return 0
	}
	return st.Size()
}

// fillForDeletes stores n messages across two channels, sent an hour apart
// from 2020 on, with the log written back, and returns them with their sealed
// bodies as stored.
func fillForDeletes(t *testing.T, s *Store, path string, n int) ([]domain.Message, [][]byte) {
	t.Helper()
	var msgs []domain.Message
	for i := 0; i < n; i++ {
		m, err := domain.NewMessage(fmt.Sprintf("chan-%d", i%2), []byte("bob"), fmt.Sprintf("message %d, an ordinary line of chat that someone will regret", i))
		if err != nil {
			t.Fatal(err)
		}
		m.Sent = time.Unix(1_577_836_800+int64(i)*3600, 0)
		msgs = append(msgs, m)
	}
	if _, err := s.UpsertSyncedMessages(msgs, "self", false); err != nil {
		t.Fatal(err)
	}
	s.CheckpointDeletes()
	sealed := make([][]byte, len(msgs))
	for i, m := range msgs {
		if err := s.db.QueryRow(`SELECT content_enc FROM messages WHERE id = ?`, m.ID).Scan(&sealed[i]); err != nil {
			t.Fatal(err)
		}
	}
	if left := sealedLeft(t, path, sealed); left != n {
		t.Fatalf("found %d of %d sealed bodies before any delete; the scan is wrong", left, n)
	}
	return msgs, sealed
}

func TestAnErasedBodyLeavesNoSealedCopyInTheFile(t *testing.T) {
	s, path := openTestStore(t)
	msgs, sealed := fillForDeletes(t, s, path, 400)
	var gone [][]byte
	for i := 0; i < len(msgs); i += 4 {
		if _, _, err := s.MarkDeleted(msgs[i].ID, nil, true); err != nil {
			t.Fatal(err)
		}
		if err := s.EraseContent(msgs[i].ID); err != nil {
			t.Fatal(err)
		}
		gone = append(gone, sealed[i])
	}
	s.CheckpointDeletes()
	if left := sealedLeft(t, path, gone); left != 0 {
		t.Fatalf("%d of %d erased bodies are still in the database file", left, len(gone))
	}
	var kept [][]byte
	for i := 1; i < len(msgs); i += 4 {
		kept = append(kept, sealed[i])
	}
	if left := sealedLeft(t, path, kept); left != len(kept) {
		t.Fatalf("only %d of %d bodies never deleted are still there", left, len(kept))
	}
}

func TestABatchOfDeletesEmptiesTheLog(t *testing.T) {
	s, path := openTestStore(t)
	msgs, sealed := fillForDeletes(t, s, path, 400)

	// Retention: everything in chan-0 from the first 200 hours.
	n, err := s.PruneMessagesBefore([]string{"chan-0"}, time.Unix(1_577_836_800+200*3600, 0).UnixNano())
	if err != nil || n != 100 {
		t.Fatalf("pruned %d (%v), want 100", n, err)
	}
	if w := walSize(path); w != 0 {
		t.Fatalf("the log holds %d bytes after a retention prune", w)
	}
	var pruned [][]byte
	for i := 0; i < 200; i += 2 {
		pruned = append(pruned, sealed[i])
	}
	if left := sealedLeft(t, path, pruned); left != 0 {
		t.Fatalf("%d of %d pruned bodies are still in the database file", left, len(pruned))
	}

	// Empty trash: deleted guild rows kept their bodies until now.
	var trashed [][]byte
	for i := 201; i < len(msgs); i += 2 {
		if _, _, err := s.MarkDeleted(msgs[i].ID, nil, true); err != nil {
			t.Fatal(err)
		}
		trashed = append(trashed, sealed[i])
	}
	if n, err := s.PurgeDeletedContent(nil); err != nil || n == 0 {
		t.Fatalf("empty trash scrubbed %d (%v)", n, err)
	}
	if w := walSize(path); w != 0 {
		t.Fatalf("the log holds %d bytes after emptying the trash", w)
	}
	if left := sealedLeft(t, path, trashed); left != 0 {
		t.Fatalf("%d of %d bodies emptied from the trash are still in the database file", left, len(trashed))
	}
}

// A body longer than a page lives partly on overflow pages, which a delete
// frees whole. Those are zeroed too: a window from the middle of each sealed
// body, well past its first page, is looked for.
func TestALongErasedBodyLeavesNoSealedCopyInTheFile(t *testing.T) {
	s, path := openTestStore(t)
	long := strings.Repeat("a long pasted log line, kept for a while and then deleted. ", 220) // about 13 KB
	var msgs []domain.Message
	for i := 0; i < 40; i++ {
		m, err := domain.NewMessage("chan-0", []byte("bob"), fmt.Sprintf("%d %s", i, long))
		if err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, m)
	}
	if _, err := s.UpsertSyncedMessages(msgs, "self", false); err != nil {
		t.Fatal(err)
	}
	s.CheckpointDeletes()
	var gone, kept [][]byte
	for i, m := range msgs {
		var enc []byte
		if err := s.db.QueryRow(`SELECT content_enc FROM messages WHERE id = ?`, m.ID).Scan(&enc); err != nil {
			t.Fatal(err)
		}
		if len(enc) < 8016 {
			t.Fatalf("sealed body is %d bytes; the test needs bodies longer than a page", len(enc))
		}
		w := enc[8000:8016]
		if i%2 == 0 {
			gone = append(gone, w)
		} else {
			kept = append(kept, w)
		}
	}
	if left := sealedLeft(t, path, kept); left != len(kept) {
		t.Fatalf("found %d of %d long bodies before any delete; the scan is wrong", left, len(kept))
	}
	for i := 0; i < len(msgs); i += 2 {
		if _, _, err := s.MarkDeleted(msgs[i].ID, nil, true); err != nil {
			t.Fatal(err)
		}
		if err := s.EraseContent(msgs[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	s.CheckpointDeletes()
	if left := sealedLeft(t, path, gone); left != 0 {
		t.Fatalf("%d of %d long erased bodies still have sealed bytes in the file", left, len(gone))
	}
}

// The checkpoint shares the one writer connection. A read in progress must
// not hold it, and every write behind it, for the store's busy timeout.
func TestACheckpointDoesNotWaitOutAnOpenRead(t *testing.T) {
	s, path := openTestStore(t)
	msgs, _ := fillForDeletes(t, s, path, 200)
	if s.rdb == nil {
		t.Fatal("the store has no reader pool")
	}
	tx, err := s.rdb.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, _, err := s.MarkDeleted(msgs[i].ID, nil, true); err != nil {
			t.Fatal(err)
		}
		if err := s.EraseContent(msgs[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Now()
	s.CheckpointDeletes()
	if d := time.Since(t0); d > 250*time.Millisecond {
		t.Fatalf("CheckpointDeletes held the writer for %v with one read open", d)
	}
	_ = tx.Rollback()
	s.CheckpointDeletes()
	if w := walSize(path); w != 0 {
		t.Fatalf("the log holds %d bytes once the read is over", w)
	}
	// The writer keeps its own busy timeout after a checkpoint.
	var bt int
	if err := s.db.QueryRow(`PRAGMA busy_timeout`).Scan(&bt); err != nil || bt != 5000 {
		t.Fatalf("busy_timeout after a checkpoint is %d (%v), want 5000", bt, err)
	}
}

// The one-time clear of deleted bodies at rest writes the log back like any
// other batch of deletes: the cleared bodies are gone from the database file
// and the log is empty afterwards.
func TestClearingDeletedBodiesEmptiesTheLog(t *testing.T) {
	s, path := openTestStore(t)
	msgs, sealed := fillForDeletes(t, s, path, 400)
	var cleared, kept [][]byte
	for i := range msgs {
		if i%3 != 0 {
			kept = append(kept, sealed[i])
			continue
		}
		if _, _, err := s.MarkDeleted(msgs[i].ID, nil, true); err != nil {
			t.Fatal(err)
		}
		cleared = append(cleared, sealed[i])
	}
	s.CheckpointDeletes()
	if left := sealedLeft(t, path, cleared); left != len(cleared) {
		t.Fatalf("found %d of %d deleted bodies before the clear; the scan is wrong", left, len(cleared))
	}
	n, err := s.ClearDeletedBodies(nil)
	if err != nil || n != len(cleared) {
		t.Fatalf("cleared %d (%v), want %d", n, err, len(cleared))
	}
	if w := walSize(path); w != 0 {
		t.Fatalf("the log holds %d bytes after clearing deleted bodies", w)
	}
	if left := sealedLeft(t, path, cleared); left != 0 {
		t.Fatalf("%d of %d cleared bodies are still in the database file or its log", left, len(cleared))
	}
	if left := sealedLeft(t, path, kept); left != len(kept) {
		t.Fatalf("only %d of %d bodies never deleted are still there", left, len(kept))
	}
}

// A checkpoint that gave up because a read was open is tried again once the
// read is over, with no later batch of deletes to bring it: the one-time
// clear of deleted bodies has none.
func TestACheckpointThatGaveUpIsTriedAgain(t *testing.T) {
	first := checkpointRetryFirst
	checkpointRetryFirst = 20 * time.Millisecond
	t.Cleanup(func() { checkpointRetryFirst = first })

	s, path := openTestStore(t)
	msgs, sealed := fillForDeletes(t, s, path, 400)
	var cleared [][]byte
	for i := range msgs {
		if i%2 != 0 {
			continue
		}
		if _, _, err := s.MarkDeleted(msgs[i].ID, nil, true); err != nil {
			t.Fatal(err)
		}
		cleared = append(cleared, sealed[i])
	}
	s.CheckpointDeletes()

	tx, err := s.rdb.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClearDeletedBodies(nil); err != nil || got != len(cleared) {
		t.Fatalf("cleared %d (%v), want %d", got, err, len(cleared))
	}
	if walSize(path) == 0 {
		t.Fatal("the checkpoint finished with a read open; this test needs it to give up")
	}
	_ = tx.Rollback()

	deadline := time.Now().Add(5 * time.Second)
	for walSize(path) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if w := walSize(path); w != 0 {
		t.Fatalf("the log still holds %d bytes after the read ended", w)
	}
	if left := sealedLeft(t, path, cleared); left != 0 {
		t.Fatalf("%d of %d cleared bodies are still in the database file or its log", left, len(cleared))
	}
}
