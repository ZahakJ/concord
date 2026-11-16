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

// The clear of deleted bodies lets their blobs go whether the index backfill
// reached the rows before the clear or not: each body's blobs are noted under
// its row as it is erased, and released once the index is complete. A blob a
// live row also names stays, and only a kept blob is served.
func TestTheClearOfDeletedBodiesTakesTheirBlobs(t *testing.T) {
	for _, indexFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("indexFirst=%v", indexFirst), func(t *testing.T) {
			s, _ := openTestStore(t)
			alone, shared := blobID('a'), blobID('b')
			holdBlob(t, s, alone)
			holdBlob(t, s, shared)
			gone := saveBody(t, s, "deleted "+imageToken(alone)+" "+imageToken(shared))
			live := saveBody(t, s, "live "+imageToken(shared))
			// Rows from before the index: stored with no references.
			if _, err := s.db.Exec(`DELETE FROM blob_refs`); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := s.MarkDeleted(gone.ID, nil, true); err != nil || !ok {
				t.Fatalf("MarkDeleted: %v %v", ok, err)
			}
			backfill := func() {
				t.Helper()
				for i := 0; i < 20; i++ {
					if done, err := s.BackfillBlobRefs(); err != nil {
						t.Fatal(err)
					} else if done {
						return
					}
				}
				t.Fatal("the backfill never finished")
			}

			if indexFirst {
				backfill()
			}
			if n, err := s.ClearDeletedBodies(nil); err != nil || n != 1 {
				t.Fatalf("ClearDeletedBodies cleared %d (err %v), want 1", n, err)
			}
			if body, _ := s.MessageContent(gone.ID); body != "" {
				t.Fatalf("the deleted body was kept: %q", body)
			}
			if !indexFirst {
				if _, err := s.ReleaseErasedBlobs(); err != ErrBlobRefsIncomplete {
					t.Errorf("ReleaseErasedBlobs before the index is complete: %v, want ErrBlobRefsIncomplete", err)
				}
				if !heldBlob(s, shared) {
					t.Error("a blob went before the index was complete")
				}
				backfill()
			}
			banned, err := s.ReleaseErasedBlobs()
			if err != nil {
				t.Fatal(err)
			}
			if heldBlob(s, alone) || !s.BlobBanned(alone) || s.BlobServable(alone) {
				t.Errorf("an erased body's blob was kept (banned %v)", banned)
			}
			if !heldBlob(s, shared) || s.BlobBanned(shared) || !s.BlobServable(shared) {
				t.Errorf("a blob the live row %s names went with the erased one (banned %v)", live.ID, banned)
			}
			if again, err := s.ReleaseErasedBlobs(); err != nil || len(again) != 0 {
				t.Errorf("a second release banned %v (%v)", again, err)
			}
		})
	}
}
