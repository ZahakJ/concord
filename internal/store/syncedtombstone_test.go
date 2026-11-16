package store

import (
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// A deleted row taken from history sync for a message this device does not
// hold is stored as a tombstone with no body, from any serving member.
func TestASyncedTombstoneForAnUnheldRowIsStoredWithoutABody(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		s, _ := openTestStore(t)
		m := domain.Message{ID: "m1", ChannelID: "ch", Sender: []byte("a"), Content: "served words",
			Sent: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Deleted: true}
		m.Updated = m.Sent.Add(time.Minute)
		changed, err := s.UpsertSyncedMessage(m, "fp-self", trusted)
		if err != nil || !changed {
			t.Fatalf("trusted=%v: upsert changed=%v err=%v", trusted, changed, err)
		}
		row, ok, err := s.MessageByID("m1")
		if err != nil || !ok || !row.Deleted {
			t.Fatalf("trusted=%v: row held=%v deleted=%v err=%v", trusted, ok, row.Deleted, err)
		}
		if body, err := s.MessageContent("m1"); err != nil || body != "" {
			t.Errorf("trusted=%v: a synced tombstone kept the body %q (err %v)", trusted, body, err)
		}
	}
}

// A deleted row taken from a trusted history sync for a message this device
// holds live deletes it and takes the body held here with it.
func TestASyncedTombstoneForAHeldRowTakesTheBody(t *testing.T) {
	s, _ := openTestStore(t)
	sent := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.SaveMessage(domain.Message{ID: "m1", ChannelID: "ch", Sender: []byte("a"), Content: "held words", Sent: sent}); err != nil {
		t.Fatal(err)
	}
	tomb := domain.Message{ID: "m1", ChannelID: "ch", Sender: []byte("a"), Sent: sent, Deleted: true, Updated: sent.Add(time.Minute)}
	changed, err := s.UpsertSyncedMessage(tomb, "fp-self", true)
	if err != nil || !changed {
		t.Fatalf("upsert changed=%v err=%v", changed, err)
	}
	row, ok, err := s.MessageByID("m1")
	if err != nil || !ok || !row.Deleted {
		t.Fatalf("row held=%v deleted=%v err=%v", ok, row.Deleted, err)
	}
	if body, err := s.MessageContent("m1"); err != nil || body != "" {
		t.Errorf("the held body survived a synced delete: %q (err %v)", body, err)
	}
}
