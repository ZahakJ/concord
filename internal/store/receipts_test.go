package store

import (
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func receiptMsgs(t *testing.T, st *Store, ch string, sender []byte, n int) ([]string, time.Time) {
	t.Helper()
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	var ids []string
	for i := 0; i < n; i++ {
		m, err := domain.NewMessage(ch, sender, "hi")
		if err != nil {
			t.Fatal(err)
		}
		m.Sent = base.Add(time.Duration(i) * time.Minute)
		if _, err := st.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	return ids, base
}

func TestReceiptsOnlyAttachToOurMessagesInThatChannel(t *testing.T) {
	st, _ := openTestStore(t)
	me, them := []byte("me-key"), []byte("them-key")
	mine, _ := receiptMsgs(t, st, "dm1", me, 2)
	theirs, _ := receiptMsgs(t, st, "dm1", them, 1)
	other, _ := receiptMsgs(t, st, "dm2", me, 1)

	// Someone else's message, and one in another channel, are ignored.
	changed, err := st.RecordDelivered(me, "dm1", "bob", append(append([]string{}, theirs...), other...), 100)
	if err != nil || len(changed) != 0 {
		t.Fatalf("foreign ids accepted: %v %v", changed, err)
	}
	changed, _ = st.RecordDelivered(me, "dm1", "bob", append(mine, "nope"), 100)
	if len(changed) != 2 {
		t.Fatalf("changed = %v, want both of ours", changed)
	}
	// Repeating is a no-op and keeps the first time.
	changed, _ = st.RecordDelivered(me, "dm1", "bob", mine, 200)
	if len(changed) != 0 {
		t.Fatalf("repeat changed %v", changed)
	}
	rs, _ := st.ReceiptsFor("dm1")
	if len(rs) != 2 || len(rs[mine[0]]) != 1 || rs[mine[0]][0].Delivered != 100 {
		t.Fatalf("receipts = %+v", rs)
	}
}

func TestReadReceiptCoversOnlyTheSpan(t *testing.T) {
	st, _ := openTestStore(t)
	me := []byte("me-key")
	ids, base := receiptMsgs(t, st, "dm1", me, 4)
	at := func(i int) int64 { return base.Add(time.Duration(i) * time.Minute).UnixNano() }
	// Read through message 2, having been through message 0: covers 1 and 2.
	changed, err := st.RecordRead(me, "dm1", "bob", at(0), at(2), 50)
	if err != nil || len(changed) != 2 {
		t.Fatalf("changed = %v, %v", changed, err)
	}
	rs, _ := st.ReceiptsFor("dm1")
	if _, ok := rs[ids[0]]; ok {
		t.Fatal("a message before the span got a receipt")
	}
	if _, ok := rs[ids[3]]; ok {
		t.Fatal("a message after the span got a receipt")
	}
	if r := rs[ids[1]][0]; r.Read != 50 || r.Delivered != 50 {
		t.Fatalf("read implies delivered: %+v", r)
	}
	if err := st.ClearReadReceipts(); err != nil {
		t.Fatal(err)
	}
	rs, _ = st.ReceiptsFor("dm1")
	if r := rs[ids[1]][0]; r.Read != 0 || r.Delivered != 50 {
		t.Fatalf("after clear: %+v", r)
	}
}

func TestReceiptsGoWithTheirMessages(t *testing.T) {
	st, _ := openTestStore(t)
	me := []byte("me-key")
	ids, _ := receiptMsgs(t, st, "dm1", me, 2)
	receiptMsgs(t, st, "dm2", me, 1)
	_, _ = st.RecordDelivered(me, "dm1", "bob", ids, 1)
	if n, err := st.PruneMessagesBefore([]string{"dm1"}, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()); err != nil || n != 2 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	if rs, _ := st.ReceiptsFor("dm1"); len(rs) != 0 {
		t.Fatalf("receipts outlived prune: %v", rs)
	}
	ids2, _ := receiptMsgs(t, st, "dm3", me, 1)
	_, _ = st.RecordDelivered(me, "dm3", "bob", ids2, 1)
	if err := st.DeleteChannel("dm3"); err != nil {
		t.Fatal(err)
	}
	if rs, _ := st.ReceiptsFor("dm3"); len(rs) != 0 {
		t.Fatalf("receipts outlived channel: %v", rs)
	}
}
