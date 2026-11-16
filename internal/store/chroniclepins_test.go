package store

import (
	"fmt"
	"testing"
)

// holdSized stores a blob whose recorded size is size, without that many
// bytes: the bounds read the size column.
func holdSized(t *testing.T, s *Store, id string, size int64) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO attachments (blob_id, ct, size, created, last_used) VALUES (?, ?, ?, 1, 1)`,
		id, []byte("ct"), size); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
}

func hexID(n int) string { return fmt.Sprintf("%064x", n) }

func pinnedColumn(t *testing.T, s *Store, id string) bool {
	t.Helper()
	p, ok, err := s.AttachmentPinned(id)
	if err != nil || !ok {
		t.Fatalf("AttachmentPinned(%s…): ok %v err %v", id[:6], ok, err)
	}
	return p
}

// An archive picture held here is pinned and served, up to the guild's count
// bound; past it the picture is held but neither pinned nor served, and
// asking is not an error.
func TestAnArchivePictureIsPinnedUpToTheCountBound(t *testing.T) {
	s := completeStore(t)
	for i := 0; i < ChroniclePinsPerGuild+1; i++ {
		holdSized(t, s, hexID(i), 10)
	}
	for i := 0; i < ChroniclePinsPerGuild; i++ {
		ok, err := s.PinChronicleBlob("g", "chron-1", hexID(i))
		if err != nil || !ok {
			t.Fatalf("pin %d within the bound: %v %v", i, ok, err)
		}
	}
	if !s.BlobServable(hexID(0)) {
		t.Fatal("a pinned archive picture is not served")
	}
	past := hexID(ChroniclePinsPerGuild)
	ok, err := s.PinChronicleBlob("g", "chron-1", past)
	if err != nil {
		t.Fatalf("pin past the bound returned an error: %v", err)
	}
	if ok || pinnedColumn(t, s, past) {
		t.Fatal("a picture past the count bound was pinned")
	}
	if s.BlobServable(past) {
		t.Fatal("a picture past the count bound is served")
	}
	if !heldBlob(s, past) {
		t.Fatal("a picture past the count bound is no longer held")
	}
	if n, _, _ := s.ChroniclePinned("g"); n != ChroniclePinsPerGuild {
		t.Fatalf("guild holds %d pins, want %d", n, ChroniclePinsPerGuild)
	}
	// The bound is per guild: another guild's archive may still pin.
	if ok, err := s.PinChronicleBlob("h", "chron-2", past); err != nil || !ok {
		t.Fatalf("another guild could not pin: %v %v", ok, err)
	}
}

// The byte bounds: one per guild and one for the device.
func TestAnArchivePictureIsPinnedUpToTheByteBounds(t *testing.T) {
	s := completeStore(t)
	const mib = 1 << 20
	big, over := hexID(1), hexID(2)
	holdSized(t, s, big, 100*mib)
	holdSized(t, s, over, 30*mib)
	if ok, err := s.PinChronicleBlob("g", "c", big); err != nil || !ok {
		t.Fatalf("pin of 100 MiB: %v %v", ok, err)
	}
	if ok, err := s.PinChronicleBlob("g", "c", over); err != nil || ok {
		t.Fatalf("pin past the guild's bytes: ok %v err %v", ok, err)
	}
	if pinnedColumn(t, s, over) || s.BlobServable(over) {
		t.Fatal("a picture past the guild's byte bound is pinned or served")
	}
	// Device-wide: four more guilds of 100 MiB reach 500 MiB of the 512; a
	// fifth does not fit, though its own guild bound would allow it.
	for g := 0; g < 5; g++ {
		id := hexID(100 + g)
		holdSized(t, s, id, 100*mib)
		want := g < 4
		ok, err := s.PinChronicleBlob(fmt.Sprintf("guild-%d", g), "c", id)
		if err != nil || ok != want {
			t.Fatalf("guild %d: pinned %v, want %v (err %v)", g, ok, want, err)
		}
	}
	_, gBytes, _ := s.ChroniclePinned("g")
	if gBytes != 100*mib {
		t.Fatalf("guild pin bytes %d", gBytes)
	}
}

// A picture the import pinned is not counted, and taking back the archive's
// pins leaves it pinned.
func TestAnImportsPinIsNotTakenBack(t *testing.T) {
	s := completeStore(t)
	imported := blobID('a')
	holdBlob(t, s, imported)
	if err := s.PinAttachment(imported, true); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.PinChronicleBlob("g", "c", imported); err != nil || !ok {
		t.Fatalf("PinChronicleBlob on an imported picture: %v %v", ok, err)
	}
	if n, _, _ := s.ChroniclePinned("g"); n != 0 {
		t.Fatalf("an imported picture was counted: %d", n)
	}
	if _, err := s.ReleaseChronicleBlobs("g", "c"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnpinOtherChronicles("g", "other"); err != nil {
		t.Fatal(err)
	}
	if !pinnedColumn(t, s, imported) || !s.BlobServable(imported) {
		t.Fatal("an imported picture lost its pin")
	}
}

// Removing the archive takes back its pins and removes and bans each picture
// nothing else keeps, so it is neither served nor stored again. A picture a
// held message names, or another guild's archive pins, stays.
func TestRemovingTheArchiveTakesItsPictures(t *testing.T) {
	s := completeStore(t)
	only, named, shared, otherArchive := blobID('a'), blobID('b'), blobID('c'), blobID('d')
	for _, id := range []string{only, named, shared, otherArchive} {
		holdBlob(t, s, id)
		if ok, err := s.PinChronicleBlob("g", "c1", id); err != nil || !ok {
			t.Fatalf("pin: %v %v", ok, err)
		}
	}
	if ok, err := s.PinChronicleBlob("g", "c2", otherArchive); err != nil || !ok {
		t.Fatalf("pin: %v %v", ok, err)
	}
	if ok, err := s.PinChronicleBlob("h", "c9", shared); err != nil || !ok {
		t.Fatalf("pin: %v %v", ok, err)
	}
	saveBody(t, s, "live "+imageToken(named))

	banned, err := s.ReleaseChronicleBlobs("g", "c1")
	if err != nil {
		t.Fatal(err)
	}
	// otherArchive was re-pinned for c2 in the same guild: the guild's pin
	// follows the archive that named it last, so it is not c1's any more.
	if len(banned) != 1 || banned[0] != only {
		t.Fatalf("banned %v, want only %s…", banned, only[:4])
	}
	if heldBlob(s, only) || !s.BlobBanned(only) || s.BlobServable(only) {
		t.Fatal("the removed archive's picture is still held, unbanned or served")
	}
	if err := s.SaveAttachment(only, []byte("again")); err != nil || heldBlob(s, only) {
		t.Fatal("the removed archive's picture was stored again")
	}
	if !s.BlobServable(named) || pinnedColumn(t, s, named) {
		t.Fatal("a picture a held message names was not kept as a cache entry the message serves")
	}
	if !s.BlobServable(shared) || !pinnedColumn(t, s, shared) {
		t.Fatal("a picture another guild's archive pins lost its pin")
	}
	if !s.BlobServable(otherArchive) {
		t.Fatal("a picture the guild's other archive pins was released")
	}
	if n, _, _ := s.ChroniclePinned("g"); n != 1 {
		t.Fatalf("guild g holds %d pins after the release, want 1", n)
	}
}

// When the guild shows another archive, the pins of the old one go back to
// the cache: held, not banned, not served.
func TestAReplacedArchiveIsUnpinned(t *testing.T) {
	s := completeStore(t)
	old, cur := blobID('a'), blobID('b')
	holdBlob(t, s, old)
	holdBlob(t, s, cur)
	_, _ = s.PinChronicleBlob("g", "old", old)
	_, _ = s.PinChronicleBlob("g", "new", cur)
	if err := s.UnpinOtherChronicles("g", "new"); err != nil {
		t.Fatal(err)
	}
	if pinnedColumn(t, s, old) || s.BlobServable(old) || s.BlobBanned(old) || !heldBlob(s, old) {
		t.Fatal("the replaced archive's picture is pinned, served, banned or gone")
	}
	if !pinnedColumn(t, s, cur) || !s.BlobServable(cur) {
		t.Fatal("the current archive's picture lost its pin")
	}
}

// Leaving the guild takes back its archive's pins.
func TestLeavingTheGuildUnpinsItsArchive(t *testing.T) {
	s := completeStore(t)
	id := blobID('a')
	holdBlob(t, s, id)
	if ok, _ := s.PinChronicleBlob("g", "c", id); !ok {
		t.Fatal("not pinned")
	}
	if err := s.DeleteGuild("g"); err != nil {
		t.Fatal(err)
	}
	if pinnedColumn(t, s, id) || s.BlobServable(id) {
		t.Fatal("a left guild's archive picture is still pinned or served")
	}
	if n, _, _ := s.ChroniclePinned("g"); n != 0 {
		t.Fatalf("%d pins left", n)
	}
}
