package store

import (
	"bytes"
	"fmt"
	"testing"
)

// TestChronicleCacheSparesPinned is the eviction contract in one test. Unpinned
// pages are a cache and may go; pinned pages are the archive's home copy on the
// device that imported it, and there is no peer to fetch them back from — so the
// ceiling must be enforced entirely out of the unpinned pile, and must not even
// COUNT the pinned one, or a large pinned archive would silently evict every
// cached page on the machine that owns it.
func TestChronicleCacheSparesPinned(t *testing.T) {
	s, _ := openTestStore(t)
	page := bytes.Repeat([]byte{9}, 4096)

	// A pinned page far bigger than the ceiling we are about to set.
	if err := s.SaveChronicleChunk("pinned-1", bytes.Repeat([]byte{1}, 40960), true); err != nil {
		t.Fatalf("SaveChronicleChunk(pinned): %v", err)
	}
	// Ten unpinned pages, saved oldest-first so last_used orders them.
	for i := 0; i < 10; i++ {
		if err := s.SaveChronicleChunk(fmt.Sprintf("free-%d", i), page, false); err != nil {
			t.Fatalf("SaveChronicleChunk: %v", err)
		}
	}
	// Touch the newest so it is unambiguously the most recently used.
	if _, ok, err := s.ChronicleChunk("free-9"); err != nil || !ok {
		t.Fatalf("ChronicleChunk(free-9): %v ok=%v", err, ok)
	}

	// Room for two unpinned pages. The pinned 40 KiB must not consume any of it.
	s.SetChronicleCap(2 * 4096)

	if _, ok, err := s.ChronicleChunk("pinned-1"); err != nil || !ok {
		t.Fatal("the pinned page was evicted; a pinned archive has no second source")
	}
	present, bytesHeld, pinned, err := s.ChronicleChunkPresence([]string{
		"pinned-1", "free-0", "free-1", "free-2", "free-3", "free-4",
		"free-5", "free-6", "free-7", "free-8", "free-9",
	})
	if err != nil {
		t.Fatalf("ChronicleChunkPresence: %v", err)
	}
	if pinned != 1 {
		t.Fatalf("pinned count = %d, want 1", pinned)
	}
	// The pinned page plus at most two unpinned survivors.
	if len(present) > 3 {
		t.Fatalf("%d pages survived a 2-page ceiling: %v", len(present), present)
	}
	if !present["free-9"] {
		t.Fatal("the most recently used page was evicted before older ones")
	}
	if bytesHeld <= 40960 {
		t.Fatalf("held %d bytes, expected the 40 KiB pinned page plus survivors", bytesHeld)
	}

	// Unpinning hands the big page to the cache, where the same ceiling now
	// does apply to it.
	if err := s.PinChronicleChunks([]string{"pinned-1"}, false); err != nil {
		t.Fatalf("PinChronicleChunks(false): %v", err)
	}
	if _, ok, _ := s.ChronicleChunk("pinned-1"); ok {
		t.Fatal("an unpinned 40 KiB page survived a 8 KiB ceiling")
	}
}

// TestAttachmentPinSurvivesEviction covers the same rule on the attachment
// table, which chronicle-referenced pictures rely on: the importing device is
// the only holder of an archived image, so its blob must escape the LRU that
// exists for images every member can re-fetch from every other member.
func TestAttachmentPinSurvivesEviction(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.SaveAttachment("keep-me", bytes.Repeat([]byte{3}, 512)); err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
	if err := s.PinAttachment("keep-me", true); err != nil {
		t.Fatalf("PinAttachment: %v", err)
	}
	// Eviction runs on every save and is a no-op far below the 1 GiB ceiling, so
	// what this asserts is the query shape rather than a real overflow: a pinned
	// row must not be a candidate at all.
	var candidates int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM attachments WHERE pinned = 0 AND blob_id = 'keep-me'`).Scan(&candidates); err != nil {
		t.Fatalf("count: %v", err)
	}
	if candidates != 0 {
		t.Fatal("a pinned attachment is still an eviction candidate")
	}
	if err := s.PinAttachment("keep-me", false); err != nil {
		t.Fatalf("PinAttachment(false): %v", err)
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM attachments WHERE pinned = 0 AND blob_id = 'keep-me'`).Scan(&candidates); err != nil {
		t.Fatalf("count: %v", err)
	}
	if candidates != 1 {
		t.Fatal("unpinning did not return the blob to the cache")
	}
}

// TestChronicleManifestIsStoredVerbatim pins the raw-bytes rule at the storage
// layer: a manifest is signed over its exact bytes, so anything that re-encodes
// it on the way in or out breaks every signature that follows.
func TestChronicleManifestIsStoredVerbatim(t *testing.T) {
	s, _ := openTestStore(t)
	// Deliberately not what Go's json package would emit: odd spacing and a
	// field this build has never heard of.
	raw := []byte(`{"v":1,  "guildId":"g1","fromTheFuture":{"x":[1,2,3]},"sig":"AA=="}`)

	inserted, err := s.SaveChronicleManifest("chr-1", "g1", raw)
	if err != nil || !inserted {
		t.Fatalf("SaveChronicleManifest: %v inserted=%v", err, inserted)
	}
	// Idempotent, and it says so.
	inserted, err = s.SaveChronicleManifest("chr-1", "g1", raw)
	if err != nil || inserted {
		t.Fatalf("re-saving the same manifest reported inserted=%v (err %v)", inserted, err)
	}

	rows, err := s.ChronicleManifests("g1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("ChronicleManifests: %v rows=%d", err, len(rows))
	}
	if !bytes.Equal(rows[0].Raw, raw) {
		t.Fatalf("manifest came back re-encoded:\n got %s\nwant %s", rows[0].Raw, raw)
	}
	if rows[0].ChronicleID != "chr-1" || rows[0].GuildID != "g1" {
		t.Fatalf("wrong row identity: %+v", rows[0])
	}
	if got, err := s.ChronicleManifests("g2"); err != nil || len(got) != 0 {
		t.Fatalf("another guild sees %d manifests (err %v)", len(got), err)
	}
}
