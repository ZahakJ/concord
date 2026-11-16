package store

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

var testKeys = strings.Repeat("A", 75)

func blobID(c byte) string { return strings.Repeat(string(c), 64) }

func imageToken(id string) string {
	return "![image](concord://attach/v1/" + id + "/" + testKeys + "/png/1x1)"
}

func lpToken(t *testing.T, id string) string {
	t.Helper()
	raw, err := json.Marshal([]map[string]any{{"u": "https://example.com/x", "t": "Example",
		"i": map[string]any{"b": id, "k": testKeys, "f": "jpeg", "w": 4, "h": 3}}})
	if err != nil {
		t.Fatal(err)
	}
	return "[lp](concord://lp/v1/" + base64.RawURLEncoding.EncodeToString(raw) + ")"
}

// completeStore is a test store whose index backfill has run.
func completeStore(t *testing.T) *Store {
	t.Helper()
	s, _ := openTestStore(t)
	if done, err := s.BackfillBlobRefs(); err != nil || !done {
		t.Fatalf("BackfillBlobRefs on an empty store: %v %v", done, err)
	}
	return s
}

func saveBody(t *testing.T, s *Store, body string) domain.Message {
	t.Helper()
	m, _ := domain.NewMessage("chan-1", []byte("alice-key"), body)
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	return m
}

func holdBlob(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.SaveAttachment(id, []byte("ciphertext of "+id[:4])); err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
}

func heldBlob(s *Store, id string) bool {
	_, ok, err := s.GetAttachment(id)
	return err == nil && ok
}

// The blob ids of a body are those of its image and file tokens and its link
// previews' pictures, each once; anything malformed names nothing.
func TestBlobIDsInReadsEveryTokenKind(t *testing.T) {
	a, b, c := blobID('a'), blobID('b'), blobID('c')
	body := "words " + imageToken(a) + " " +
		"![image](concord://attach/v2/" + b + "/" + testKeys + "/png/1x1/1/eA/eQ) " +
		"[file](concord://file/v1/" + c + "/" + testKeys + "/12/dGV4dA/bmFtZQ) " +
		imageToken(a) + " " + lpToken(t, blobID('d')) +
		" concord://attach/v1/" + strings.Repeat("z", 64) + "/ [lp](concord://lp/v1/!!!)"
	got := BlobIDsIn(body)
	want := []string{a, b, c, blobID('d')}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BlobIDsIn = %v, want %v", got, want)
	}
}

// A blob is removed and banned when the last held message naming it lets go
// of it, and not before. A banned blob is not stored again.
func TestABlobGoesWithTheLastMessageThatNamesIt(t *testing.T) {
	s := completeStore(t)
	shared, own := blobID('a'), blobID('b')
	holdBlob(t, s, shared)
	holdBlob(t, s, own)
	first := saveBody(t, s, imageToken(shared)+" "+lpToken(t, own))
	second := saveBody(t, s, "a forward "+imageToken(shared))

	banned, err := s.ReleaseBlobs(first.ID)
	if err != nil {
		t.Fatalf("ReleaseBlobs: %v", err)
	}
	if len(banned) != 1 || banned[0] != own {
		t.Errorf("banned %v, want only the first message's own card picture", banned)
	}
	if !heldBlob(s, shared) || s.BlobBanned(shared) {
		t.Error("a blob another held message names was removed")
	}
	if heldBlob(s, own) || !s.BlobBanned(own) {
		t.Error("a blob no other message names was kept")
	}
	if _, err := s.ReleaseBlobs(second.ID); err != nil {
		t.Fatalf("ReleaseBlobs: %v", err)
	}
	if heldBlob(s, shared) || !s.BlobBanned(shared) {
		t.Error("the shared blob outlived the last message naming it")
	}
	holdBlob(t, s, shared)
	if heldBlob(s, shared) {
		t.Error("a banned blob was stored again")
	}
}

// A blob in the guild GIF pack, or pinned, is kept when its message goes.
func TestABlobThePackOrAPinKeepsStays(t *testing.T) {
	s := completeStore(t)
	gif, pinned := blobID('a'), blobID('b')
	holdBlob(t, s, gif)
	holdBlob(t, s, pinned)
	if err := s.SaveGuildGif(GuildGifRow{GuildID: "g", ID: gif, Name: "wave", Keys: testKeys, Subtype: "gif"}); err != nil {
		t.Fatalf("SaveGuildGif: %v", err)
	}
	if err := s.PinAttachment(pinned, true); err != nil {
		t.Fatalf("PinAttachment: %v", err)
	}
	m := saveBody(t, s, imageToken(gif)+" "+imageToken(pinned))
	if banned, err := s.ReleaseBlobs(m.ID); err != nil || len(banned) != 0 {
		t.Errorf("ReleaseBlobs banned %v (%v), want none", banned, err)
	}
	if !heldBlob(s, gif) || !heldBlob(s, pinned) {
		t.Error("a pack or pinned blob was removed")
	}
}

// Every path that stores or changes a body indexes its blobs: an insert, a
// row taken by history sync, and an edit, which keeps the blobs of the body
// it replaced.
func TestEveryBodyWriteIndexesItsBlobs(t *testing.T) {
	s := completeStore(t)
	a, b, c := blobID('a'), blobID('b'), blobID('c')
	for _, id := range []string{a, b, c} {
		holdBlob(t, s, id)
	}
	author := []byte("alice-key")
	edited := saveBody(t, s, imageToken(a))
	if ok, err := s.UpdateContent(edited.ID, author, imageToken(b), nil, "", false); err != nil || !ok {
		t.Fatalf("UpdateContent: %v %v", ok, err)
	}
	synced, _ := domain.NewMessage("chan-1", author, imageToken(c))
	if _, err := s.UpsertSyncedMessage(synced, "fpr-self", false); err != nil {
		t.Fatalf("UpsertSyncedMessage: %v", err)
	}
	// A second message naming each keeps it unless the index missed it.
	other := saveBody(t, s, imageToken(a)+imageToken(b)+imageToken(c))
	if banned, _ := s.ReleaseBlobs(other.ID); len(banned) != 0 {
		t.Errorf("blobs %v were banned though an indexed message still names them", banned)
	}
	if banned, _ := s.ReleaseBlobs(edited.ID); len(banned) != 2 {
		t.Errorf("an edited message released %v, want both its versions' blobs", banned)
	}
	if banned, _ := s.ReleaseBlobs(synced.ID); len(banned) != 1 {
		t.Errorf("a synced message released %v, want its blob", banned)
	}
}

// Until the backfill has indexed every row stored before the index, no blob
// is removed or banned; the backfill then finds the old rows' blobs.
func TestNoBlobGoesBeforeTheBackfillIsComplete(t *testing.T) {
	s, _ := openTestStore(t)
	shared := blobID('a')
	holdBlob(t, s, shared)
	old := saveBody(t, s, "from before the index "+imageToken(shared))
	if _, err := s.db.Exec(`DELETE FROM blob_refs WHERE message_id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}
	fresh := saveBody(t, s, imageToken(shared))

	if s.BlobRefsComplete() {
		t.Fatal("a store that has not run the backfill says it is complete")
	}
	if banned, err := s.ReleaseBlobs(fresh.ID); err != nil || len(banned) != 0 || !heldBlob(s, shared) {
		t.Fatalf("a blob went before the backfill finished: %v %v", banned, err)
	}
	for i := 0; ; i++ {
		done, err := s.BackfillBlobRefs()
		if err != nil {
			t.Fatalf("BackfillBlobRefs: %v", err)
		}
		if done {
			break
		}
		if i > 10 {
			t.Fatal("backfill never finished")
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM blob_refs WHERE message_id = ?`, old.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("the backfill indexed %d blobs of the old row (%v)", n, err)
	}
}

// The backfill walks a store larger than one batch.
func TestBlobBackfillCrossesBatches(t *testing.T) {
	s, _ := openTestStore(t)
	id := blobID('e')
	for i := 0; i < blobRefsBatch+5; i++ {
		saveBody(t, s, imageToken(id))
	}
	if _, err := s.db.Exec(`DELETE FROM blob_refs`); err != nil {
		t.Fatal(err)
	}
	batches := 0
	for {
		done, err := s.BackfillBlobRefs()
		if err != nil {
			t.Fatal(err)
		}
		batches++
		if done {
			break
		}
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM blob_refs`).Scan(&n)
	if batches != 2 || n != blobRefsBatch+5 {
		t.Errorf("%d batches indexed %d refs, want 2 and %d", batches, n, blobRefsBatch+5)
	}
}
