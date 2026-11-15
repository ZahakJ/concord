package store

import (
	"bytes"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func hiddenSave(t *testing.T, s *Store, ch, id, body string, at time.Time) {
	t.Helper()
	m := domain.Message{ID: id, ChannelID: ch, Sender: []byte("alice"), Name: "alice", Content: body, Sent: at}
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
}

func idsOf(ms []domain.Message) string {
	out := ""
	for _, m := range ms {
		out += m.ID + ","
	}
	return out
}

func TestHiddenMessageLeavesEveryRead(t *testing.T) {
	s, _ := openTestStore(t)
	base := time.Now().Add(-time.Hour)
	hiddenSave(t, s, "c1", "m1", "alpha needle", base)
	hiddenSave(t, s, "c1", "m2", "beta needle", base.Add(time.Minute))
	hiddenSave(t, s, "c1", "m3", "gamma needle", base.Add(2*time.Minute))

	if added, err := s.HideMessage("c1", "m3"); err != nil || !added {
		t.Fatalf("HideMessage = %v, %v", added, err)
	}
	if added, _ := s.HideMessage("c1", "m3"); added {
		t.Fatal("hiding twice reported a change")
	}

	msgs, _ := s.Messages("c1", 0)
	if got := idsOf(msgs); got != "m1,m2," {
		t.Fatalf("Messages = %s, want m1,m2,", got)
	}
	before, _ := s.MessagesBefore("c1", base.Add(time.Hour).UnixNano(), 10)
	if got := idsOf(before); got != "m1,m2," {
		t.Fatalf("MessagesBefore = %s", got)
	}
	newest, ok, _ := s.NewestMessage("c1")
	if !ok || newest.ID != "m2" {
		t.Fatalf("NewestMessage = %v %v, want m2 (the last-message preview)", newest.ID, ok)
	}
	counts, _ := s.UnreadCounts(map[string]int64{"c1": 0})
	if counts["c1"] != 2 {
		t.Fatalf("UnreadCounts = %d, want 2", counts["c1"])
	}
	bs, _ := s.UnreadBySender("c1", 0)
	if len(bs) != 1 || bs[0].N != 2 {
		t.Fatalf("UnreadBySender = %+v", bs)
	}
	all, _ := s.UnreadBySenderAll(map[string]int64{"c1": 0})
	if len(all["c1"]) != 1 || all["c1"][0].N != 2 {
		t.Fatalf("UnreadBySenderAll = %+v", all)
	}
	rows, _ := s.UnreadRows("c1", 0, 10)
	if got := idsOf(rows); got != "m1,m2," {
		t.Fatalf("UnreadRows = %s", got)
	}
	found, err := s.SearchMessages("needle", 10)
	if err != nil || idsOf(found) != "m2,m1," {
		t.Fatalf("SearchMessages = %s, %v", idsOf(found), err)
	}
	hits, err := s.InboxCandidates([]byte("someone-else"), []string{"needle"}, 0, 0, 10)
	if err != nil || len(hits) != 2 {
		t.Fatalf("InboxCandidates = %d hits, %v", len(hits), err)
	}

	// The row is still there for sync: peers asking us for history get it.
	if m, ok, _ := s.MessageByID("m3"); !ok || m.Content != "gamma needle" {
		t.Fatalf("hidden row was altered: %+v %v", m, ok)
	}

	// Undo brings it back everywhere.
	if ok, err := s.UnhideMessage("m3"); err != nil || !ok {
		t.Fatalf("UnhideMessage = %v, %v", ok, err)
	}
	msgs, _ = s.Messages("c1", 0)
	if got := idsOf(msgs); got != "m1,m2,m3," {
		t.Fatalf("after unhide Messages = %s", got)
	}
}

func TestHideBeforeTheMessageArrives(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.HideMessage("c1", "late"); err != nil {
		t.Fatal(err)
	}
	hiddenSave(t, s, "c1", "late", "arrives after the hide", time.Now())
	hiddenSave(t, s, "c1", "other", "visible", time.Now().Add(time.Second))
	msgs, _ := s.Messages("c1", 0)
	if got := idsOf(msgs); got != "other," {
		t.Fatalf("Messages = %s, want other,", got)
	}
	if c, _ := s.UnreadCounts(map[string]int64{"c1": 0}); c["c1"] != 1 {
		t.Fatalf("unread = %d, want 1", c["c1"])
	}
}

func TestHiddenSurvivesRestart(t *testing.T) {
	s, path := openTestStore(t)
	hiddenSave(t, s, "c1", "m1", "x", time.Now())
	if _, err := s.HideMessage("c1", "m1"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s2, err := Open(path, bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if !s2.IsHidden("m1") {
		t.Fatal("hide did not survive a restart")
	}
	if msgs, _ := s2.Messages("c1", 0); len(msgs) != 0 {
		t.Fatalf("hidden message back after restart: %s", idsOf(msgs))
	}
	if ids, _ := s2.HiddenIDs("c1"); len(ids) != 1 || ids[0] != "m1" {
		t.Fatalf("HiddenIDs = %v", ids)
	}
}

func TestHiddenRowsArePruned(t *testing.T) {
	s, _ := openTestStore(t)
	old := time.Now().Add(-48 * time.Hour)
	hiddenSave(t, s, "c1", "old", "x", old)
	hiddenSave(t, s, "c1", "new", "y", time.Now())
	hiddenSave(t, s, "c2", "gone", "z", time.Now())
	hiddenSave(t, s, "c3", "tomb", "t", time.Now())
	for _, p := range [][2]string{{"c1", "old"}, {"c1", "new"}, {"c2", "gone"}, {"c3", "tomb"}, {"c1", "never-arrived"}} {
		if _, err := s.HideMessage(p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}

	// Retention prune: the pruned message's hide goes with it.
	if n, err := s.PruneMessagesBefore([]string{"c1"}, time.Now().Add(-time.Hour).UnixNano()); err != nil || n != 1 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	if s.IsHidden("old") {
		t.Fatal("hide row outlived the pruned message")
	}
	// A message deleted for everyone needs no hide.
	if _, ok, err := s.MarkDeleted("tomb", []byte("alice"), false); err != nil || !ok {
		t.Fatalf("MarkDeleted = %v, %v", ok, err)
	}
	if s.IsHidden("tomb") {
		t.Fatal("hide row outlived a delete-for-everyone")
	}
	// Channel deletion.
	if err := s.DeleteChannel("c2"); err != nil {
		t.Fatal(err)
	}
	if s.IsHidden("gone") {
		t.Fatal("hide row outlived its channel")
	}
	// A hide that never met its message is dropped once it is stale, not before.
	if n, _ := s.SweepHidden(time.Now()); n != 0 || !s.IsHidden("never-arrived") {
		t.Fatalf("a fresh orphan hide was swept (n=%d)", n)
	}
	if n, _ := s.SweepHidden(time.Now().Add(hiddenOrphanTTL + time.Hour)); n != 1 || s.IsHidden("never-arrived") {
		t.Fatalf("a stale orphan hide survived (n=%d)", n)
	}
	if !s.IsHidden("new") {
		t.Fatal("sweep dropped the hide of a live message")
	}
}
