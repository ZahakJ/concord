package store

import (
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func seqOf(t *testing.T, s *Store, id string) int64 {
	t.Helper()
	var n int64
	if err := s.db.QueryRow(`SELECT seq FROM messages WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("seq of %s: %v", id, err)
	}
	return n
}

func saveAt(t *testing.T, s *Store, id, channel string, sent time.Time) domain.Message {
	t.Helper()
	m := domain.Message{ID: id, ChannelID: channel, Sender: []byte("author-key-32-bytes-long-padding"), Content: id, Sent: sent}
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage %s: %v", id, err)
	}
	return m
}

// TestEveryChangeTakesTheNextNumber: an insert, an edit, a delete and a
// reaction each move a row to the end of the change order, and a number is
// never handed out twice — not even the number of a row that was deleted, or a
// peer already past it would never be offered the row that inherited it.
func TestEveryChangeTakesTheNextNumber(t *testing.T) {
	s, _ := openTestStore(t)
	now := time.Now()
	a := saveAt(t, s, "a", "ch", now.Add(-time.Hour))
	saveAt(t, s, "b", "ch", now)
	if !(seqOf(t, s, "a") < seqOf(t, s, "b")) {
		t.Fatal("inserts are not numbered in order")
	}

	if _, err := s.ToggleReaction("a", "fpr", "👍"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}
	if !(seqOf(t, s, "a") > seqOf(t, s, "b")) {
		t.Fatal("a reaction did not move its message to the end of the change order")
	}
	before := seqOf(t, s, "a")
	if _, err := s.UpdateContent("a", a.Sender, "edited", nil, "", false); err != nil {
		t.Fatalf("UpdateContent: %v", err)
	}
	if seqOf(t, s, "a") <= before {
		t.Fatal("an edit did not take a new number")
	}
	before = seqOf(t, s, "b")
	if _, _, err := s.MarkDeleted("b", a.Sender, false); err != nil {
		t.Fatalf("MarkDeleted: %v", err)
	}
	if seqOf(t, s, "b") <= before {
		t.Fatal("a delete did not take a new number")
	}

	top := seqOf(t, s, "b")
	if _, err := s.db.Exec(`DELETE FROM messages WHERE id = 'b'`); err != nil {
		t.Fatalf("delete row: %v", err)
	}
	saveAt(t, s, "c", "ch", now)
	if got := seqOf(t, s, "c"); got <= top {
		t.Fatalf("a new row took number %d after the row holding %d was removed", got, top)
	}
}

// TestChangePagesResumeAfterTheirLastRow: a page of changes is in change order
// and a reader can resume exactly where it ended, so a page cut short loses
// nothing; the time filter applies only when asked for.
func TestChangePagesResumeAfterTheirLastRow(t *testing.T) {
	s, _ := openTestStore(t)
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		saveAt(t, s, string(rune('a'+i)), "ch", base.Add(time.Duration(4-i)*time.Minute)) // sent in reverse order
	}
	page, err := s.MessagesAfterSeq("ch", 0, 0, 3)
	if err != nil {
		t.Fatalf("MessagesAfterSeq: %v", err)
	}
	if len(page.Msgs) != 3 || !page.Full || page.Msgs[0].ID != "a" || page.Msgs[2].ID != "c" {
		t.Fatalf("first page = %+v", page)
	}
	rest, err := s.MessagesAfterSeq("ch", page.LastRead, 0, 3)
	if err != nil {
		t.Fatalf("MessagesAfterSeq: %v", err)
	}
	if len(rest.Msgs) != 2 || rest.Full || rest.Msgs[0].ID != "d" || rest.Msgs[1].ID != "e" {
		t.Fatalf("second page = %+v", rest)
	}
	// Time-filtered: only the two sent last (a and b, in change order).
	since := base.Add(2*time.Minute + time.Second).UnixNano()
	timed, err := s.MessagesAfterSeq("ch", 0, since, 10)
	if err != nil {
		t.Fatalf("MessagesAfterSeq: %v", err)
	}
	if len(timed.Msgs) != 2 || timed.Msgs[0].ID != "a" || timed.Msgs[1].ID != "b" {
		t.Fatalf("time-filtered page = %+v", timed.Msgs)
	}
}

// TestSyncMarksRiseAndForget: a mark only rises within one numbering, a new
// numbering replaces it, the floor only rises, and deleting the channel takes
// both with its rows — a mark claims we hold what a peer served.
func TestSyncMarksRiseAndForget(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.SetSyncMark("peer", "ch", "space1", 10, 5); err != nil {
		t.Fatalf("SetSyncMark: %v", err)
	}
	_ = s.SetSyncMark("peer", "ch", "space1", 7, 3)
	space, seq, local, err := s.SyncMarks("peer", []string{"ch", "other"})
	if err != nil || space != "space1" || seq["ch"] != 10 || local["ch"] != 5 {
		t.Fatalf("marks after a lower write: %q %v %v %v", space, seq, local, err)
	}
	if _, ok := seq["other"]; ok {
		t.Fatal("a channel with no mark came back with one")
	}
	_ = s.SetSyncMark("peer", "ch", "space2", 2, 1)
	space, seq, _, _ = s.SyncMarks("peer", []string{"ch"})
	if space != "space2" || seq["ch"] != 2 {
		t.Fatalf("a new numbering did not replace the mark: %q %v", space, seq)
	}

	_ = s.RaiseSyncFloor("ch", 100)
	_ = s.RaiseSyncFloor("ch", 50)
	if f, _ := s.SyncFloor("ch"); f != 100 {
		t.Fatalf("floor = %d, want 100", f)
	}
	if err := s.DeleteChannel("ch"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if _, seq, _, _ := s.SyncMarks("peer", []string{"ch"}); len(seq) != 0 {
		t.Fatal("deleting a channel left its marks behind")
	}
	if f, _ := s.SyncFloor("ch"); f != 0 {
		t.Fatal("deleting a channel left its floor behind")
	}
}

// TestHeldDigestsMatchWhatIsServed: the digest a requester computes over its
// own copy of a row must equal the one the responder computes over the row it
// would serve, or nothing is ever skipped; and any change to the row's state
// must change it, or a changed row is withheld.
func TestHeldDigestsMatchWhatIsServed(t *testing.T) {
	s, _ := openTestStore(t)
	saveAt(t, s, "a", "ch", time.Now())
	if _, err := s.ToggleReaction("a", "fpr", "👍"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}
	held, err := s.HeldDigests("ch", 0, 10)
	if err != nil || len(held) != 1 {
		t.Fatalf("HeldDigests: %v %v", held, err)
	}
	page, err := s.MessagesAfterSeq("ch", 0, 0, 10)
	if err != nil || len(page.Msgs) != 1 {
		t.Fatalf("MessagesAfterSeq: %v %v", page, err)
	}
	if RowDigest(page.Msgs[0]) != held[0] {
		t.Fatal("the served row's digest differs from the held copy's")
	}
	if _, err := s.ToggleReaction("a", "fpr2", "👍"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}
	page, _ = s.MessagesAfterSeq("ch", 0, 0, 10)
	if RowDigest(page.Msgs[0]) == held[0] {
		t.Fatal("a new reaction left the digest unchanged")
	}
}

// TestUpgradeNumbersExistingHistory: a database from before the change counter
// opens with every existing row numbered in the order it was written, the
// counter past them, and each channel's floor at its newest send time — where
// the old cursor stood, less the update stamps.
func TestUpgradeNumbersExistingHistory(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = byte(200 - i)
	}
	path := writeOldWorkspace(t, key)
	s, err := Open(path, key[:])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	a, b, c := seqOf(t, s, "m0"), seqOf(t, s, "m1"), seqOf(t, s, "m2")
	if !(0 < a && a < b && b < c) {
		t.Fatalf("existing rows numbered %d, %d, %d", a, b, c)
	}
	var maxSent int64
	if err := s.db.QueryRow(`SELECT MAX(sent) FROM messages WHERE channel_id = 'c-old'`).Scan(&maxSent); err != nil {
		t.Fatalf("max sent: %v", err)
	}
	if f, _ := s.SyncFloor("c-old"); f != maxSent {
		t.Fatalf("upgraded floor = %d, want the newest send time %d", f, maxSent)
	}
	saveAt(t, s, "new", "c-old", time.Now())
	if seqOf(t, s, "new") <= c {
		t.Fatal("the first row after the upgrade did not number past the old ones")
	}
	// And a second open changes nothing.
	_ = s.Close()
	s2, err := Open(path, key[:])
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if got := seqOf(t, s2, "m0"); got != a {
		t.Fatalf("reopening renumbered history: %d -> %d", a, got)
	}
}
