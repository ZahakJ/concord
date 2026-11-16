package store

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
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

// manyTokens is a body naming n distinct blobs, each in an image token.
func manyTokens(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(imageToken(fmt.Sprintf("%064x", i+1)) + " ")
	}
	return sb.String()
}

// A body is indexed under at most blobRefsPerBody blobs, however many it names.
func TestABodyIsIndexedUnderAtMostTheCap(t *testing.T) {
	body := manyTokens(12000)
	if ids := BlobIDsIn(body); len(ids) != blobRefsPerBody {
		t.Errorf("BlobIDsIn read %d ids, want %d", len(ids), blobRefsPerBody)
	}
	s := completeStore(t)
	m := saveBody(t, s, body)
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM blob_refs WHERE message_id = ?`, m.ID).Scan(&n)
	if n != blobRefsPerBody {
		t.Errorf("a body naming 12000 blobs wrote %d references, want %d", n, blobRefsPerBody)
	}
}

// A backfill batch ends once it has written about blobRefsBatchRefs
// references, and the next batch goes on from where it stopped.
func TestABackfillBatchEndsAtTheReferenceBound(t *testing.T) {
	s, _ := openTestStore(t)
	const rows = 400
	body := manyTokens(blobRefsPerBody)
	for i := 0; i < rows; i++ {
		saveBody(t, s, body)
	}
	if _, err := s.db.Exec(`DELETE FROM blob_refs`); err != nil {
		t.Fatal(err)
	}
	count := func() (n int) {
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM blob_refs`).Scan(&n)
		return n
	}
	done, err := s.BackfillBlobRefs()
	if err != nil {
		t.Fatal(err)
	}
	first := count()
	if done || first < blobRefsBatchRefs || first >= blobRefsBatchRefs+blobRefsPerBody {
		t.Errorf("the first batch wrote %d references (done=%v), want it to stop just past %d", first, done, blobRefsBatchRefs)
	}
	for calls := 0; !done && calls < 10; calls++ {
		if done, err = s.BackfillBlobRefs(); err != nil {
			t.Fatal(err)
		}
	}
	if !done || count() != rows*blobRefsPerBody {
		t.Errorf("after the backfill: done=%v, %d references, want %d", done, count(), rows*blobRefsPerBody)
	}
}

// A backfill batch also ends once it has read blobRefsBatchBytes of bodies.
func TestABackfillBatchEndsAtTheByteBound(t *testing.T) {
	s, _ := openTestStore(t)
	const rows = 6
	long := strings.Repeat("x", 1<<20)
	for i := 0; i < rows; i++ {
		saveBody(t, s, long+imageToken(fmt.Sprintf("%064x", i+1)))
	}
	if _, err := s.db.Exec(`DELETE FROM blob_refs`); err != nil {
		t.Fatal(err)
	}
	calls, done := 0, false
	for ; !done && calls < 20; calls++ {
		var err error
		if done, err = s.BackfillBlobRefs(); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM blob_refs`).Scan(&n)
	if !done || calls < 2 || n != rows {
		t.Errorf("%d calls (done=%v) indexed %d references, want at least 2 calls and %d", calls, done, n, rows)
	}
}

// An edit, and a relayed row replaced by its authenticated copy, write the new
// body and its blob references in one transaction: when the references cannot
// be written, the body is not changed either.
func TestABodyChangeAndItsReferencesAreWrittenTogether(t *testing.T) {
	s := completeStore(t)
	edited := saveBody(t, s, "before the edit")
	relayed, _ := domain.NewMessage("chan-1", []byte("alice-key"), "as relayed")
	relayed.Unverified = true
	if _, err := s.SaveMessage(relayed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`ALTER TABLE blob_refs RENAME TO blob_refs_away`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateContent(edited.ID, edited.Sender, "after "+imageToken(blobID('a')), nil, "", false); err == nil {
		t.Error("an edit whose references could not be written reported no error")
	}
	if body, _ := s.MessageContent(edited.ID); body != "before the edit" {
		t.Errorf("the edit was stored without its references: %q", body)
	}
	proven := relayed
	proven.Content = "as written " + imageToken(blobID('b'))
	proven.Unverified = false
	if _, err := s.ProveRelayedMessage(proven); err == nil {
		t.Error("a proven copy whose references could not be written reported no error")
	}
	if body, _ := s.MessageContent(relayed.ID); body != "as relayed" {
		t.Errorf("the proven copy was stored without its references: %q", body)
	}
}

// Rows written to the store by a build that does not index blobs, after the
// backfill finished, are indexed at the next start before any blob goes.
// After a clean close there is nothing to index.
func TestRowsWrittenWithoutTheIndexAreIndexedAtTheNextStart(t *testing.T) {
	s, path := openTestStore(t)
	reopen := func() *Store {
		t.Helper()
		r, err := Open(path, bytes.Repeat([]byte{0x42}, 32))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = r.Close() })
		return r
	}
	backfill := func(st *Store) int {
		t.Helper()
		for calls := 1; calls < 20; calls++ {
			done, err := st.BackfillBlobRefs()
			if err != nil {
				t.Fatal(err)
			}
			if done {
				return calls
			}
		}
		t.Fatal("the backfill never finished")
		return 0
	}
	backfill(s)
	shared := blobID('a')
	holdBlob(t, s, shared)
	gone := saveBody(t, s, "to be deleted "+imageToken(shared))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A start after a clean close finds nothing past the cursor.
	s = reopen()
	var newest int64
	_ = s.db.QueryRow(`SELECT max(rowid) FROM messages`).Scan(&newest)
	if cur, _ := s.GetSetting(settingBlobRefsCursor); cur != fmt.Sprint(newest) {
		t.Errorf("after a clean close the cursor is %q, want the newest row %d", cur, newest)
	}
	if calls := backfill(s); calls != 1 {
		t.Errorf("after a clean close the backfill took %d calls, want 1", calls)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A build without the index writes a row naming the same blob: the row
	// is stored with no references, and the cursor is not moved.
	old := reopen()
	written := saveBody(t, old, "written by an older build "+imageToken(shared))
	if _, err := old.db.Exec(`DELETE FROM blob_refs WHERE message_id = ?`, written.ID); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	s = reopen()
	if s.BlobRefsComplete() {
		t.Error("a store with rows past the cursor says it is complete before the backfill ran")
	}
	backfill(s)
	if banned, err := s.ReleaseBlobs(gone.ID); err != nil || len(banned) != 0 || !heldBlob(s, shared) {
		t.Errorf("a blob a row written by an older build names was removed: %v %v", banned, err)
	}
}

// A blob that a scheduled send names stays when the last held message naming
// it goes.
func TestABlobAScheduledSendNamesStays(t *testing.T) {
	s := completeStore(t)
	queued, alone := blobID('a'), blobID('b')
	holdBlob(t, s, queued)
	holdBlob(t, s, alone)
	m := saveBody(t, s, imageToken(queued)+" "+imageToken(alone))
	if err := s.AddScheduledSend(ScheduledSend{ID: "later-1", ChannelID: "chan-1",
		Content: "forwarded later " + imageToken(queued), FireAt: 1 << 40, Created: 1}); err != nil {
		t.Fatal(err)
	}
	banned, err := s.ReleaseBlobs(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !heldBlob(s, queued) || s.BlobBanned(queued) {
		t.Errorf("a blob a scheduled send names was removed (banned %v)", banned)
	}
	if heldBlob(s, alone) || !s.BlobBanned(alone) {
		t.Errorf("a blob nothing else names was kept (banned %v)", banned)
	}
}

// A ban is lifted when this device stores a message from another account
// that names the blob, and not by another message from the account whose
// message was released, from the same device or another of its devices.
func TestAnotherAccountsMessageLiftsABan(t *testing.T) {
	s := completeStore(t)
	b := blobID('a')
	holdBlob(t, s, b)
	device := func(acct *identity.Identity) []byte {
		t.Helper()
		other, err := identity.Generate()
		if err != nil {
			t.Fatal(err)
		}
		return acct.IssueDeviceCertFor(other.PublicKey(), "device", 1).Marshal()
	}
	x, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	y, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	xPhone, xDesk := device(x), device(x)
	from := func(sender []byte, body string) domain.Message {
		t.Helper()
		m, _ := domain.NewMessage("chan-1", sender, body)
		if _, err := s.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
		return m
	}
	released := from(xPhone, "copied "+imageToken(b))
	if banned, err := s.ReleaseBlobs(released.ID); err != nil || len(banned) != 1 || !s.BlobBanned(b) {
		t.Fatalf("setup: the released blob was not banned: %v %v", banned, err)
	}
	for name, sender := range map[string][]byte{"the same device": xPhone, "another device of the account": xDesk, "the account key": x.PublicKey()} {
		from(sender, "again from "+name+" "+imageToken(b))
		if !s.BlobBanned(b) {
			t.Fatalf("a message from %s whose message was released lifted the ban", name)
		}
	}
	from(device(y), "the picture's own message "+imageToken(b))
	if s.BlobBanned(b) {
		t.Error("a message from another account naming the blob left it banned")
	}
	if err := s.SaveAttachment(b, []byte("ciphertext again")); err != nil || !heldBlob(s, b) {
		t.Errorf("the blob could not be stored again after the ban was lifted: %v", err)
	}
}

// A blob is served only while something on the device keeps it: a held row
// whose body names it and is not erased, the guild GIF pack, or a pin. A
// cache entry no row names, a blob only an erased row names, and a banned
// blob are not.
func TestOnlyAKeptBlobIsServed(t *testing.T) {
	s := completeStore(t)
	named, orphan, erased, gif, pinned := blobID('a'), blobID('b'), blobID('c'), blobID('d'), blobID('e')
	for _, id := range []string{named, orphan, erased, gif, pinned} {
		holdBlob(t, s, id)
	}
	saveBody(t, s, "live "+imageToken(named))
	m := saveBody(t, s, "erased "+imageToken(erased))
	if _, ok, err := s.MarkDeleted(m.ID, nil, true); err != nil || !ok {
		t.Fatalf("MarkDeleted: %v %v", ok, err)
	}
	if err := s.EraseContent(m.ID); err != nil {
		t.Fatal(err)
	}
	// Deleted with its body kept for reveal: held, and served to nobody.
	keptForReveal := blobID('f')
	holdBlob(t, s, keptForReveal)
	k := saveBody(t, s, "kept for reveal "+imageToken(keptForReveal))
	if _, ok, err := s.MarkDeleted(k.ID, nil, true); err != nil || !ok {
		t.Fatalf("MarkDeleted: %v %v", ok, err)
	}
	if err := s.SaveGuildGif(GuildGifRow{GuildID: "g", ID: gif, Name: "n", Keys: testKeys, Subtype: "gif"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PinAttachment(pinned, true); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{named: true, gif: true, pinned: true, orphan: false, erased: false, keptForReveal: false} {
		if got := s.BlobServable(id); got != want {
			t.Errorf("blob %s…: servable %v, want %v", id[:4], got, want)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO banned_blobs (blob_id, at) VALUES (?, 1)`, named); err != nil {
		t.Fatal(err)
	}
	if s.BlobServable(named) {
		t.Error("a banned blob is servable")
	}
}
