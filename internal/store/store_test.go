package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zahak/concord/internal/domain"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "concord.db")
	key := bytes.Repeat([]byte{0x42}, 32)
	s, err := Open(path, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestGuildRoundTrip(t *testing.T) {
	s, _ := openTestStore(t)
	g := domain.NewGuild("MyServer", []byte("group-id"), []byte("owner-key"))

	if err := s.SaveGuild(g); err != nil {
		t.Fatalf("SaveGuild: %v", err)
	}
	guilds, err := s.Guilds()
	if err != nil {
		t.Fatalf("Guilds: %v", err)
	}
	if len(guilds) != 1 {
		t.Fatalf("got %d guilds, want 1", len(guilds))
	}
	got := guilds[0]
	if got.Name != "MyServer" || !bytes.Equal(got.GroupID, []byte("group-id")) {
		t.Fatalf("guild mismatch: %+v", got)
	}
	if len(got.Channels) != 1 || got.Channels[0].Name != "general" {
		t.Fatalf("channels not restored: %+v", got.Channels)
	}
}

func TestMessageRoundTripAndOrder(t *testing.T) {
	s, _ := openTestStore(t)

	for _, body := range []string{"first", "second", "third"} {
		m, err := domain.NewMessage("chan-1", []byte("alice"), body)
		if err != nil {
			t.Fatalf("NewMessage: %v", err)
		}
		if _, err := s.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
	}

	msgs, err := s.Messages("chan-1", 0)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	// Chronological order preserved.
	if msgs[0].Content != "first" || msgs[2].Content != "third" {
		t.Fatalf("wrong order: %q ... %q", msgs[0].Content, msgs[2].Content)
	}
}

func TestMessageBodiesEncryptedAtRest(t *testing.T) {
	s, path := openTestStore(t)
	m, _ := domain.NewMessage("chan-1", []byte("alice"), "topsecret-plaintext")
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read db file: %v", err)
	}
	if bytes.Contains(raw, []byte("topsecret-plaintext")) {
		t.Fatal("plaintext message body found in database file")
	}
}

func TestWrongKeyCannotRead(t *testing.T) {
	// Save with one key...
	path := filepath.Join(t.TempDir(), "c.db")
	s1, err := Open(path, bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("Open s1: %v", err)
	}
	m, _ := domain.NewMessage("chan-1", []byte("alice"), "secret")
	if _, err := s1.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	_ = s1.Close()

	// ...reopen with a different key: reading must fail, not return garbage.
	s2, err := Open(path, bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatalf("Open s2: %v", err)
	}
	defer s2.Close()
	if _, err := s2.Messages("chan-1", 0); err == nil {
		t.Fatal("expected decryption failure with wrong key")
	}
}

func TestContactsTOFUAndVerify(t *testing.T) {
	s, _ := openTestStore(t)

	if err := s.RecordContact("peer-1", "FPR1"); err != nil {
		t.Fatalf("RecordContact: %v", err)
	}
	// Re-recording must not reset state or duplicate.
	if err := s.RecordContact("peer-1", "FPR1"); err != nil {
		t.Fatalf("RecordContact (repeat): %v", err)
	}
	contacts, err := s.Contacts()
	if err != nil {
		t.Fatalf("Contacts: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("got %d contacts, want 1", len(contacts))
	}
	if contacts[0].Verified {
		t.Fatal("new contact should start unverified")
	}

	if err := s.SetVerified("peer-1"); err != nil {
		t.Fatalf("SetVerified: %v", err)
	}
	contacts, _ = s.Contacts()
	if !contacts[0].Verified {
		t.Fatal("contact should be verified after SetVerified")
	}

	// Verifying an unknown contact is an error.
	if err := s.SetVerified("nobody"); err == nil {
		t.Fatal("expected error verifying unknown contact")
	}
}

// Device linking imports the account's verifications before this device has
// ever sighted those peers; a later real sighting must not clear the flag.
func TestImportVerifiedFingerprint(t *testing.T) {
	s, _ := openTestStore(t)

	if err := s.ImportVerifiedFingerprint("FPRX"); err != nil {
		t.Fatalf("ImportVerifiedFingerprint: %v", err)
	}
	vf, err := s.VerifiedFingerprints()
	if err != nil {
		t.Fatalf("VerifiedFingerprints: %v", err)
	}
	if !vf["FPRX"] {
		t.Fatal("imported fingerprint should be verified before any sighting")
	}

	// The peer shows up for real later — still verified, and importing again
	// stays idempotent.
	if err := s.RecordContact("peer-x", "FPRX"); err != nil {
		t.Fatalf("RecordContact: %v", err)
	}
	if err := s.ImportVerifiedFingerprint("FPRX"); err != nil {
		t.Fatalf("ImportVerifiedFingerprint (repeat): %v", err)
	}
	vf, _ = s.VerifiedFingerprints()
	if !vf["FPRX"] {
		t.Fatal("fingerprint should stay verified after a real sighting")
	}
}

func TestMarkDeletedAuthorization(t *testing.T) {
	s, _ := openTestStore(t)
	author := []byte("alice-key")
	m, _ := domain.NewMessage("chan-1", author, "secret plan")
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	// A different peer cannot delete it (not the author, not forced).
	if _, ok, err := s.MarkDeleted(m.ID, []byte("eve-key"), false); err != nil || ok {
		t.Fatalf("non-author delete: ok=%v err=%v (want ok=false)", ok, err)
	}
	msgs, _ := s.Messages("chan-1", 0)
	if len(msgs) != 1 || msgs[0].Deleted || msgs[0].Content != "secret plan" {
		t.Fatal("message should be intact after unauthorized delete attempt")
	}

	// The author can delete their own.
	deleted, ok, err := s.MarkDeleted(m.ID, author, false)
	if err != nil || !ok {
		t.Fatalf("author delete: ok=%v err=%v (want ok=true)", ok, err)
	}
	if !deleted.Deleted {
		t.Fatal("returned message should be marked deleted")
	}
	msgs, _ = s.Messages("chan-1", 0)
	if !msgs[0].Deleted || msgs[0].Content != "" {
		t.Fatalf("stored message should be tombstoned with blank content, got %+v", msgs[0])
	}

	// A moderator (force=true) can delete anyone else's message.
	other, _ := domain.NewMessage("chan-1", []byte("bob-key"), "bob's message")
	if _, err := s.SaveMessage(other); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if _, ok, err := s.MarkDeleted(other.ID, []byte("mod-key"), true); err != nil || !ok {
		t.Fatalf("forced moderator delete: ok=%v err=%v (want ok=true)", ok, err)
	}
}

func TestReactionToggleAndAggregate(t *testing.T) {
	s, _ := openTestStore(t)
	m, _ := domain.NewMessage("chan-1", []byte("alice"), "hi")
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	// Two peers react; one adds a second emoji.
	mustToggle := func(fpr, emoji string, wantAdded bool) {
		added, err := s.ToggleReaction(m.ID, fpr, emoji)
		if err != nil || added != wantAdded {
			t.Fatalf("Toggle(%s,%s): added=%v err=%v (want added=%v)", fpr, emoji, added, err, wantAdded)
		}
	}
	mustToggle("fprA", "👍", true)
	mustToggle("fprB", "👍", true)
	mustToggle("fprA", "🎉", true)

	msgs, _ := s.Messages("chan-1", 0)
	r := msgs[0].Reactions
	if len(r["👍"]) != 2 || len(r["🎉"]) != 1 {
		t.Fatalf("aggregation wrong: %+v", r)
	}

	// fprA taps 👍 again → un-reacts.
	mustToggle("fprA", "👍", false)
	msgs, _ = s.Messages("chan-1", 0)
	if len(msgs[0].Reactions["👍"]) != 1 {
		t.Fatalf("expected 1 👍 after un-react, got %+v", msgs[0].Reactions["👍"])
	}
}

func TestUpdateContentAuthorization(t *testing.T) {
	s, _ := openTestStore(t)
	author := []byte("alice")
	m, _ := domain.NewMessage("chan-1", author, "typo here")
	_, _ = s.SaveMessage(m)

	// Non-author can't edit.
	if ok, err := s.UpdateContent(m.ID, []byte("eve"), "hacked"); err != nil || ok {
		t.Fatalf("non-author edit: ok=%v err=%v", ok, err)
	}
	// Author can; content updates and edited flag set.
	if ok, err := s.UpdateContent(m.ID, author, "fixed now"); err != nil || !ok {
		t.Fatalf("author edit: ok=%v err=%v", ok, err)
	}
	msgs, _ := s.Messages("chan-1", 0)
	if msgs[0].Content != "fixed now" || !msgs[0].Edited {
		t.Fatalf("edit not applied: %+v", msgs[0])
	}
}

func TestSaveMessageIdempotent(t *testing.T) {
	s, _ := openTestStore(t)
	m, _ := domain.NewMessage("chan-1", []byte("alice"), "hi")
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("second save (dup): %v", err)
	}
	msgs, _ := s.Messages("chan-1", 0)
	if len(msgs) != 1 {
		t.Fatalf("duplicate message stored: got %d, want 1", len(msgs))
	}
}

func TestProfileRoundTrip(t *testing.T) {
	s, _ := openTestStore(t)
	p := ProfileRow{Fingerprint: "fpr-1", Name: "euclid", Status: "hi", Emoji: "🌀", Color: "#123456"}
	if err := s.SaveProfile(p); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	// Upsert overwrites.
	p.Name = "euclid2"
	if err := s.SaveProfile(p); err != nil {
		t.Fatalf("SaveProfile (update): %v", err)
	}
	rows, err := s.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "euclid2" || rows[0].Emoji != "🌀" {
		t.Fatalf("profile not persisted correctly: %+v", rows)
	}
}

func TestCommitLog(t *testing.T) {
	s, _ := openTestStore(t)
	g1, g2 := []byte("group-1"), []byte("group-2")

	for _, e := range []uint64{3, 1, 2} { // insert out of order; reads must sort
		if err := s.SaveCommit(g1, e, []byte{byte(e)}); err != nil {
			t.Fatalf("SaveCommit: %v", err)
		}
	}
	if err := s.SaveCommit(g1, 2, []byte{99}); err != nil { // duplicate epoch: no-op
		t.Fatalf("SaveCommit (dup): %v", err)
	}
	if err := s.SaveCommit(g2, 1, []byte{42}); err != nil {
		t.Fatalf("SaveCommit (other group): %v", err)
	}

	rows, err := s.CommitsAfter(g1, 1)
	if err != nil {
		t.Fatalf("CommitsAfter: %v", err)
	}
	if len(rows) != 2 || rows[0].Epoch != 2 || rows[1].Epoch != 3 {
		t.Fatalf("wrong commits: %+v", rows)
	}
	if rows[0].Commit[0] != 2 {
		t.Fatal("duplicate SaveCommit overwrote the original commit bytes")
	}
}

func TestChangedSinceServesStateUpdates(t *testing.T) {
	s, _ := openTestStore(t)
	m, _ := domain.NewMessage("chan-1", []byte("alice"), "hello")
	if _, err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	cursor, err := s.LatestTimestamp("chan-1")
	if err != nil || cursor != m.Sent.UnixNano() {
		t.Fatalf("LatestTimestamp = %d, %v; want %d", cursor, err, m.Sent.UnixNano())
	}
	// Nothing changed since the cursor.
	if got, _ := s.MessagesChangedSince("chan-1", cursor, 0); len(got) != 0 {
		t.Fatalf("expected no changes, got %d", len(got))
	}

	// A reaction touches the message: it must move the cursor and be served.
	if _, err := s.ToggleReaction(m.ID, "fpr-bob", "👍"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}
	newCursor, _ := s.LatestTimestamp("chan-1")
	if newCursor <= cursor {
		t.Fatalf("cursor did not advance after reaction: %d <= %d", newCursor, cursor)
	}
	got, err := s.MessagesChangedSince("chan-1", cursor, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("changed-since after reaction: %v, %d rows", err, len(got))
	}
	if len(got[0].Reactions["👍"]) != 1 {
		t.Fatalf("reaction not carried: %+v", got[0].Reactions)
	}
	if got[0].Updated.IsZero() {
		t.Fatal("Updated not populated on served row")
	}

	// A delete tombstone is served too, with blank content.
	if _, ok, err := s.MarkDeleted(m.ID, []byte("alice"), false); err != nil || !ok {
		t.Fatalf("MarkDeleted: %v %v", ok, err)
	}
	got, _ = s.MessagesChangedSince("chan-1", newCursor, 0)
	if len(got) != 1 || !got[0].Deleted || got[0].Content != "" {
		t.Fatalf("tombstone not served correctly: %+v", got)
	}
}

func TestUpsertSyncedMessage(t *testing.T) {
	s, _ := openTestStore(t)
	const self = "fpr-self"

	// Unknown message: inserted as-is, including state flags and reactions.
	remote, _ := domain.NewMessage("chan-1", []byte("alice"), "from-away")
	remote.Edited = true
	remote.Updated = remote.Sent.Add(time.Minute)
	remote.Reactions = map[string][]string{"👍": {"fpr-bob", self}}
	changed, err := s.UpsertSyncedMessage(remote, self)
	if err != nil || !changed {
		t.Fatalf("insert: changed=%v err=%v", changed, err)
	}
	got, ok, _ := s.MessageByID(remote.ID)
	if !ok || got.Content != "from-away" || !got.Edited {
		t.Fatalf("inserted row wrong: %+v", got)
	}
	// The remote's claim about OUR reaction must not be adopted.
	if len(got.Reactions["👍"]) != 1 || got.Reactions["👍"][0] != "fpr-bob" {
		t.Fatalf("self reaction adopted from remote: %+v", got.Reactions)
	}

	// Local message with our own reaction; a fresher remote snapshot replaces
	// other peers' reactions but leaves ours alone.
	local, _ := domain.NewMessage("chan-1", []byte("alice"), "hello")
	if _, err := s.SaveMessage(local); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if _, err := s.ToggleReaction(local.ID, self, "🔥"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}
	snap := local
	snap.Updated = time.Now().Add(time.Hour) // remote is fresher
	snap.Reactions = map[string][]string{"👍": {"fpr-bob"}}
	if _, err := s.UpsertSyncedMessage(snap, self); err != nil {
		t.Fatalf("upsert reactions: %v", err)
	}
	got, _, _ = s.MessageByID(local.ID)
	if len(got.Reactions["🔥"]) != 1 || got.Reactions["🔥"][0] != self {
		t.Fatalf("own reaction lost: %+v", got.Reactions)
	}
	if len(got.Reactions["👍"]) != 1 || got.Reactions["👍"][0] != "fpr-bob" {
		t.Fatalf("remote reaction not adopted: %+v", got.Reactions)
	}

	// A stale remote (older Updated) must not clobber newer local state.
	if ok, err := s.UpdateContent(local.ID, []byte("alice"), "hello-edited"); err != nil || !ok {
		t.Fatalf("UpdateContent: %v %v", ok, err)
	}
	stale := local
	stale.Content = "hello" // pre-edit copy, zero Updated
	if _, err := s.UpsertSyncedMessage(stale, self); err != nil {
		t.Fatalf("stale upsert: %v", err)
	}
	got, _, _ = s.MessageByID(local.ID)
	if got.Content != "hello-edited" {
		t.Fatalf("stale remote clobbered local edit: %q", got.Content)
	}

	// Tombstones always win, regardless of timestamps.
	dead := local
	dead.Deleted = true
	if changed, err := s.UpsertSyncedMessage(dead, self); err != nil || !changed {
		t.Fatalf("tombstone upsert: changed=%v err=%v", changed, err)
	}
	got, _, _ = s.MessageByID(local.ID)
	if !got.Deleted {
		t.Fatal("tombstone not applied")
	}
}

func TestAttachmentRoundTrip(t *testing.T) {
	s, _ := openTestStore(t)
	ct := bytes.Repeat([]byte{7}, 1024)

	if err := s.SaveAttachment("blob-1", ct); err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
	// Idempotent by blob ID.
	if err := s.SaveAttachment("blob-1", []byte("different")); err != nil {
		t.Fatalf("SaveAttachment (dup): %v", err)
	}
	got, ok, err := s.GetAttachment("blob-1")
	if err != nil || !ok {
		t.Fatalf("GetAttachment: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, ct) {
		t.Fatal("duplicate save overwrote original ciphertext")
	}
	if _, ok, _ := s.GetAttachment("blob-missing"); ok {
		t.Fatal("missing blob reported present")
	}
}

// TestReadStateAdvance covers the newest-wins read cursor: advancing works,
// stale/duplicate marks are rejected, and the map read returns everything.
func TestReadStateAdvance(t *testing.T) {
	s, _ := openTestStore(t)

	adv, err := s.AdvanceReadState("ch1", 1000)
	if err != nil || !adv {
		t.Fatalf("first advance = (%v, %v), want (true, nil)", adv, err)
	}
	// Stale and duplicate marks must not move the cursor.
	if adv, _ = s.AdvanceReadState("ch1", 900); adv {
		t.Fatal("stale mark advanced the cursor")
	}
	if adv, _ = s.AdvanceReadState("ch1", 1000); adv {
		t.Fatal("duplicate mark advanced the cursor")
	}
	if adv, _ = s.AdvanceReadState("ch1", 2000); !adv {
		t.Fatal("newer mark did not advance")
	}
	if adv, _ = s.AdvanceReadState("ch2", 500); !adv {
		t.Fatal("second channel did not advance")
	}

	got, err := s.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got["ch1"] != 2000 || got["ch2"] != 500 || len(got) != 2 {
		t.Fatalf("ReadState = %v, want ch1:2000 ch2:500", got)
	}
}
