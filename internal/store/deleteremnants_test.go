package store

import (
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// remnants counts what a message has beside its row: reactions, receipts,
// this device's bookmark, a hide, earlier versions, and the pin.
func remnants(t *testing.T, s *Store, id string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"reactions", "msg_receipts", "saved_messages", "hidden_messages", "message_versions"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM `+table+` WHERE message_id = ?`, id).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		out[table] = n
	}
	var pinned int
	if err := s.db.QueryRow(`SELECT pinned FROM messages WHERE id = ?`, id).Scan(&pinned); err != nil {
		t.Fatalf("read pinned: %v", err)
	}
	out["pinned"] = pinned
	return out
}

func noRemnants(t *testing.T, s *Store, id, what string) {
	t.Helper()
	for k, n := range remnants(t, s, id) {
		if n != 0 {
			t.Errorf("%s: %s still holds %d", what, k, n)
		}
	}
}

// withRemnants stores a message of author's in ch with a reaction, a delivery
// receipt, a bookmark, an edit and a pin.
func withRemnants(t *testing.T, s *Store, ch string, author []byte) domain.Message {
	t.Helper()
	m, _ := domain.NewMessage(ch, author, "said once")
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if _, err := s.ToggleReaction(m.ID, "fpr-bob", "👍"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}
	if _, err := s.RecordDelivered(author, ch, "acct-bob", []string{m.ID}, time.Now().Unix()); err != nil {
		t.Fatalf("RecordDelivered: %v", err)
	}
	if err := s.BookmarkMessage(m.ID, ch); err != nil {
		t.Fatalf("BookmarkMessage: %v", err)
	}
	if ok, err := s.UpdateContent(m.ID, author, "said twice", nil, "", false); err != nil || !ok {
		t.Fatalf("UpdateContent: %v %v", ok, err)
	}
	if on, err := s.TogglePinned(m.ID); err != nil || !on {
		t.Fatalf("TogglePinned: %v %v", on, err)
	}
	have := remnants(t, s, m.ID)
	for _, k := range []string{"reactions", "msg_receipts", "saved_messages", "message_versions", "pinned"} {
		if have[k] == 0 {
			t.Fatalf("setup: %s not recorded", k)
		}
	}
	return m
}

// A delete for everyone takes the message's reactions, receipts, bookmark,
// versions and pin with it, and a reaction or pin that arrives later is not
// taken.
func TestADeleteTakesWhatTheMessageLeavesBeside(t *testing.T) {
	s, _ := openTestStore(t)
	author := []byte("alice-key")
	m := withRemnants(t, s, "chan-1", author)

	if _, ok, err := s.MarkDeleted(m.ID, author, false); err != nil || !ok {
		t.Fatalf("MarkDeleted: %v %v", ok, err)
	}
	noRemnants(t, s, m.ID, "after a delete")

	if added, err := s.ToggleReaction(m.ID, "fpr-carol", "🔥"); err != nil || added {
		t.Errorf("a reaction was taken on a deleted message: %v %v", added, err)
	}
	if on, err := s.TogglePinned(m.ID); err != nil || on {
		t.Errorf("a deleted message was pinned: %v %v", on, err)
	}
	noRemnants(t, s, m.ID, "after a late reaction and pin")
}

// A message whose disappearing timer ran out keeps nothing beside its row.
func TestAnExpiryTakesWhatTheMessageLeavesBeside(t *testing.T) {
	s, _ := openTestStore(t)
	m := withRemnants(t, s, "chan-1", []byte("alice-key"))

	if _, ok, err := s.MarkDisappeared(m.ID); err != nil || !ok {
		t.Fatalf("MarkDisappeared: %v %v", ok, err)
	}
	noRemnants(t, s, m.ID, "after expiry")
}

// A tombstone taken by history sync takes the same things, and the reactions
// and pin the serving copy still lists are not adopted, on a row held live or
// on one the tombstone inserts.
func TestASyncedTombstoneTakesWhatTheMessageLeavesBeside(t *testing.T) {
	s, _ := openTestStore(t)
	const self = "fpr-self"
	m := withRemnants(t, s, "chan-1", []byte("alice-key"))

	dead := m
	dead.Content = ""
	dead.Deleted = true
	dead.Pinned = true
	dead.Updated = time.Now().Add(time.Hour)
	dead.Reactions = map[string][]string{"👍": {"fpr-bob"}, "🔥": {"fpr-carol"}}
	if changed, err := s.UpsertSyncedMessage(dead, self, true); err != nil || !changed {
		t.Fatalf("tombstone upsert: %v %v", changed, err)
	}
	noRemnants(t, s, m.ID, "after a synced tombstone")

	fresh, _ := domain.NewMessage("chan-1", []byte("alice-key"), "gone before it came")
	fresh.Content = ""
	fresh.Deleted = true
	fresh.Pinned = true
	fresh.Updated = time.Now()
	fresh.Reactions = map[string][]string{"👍": {"fpr-bob"}}
	if changed, err := s.UpsertSyncedMessage(fresh, self, true); err != nil || !changed {
		t.Fatalf("tombstone insert: %v %v", changed, err)
	}
	noRemnants(t, s, fresh.ID, "after a synced tombstone was inserted")
}

// SaveMessage stores a message marked Deleted as a tombstone, with no body
// and no signature, labelled disappeared when it is marked Expired.
func TestSaveMessageStoresATombstoneAsOne(t *testing.T) {
	s, _ := openTestStore(t)
	for _, expired := range []bool{false, true} {
		m, _ := domain.NewMessage("chan-1", []byte("alice-key"), "should not be stored")
		m.Deleted = true
		m.Expired = expired
		m.Sig = []byte("a signature over the words")
		if added, err := s.SaveMessage(m); err != nil || !added {
			t.Fatalf("SaveMessage: %v %v", added, err)
		}
		got, ok, err := s.MessageByID(m.ID)
		if err != nil || !ok {
			t.Fatalf("MessageByID: %v %v", err, ok)
		}
		if !got.Deleted || got.Expired != expired || got.Content != "" || len(got.Sig) != 0 {
			t.Errorf("stored as deleted=%v expired=%v content=%q sig=%d", got.Deleted, got.Expired, got.Content, len(got.Sig))
		}
		if body, _ := s.MessageContent(m.ID); body != "" {
			t.Errorf("a tombstone was stored with a body: %q", body)
		}
	}
}

// Under a retention cutoff, history sync skips a row sent before it that is
// not held, keeps going with the rest of the batch, takes a pinned one (which
// retention keeps), and still reconciles a row already held.
func TestASyncedRowPastRetentionIsSkipped(t *testing.T) {
	s, _ := openTestStore(t)
	const self = "fpr-self"
	cutoff := time.Now().Add(-time.Hour)

	old, _ := domain.NewMessage("chan-1", []byte("alice-key"), "pruned everywhere")
	old.Sent = cutoff.Add(-time.Minute)
	oldPinned, _ := domain.NewMessage("chan-1", []byte("alice-key"), "pinned, so kept")
	oldPinned.Sent = cutoff.Add(-time.Minute)
	oldPinned.Pinned = true
	recent, _ := domain.NewMessage("chan-1", []byte("alice-key"), "inside the window")
	held, _ := domain.NewMessage("chan-1", []byte("alice-key"), "held from before")
	held.Sent = cutoff.Add(-time.Minute)
	if _, err := s.SaveMessage(held); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	heldDead := held
	heldDead.Content = ""
	heldDead.Deleted = true
	heldDead.Updated = time.Now()

	changed, err := s.UpsertSyncedMessagesWith([]domain.Message{old, oldPinned, recent, heldDead}, self,
		SyncRules{Trusted: true, NotBefore: cutoff.UnixNano()})
	if err != nil {
		t.Fatalf("UpsertSyncedMessagesWith: %v", err)
	}
	if len(changed) != 3 {
		t.Errorf("%d rows changed, want 3 (the pinned, the recent and the held tombstone)", len(changed))
	}
	if _, ok, _ := s.MessageByID(old.ID); ok {
		t.Error("a row past the retention cutoff was taken back")
	}
	for _, m := range []domain.Message{oldPinned, recent} {
		if _, ok, _ := s.MessageByID(m.ID); !ok {
			t.Errorf("%q was skipped", m.Content)
		}
	}
	if got, _, _ := s.MessageByID(held.ID); !got.Deleted {
		t.Error("a held row past the cutoff did not take its tombstone")
	}
}

// Past the retention cutoff, the pin a served row claims keeps it only when
// the source is trusted.
func TestASyncedRowPastRetentionIsKeptForItsPinOnlyFromATrustedSource(t *testing.T) {
	s, _ := openTestStore(t)
	const self = "fpr-self"
	cutoff := time.Now().Add(-time.Hour)
	for _, trusted := range []bool{false, true} {
		m, _ := domain.NewMessage("chan-1", []byte("alice-key"), "pinned, says the source")
		m.Sent = cutoff.Add(-time.Minute)
		m.Pinned = true
		if _, err := s.UpsertSyncedMessagesWith([]domain.Message{m}, self,
			SyncRules{Trusted: trusted, NotBefore: cutoff.UnixNano()}); err != nil {
			t.Fatalf("UpsertSyncedMessagesWith: %v", err)
		}
		if _, ok, _ := s.MessageByID(m.ID); ok != trusted {
			t.Errorf("trusted=%v: a pinned row past the cutoff taken=%v, want %v", trusted, ok, trusted)
		}
	}
}
