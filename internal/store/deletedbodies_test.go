package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// deletedWithBody stores a message in ch and deletes it the way a moderator's
// device does, keeping its body.
func deletedWithBody(t *testing.T, s *Store, id, ch string) {
	t.Helper()
	if _, err := s.SaveMessage(domain.Message{ID: id, ChannelID: ch, Sender: []byte("a"), Content: "body of " + id,
		Sent: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.MarkDeleted(id, nil, true); err != nil || !ok {
		t.Fatalf("MarkDeleted %s: %v %v", id, ok, err)
	}
}

// Deleted rows that hold a body are cleared a batch at a time, outside the
// channels the caller keeps; live rows and kept channels are left alone, and
// a second pass finds nothing.
func TestDeletedBodiesAreClearedInBatchesOutsideKeptChannels(t *testing.T) {
	s, _ := openTestStore(t)
	for i := 0; i < 5; i++ {
		deletedWithBody(t, s, fmt.Sprintf("gone-%d", i), "ch")
	}
	deletedWithBody(t, s, "kept-0", "keep")
	deletedWithBody(t, s, "kept-1", "keep")
	if _, err := s.SaveMessage(domain.Message{ID: "live", ChannelID: "ch", Sender: []byte("a"), Content: "live words",
		Sent: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}

	for i, want := range []int{2, 2, 1, 0} {
		n, err := s.clearDeletedBodiesChunk([]string{"keep"}, 2)
		if err != nil || n != want {
			t.Fatalf("batch %d cleared %d (err %v), want %d", i, n, err, want)
		}
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("gone-%d", i)
		if body, _ := s.MessageContent(id); body != "" {
			t.Errorf("%s kept %q", id, body)
		}
	}
	for _, id := range []string{"kept-0", "kept-1"} {
		if body, _ := s.MessageContent(id); body != "body of "+id {
			t.Errorf("%s in a kept channel lost its body: %q", id, body)
		}
	}
	if body, _ := s.MessageContent("live"); body != "live words" {
		t.Errorf("a live row lost its body: %q", body)
	}
	if n, err := s.ClearDeletedBodies([]string{"keep"}); err != nil || n != 0 {
		t.Errorf("second pass cleared %d (err %v), want 0", n, err)
	}
	if n, err := s.ClearDeletedBodies(nil); err != nil || n != 2 {
		t.Errorf("with nothing kept, cleared %d (err %v), want 2", n, err)
	}
}
