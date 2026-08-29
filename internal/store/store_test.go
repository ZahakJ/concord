package store

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
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
	if ok, err := s.UpdateContent(m.ID, []byte("eve"), "hacked", nil, "", false); err != nil || ok {
		t.Fatalf("non-author edit: ok=%v err=%v", ok, err)
	}
	// Author can; content updates and edited flag set.
	if ok, err := s.UpdateContent(m.ID, author, "fixed now", nil, "", false); err != nil || !ok {
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

func TestPruneCommitsNeedsBothHorizons(t *testing.T) {
	s, _ := openTestStore(t)
	g := []byte("group-1")
	for e := uint64(1); e <= 10; e++ {
		if err := s.SaveCommit(g, e, []byte{byte(e)}); err != nil {
			t.Fatalf("SaveCommit: %v", err)
		}
	}

	// Young rows are kept however far back they are: a guild that changed
	// membership ten times this morning keeps all ten.
	n, err := s.PruneCommits(g, 2, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("PruneCommits: %v", err)
	}
	if n != 0 {
		t.Fatalf("pruned %d rows that were inside the age horizon", n)
	}

	// Old rows are kept while they are inside the count: a guild that has
	// changed membership twice since 2019 keeps both.
	if n, err = s.PruneCommits(g, 100, time.Now()); err != nil {
		t.Fatalf("PruneCommits: %v", err)
	} else if n != 0 {
		t.Fatalf("pruned %d rows that were inside the count horizon", n)
	}

	// Past both, and only then, they go — and it is the OLDEST that go.
	if n, err = s.PruneCommits(g, 3, time.Now()); err != nil {
		t.Fatalf("PruneCommits: %v", err)
	} else if n != 7 {
		t.Fatalf("pruned %d rows, want 7", n)
	}
	rows, err := s.CommitsAfter(g, 0)
	if err != nil {
		t.Fatalf("CommitsAfter: %v", err)
	}
	if len(rows) != 3 || rows[0].Epoch != 8 || rows[2].Epoch != 10 {
		t.Fatalf("kept the wrong rows: %+v", rows)
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
	changed, err := s.UpsertSyncedMessage(remote, self, true)
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
	if _, err := s.UpsertSyncedMessage(snap, self, true); err != nil {
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
	if ok, err := s.UpdateContent(local.ID, []byte("alice"), "hello-edited", nil, "", false); err != nil || !ok {
		t.Fatalf("UpdateContent: %v %v", ok, err)
	}
	stale := local
	stale.Content = "hello" // pre-edit copy, zero Updated
	if _, err := s.UpsertSyncedMessage(stale, self, true); err != nil {
		t.Fatalf("stale upsert: %v", err)
	}
	got, _, _ = s.MessageByID(local.ID)
	if got.Content != "hello-edited" {
		t.Fatalf("stale remote clobbered local edit: %q", got.Content)
	}

	// Tombstones always win, regardless of timestamps.
	dead := local
	dead.Deleted = true
	if changed, err := s.UpsertSyncedMessage(dead, self, true); err != nil || !changed {
		t.Fatalf("tombstone upsert: changed=%v err=%v", changed, err)
	}
	got, _, _ = s.MessageByID(local.ID)
	if !got.Deleted {
		t.Fatal("tombstone not applied")
	}
}

// TestUpsertSyncedMessageUntrusted pins the trust gate: a backfill from an
// untrusted source may insert a genuinely new message, but must not tombstone or
// overwrite a message we already hold.
func TestUpsertSyncedMessageUntrusted(t *testing.T) {
	s, _ := openTestStore(t)
	const self = "fpr-self"

	// New message from an untrusted peer: still inserted (gap-fill catch-up).
	fresh, _ := domain.NewMessage("chan-1", []byte("alice"), "caught-up")
	if changed, err := s.UpsertSyncedMessage(fresh, self, false); err != nil || !changed {
		t.Fatalf("untrusted insert: changed=%v err=%v", changed, err)
	}

	// A message we already hold must not be tombstoned by an untrusted backfill.
	local, _ := domain.NewMessage("chan-1", []byte("alice"), "keep me")
	if _, err := s.SaveMessage(local); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	dead := local
	dead.Deleted = true
	if _, err := s.UpsertSyncedMessage(dead, self, false); err != nil {
		t.Fatalf("untrusted tombstone: %v", err)
	}
	got, _, _ := s.MessageByID(local.ID)
	if got.Deleted {
		t.Fatal("untrusted backfill tombstoned an existing message")
	}

	// Nor overwritten with fresher content.
	rewrite := local
	rewrite.Content = "spoofed"
	rewrite.Updated = time.Now().Add(time.Hour)
	if _, err := s.UpsertSyncedMessage(rewrite, self, false); err != nil {
		t.Fatalf("untrusted rewrite: %v", err)
	}
	got, _, _ = s.MessageByID(local.ID)
	if got.Content != "keep me" {
		t.Fatalf("untrusted backfill rewrote content: %q", got.Content)
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

// PruneContacts deletes rows from a live user's database, so the rule that
// matters is what it must NOT take: a verified contact, whatever else is true
// of it, and anyone still sharing a group with us.
func TestPruneContactsKeepsVerifiedAndRelated(t *testing.T) {
	s, _ := openTestStore(t)

	// friend: unverified but still in a shared guild — kept by the keep set.
	// stranger: neither — the case this exists to clear.
	// verified: not in any guild, but verified by hand.
	if err := s.RecordContact("peer-friend", "FRIEND"); err != nil {
		t.Fatalf("RecordContact: %v", err)
	}
	if err := s.RecordContact("peer-stranger", "STRANGER"); err != nil {
		t.Fatalf("RecordContact: %v", err)
	}
	if err := s.RecordContact("peer-verified", "VERIFIED"); err != nil {
		t.Fatalf("RecordContact: %v", err)
	}
	if err := s.SetVerified("peer-verified"); err != nil {
		t.Fatalf("SetVerified: %v", err)
	}

	n, err := s.PruneContacts(map[string]bool{"FRIEND": true})
	if err != nil {
		t.Fatalf("PruneContacts: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d contacts, want 1 (the stranger)", n)
	}

	got, err := s.Contacts()
	if err != nil {
		t.Fatalf("Contacts: %v", err)
	}
	left := map[string]bool{}
	for _, c := range got {
		left[c.Fingerprint] = true
	}
	if !left["FRIEND"] {
		t.Error("pruned a contact we still share a guild with")
	}
	if !left["VERIFIED"] {
		t.Error("pruned a hand-verified contact — verification is a human act")
	}
	if left["STRANGER"] {
		t.Error("kept an unrelated stranger")
	}

	// Idempotent: a second pass with nothing new to do must delete nothing.
	if n, err := s.PruneContacts(map[string]bool{"FRIEND": true}); err != nil || n != 0 {
		t.Fatalf("second prune = (%d, %v), want (0, nil)", n, err)
	}
}

// The filtered search exists so that from:/in:/before:/after: narrow the scan
// in SQL — a filter that silently matched nothing (or everything) would
// either hide messages from the user or decrypt the whole history per
// keystroke again. Paging via BeforeUnix and the internal keyset batches get
// exercised too: a match sitting past the first batch must still be found.
func TestSearchMessagesFilters(t *testing.T) {
	s, _ := openTestStore(t)

	// Two channels with names, so in: can resolve a name to an id.
	g := domain.NewGuild("G", []byte("gid"), []byte("owner"))
	g.Channels = []domain.Channel{
		{ID: "ch-gen", GuildID: g.ID, Name: "general"},
		{ID: "ch-dev", GuildID: g.ID, Name: "dev"},
	}
	if err := s.SaveGuild(g); err != nil {
		t.Fatalf("SaveGuild: %v", err)
	}

	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	save := func(id, ch, name, body string, at time.Time) {
		t.Helper()
		if _, err := s.SaveMessage(domain.Message{
			ID: id, ChannelID: ch, Sender: []byte(name), Name: name,
			Content: body, Sent: at,
		}); err != nil {
			t.Fatalf("SaveMessage %s: %v", id, err)
		}
	}
	save("m1", "ch-gen", "alice", "pizza tonight", base)
	save("m2", "ch-dev", "alice", "pizza build is green", base.Add(1*time.Hour))
	save("m3", "ch-gen", "bob", "pizza again?", base.Add(2*time.Hour))
	save("m4", "ch-dev", "bob", "no pizza talk here", base.Add(3*time.Hour))

	contents := func(ms []domain.Message) []string {
		var out []string
		for _, m := range ms {
			out = append(out, m.ID)
		}
		return out
	}

	// from: narrows by sender display name, case-insensitively.
	hits, err := s.SearchMessages("pizza", 50, SearchFilter{FromSender: "ALICE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != "m2" || hits[1].ID != "m1" {
		t.Fatalf("from:alice got %v, want [m2 m1]", contents(hits))
	}

	// in: accepts a channel name (with or without '#') or a raw id.
	for _, ch := range []string{"dev", "#dev", "ch-dev"} {
		hits, err = s.SearchMessages("pizza", 50, SearchFilter{InChannel: ch})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 2 || hits[0].ID != "m4" || hits[1].ID != "m2" {
			t.Fatalf("in:%s got %v, want [m4 m2]", ch, contents(hits))
		}
	}

	// before:/after: bound sent strictly, in UnixNano like the column.
	hits, err = s.SearchMessages("pizza", 50, SearchFilter{
		AfterUnix:  base.UnixNano(),
		BeforeUnix: base.Add(3 * time.Hour).UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != "m3" || hits[1].ID != "m2" {
		t.Fatalf("after/before window got %v, want [m3 m2]", contents(hits))
	}

	// BeforeUnix as keyset cursor: page through all four hits one at a time.
	var paged []string
	cursor := int64(0)
	for {
		hits, err = s.SearchMessages("pizza", 1, SearchFilter{BeforeUnix: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 {
			break
		}
		paged = append(paged, hits[0].ID)
		cursor = hits[0].Sent.UnixNano()
	}
	if len(paged) != 4 || paged[0] != "m4" || paged[3] != "m1" {
		t.Fatalf("keyset paging walked %v, want [m4 m3 m2 m1]", paged)
	}

	// A match deeper than one internal batch (256 rows) must still be found:
	// the keyset loop, not the first page, is what finds it.
	old := base.Add(-time.Hour)
	for i := 0; i < 300; i++ {
		save(fmt.Sprintf("noise-%03d", i), "ch-gen", "carol", "nothing to see",
			old.Add(time.Duration(i)*time.Millisecond))
	}
	save("deep", "ch-gen", "carol", "the buried pizza", old.Add(-time.Minute))
	hits, err = s.SearchMessages("buried", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "deep" {
		t.Fatalf("match beyond the first keyset batch got %v, want [deep]", contents(hits))
	}
}

func TestStoriesRoundTripExpiryAndSeen(t *testing.T) {
	s, path := openTestStore(t)
	now := time.Now().Unix()

	fresh := StoryRow{
		StoryID: "st-fresh", GuildID: "g1", Author: "FPRA",
		Preset: "preset:galaxy", Caption: "meet in the-void tonight",
		Color1: "#aabbcc", Color2: "#112233",
		PostedAt: now, ExpiresAt: now + 3600, Sig: []byte("sig-a"),
	}
	dead := StoryRow{
		StoryID: "st-dead", GuildID: "g1", Author: "FPRB",
		Preset: "preset:dune", Caption: "yesterday's news",
		PostedAt: now - 90000, ExpiresAt: now - 3600, Sig: []byte("sig-b"),
	}
	otherGuild := fresh
	otherGuild.StoryID, otherGuild.GuildID = "st-other", "g2"
	for _, r := range []StoryRow{fresh, dead, otherGuild} {
		if _, err := s.SaveStory(r); err != nil {
			t.Fatalf("SaveStory(%s): %v", r.StoryID, err)
		}
	}

	// Saving the same story id again is a no-op, not a second row and not an
	// overwrite — gossip re-delivery and overlapping syncs replay records.
	changed := fresh
	changed.Caption = "rewritten"
	if inserted, err := s.SaveStory(changed); err != nil || inserted {
		t.Fatalf("replayed SaveStory: inserted=%v err=%v, want no-op", inserted, err)
	}

	// Listing is guild-scoped and judges expiry by the caller's clock — the
	// dead story is invisible even though no sweep has run yet.
	got, err := s.GuildStories("g1", now)
	if err != nil {
		t.Fatalf("GuildStories: %v", err)
	}
	if len(got) != 1 || got[0].StoryID != "st-fresh" {
		t.Fatalf("GuildStories(g1) = %+v, want just st-fresh", got)
	}
	r := got[0]
	if r.Caption != fresh.Caption || r.Preset != fresh.Preset ||
		r.Color1 != fresh.Color1 || r.Author != "FPRA" || !bytes.Equal(r.Sig, fresh.Sig) {
		t.Fatalf("story round-trip mismatch: %+v", r)
	}

	// The caption is content, sealed at rest like a message body.
	for _, f := range []string{path, path + "-wal"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue // -wal may not exist
		}
		if bytes.Contains(raw, []byte("meet in the-void tonight")) {
			t.Fatalf("plaintext story caption found in %s", f)
		}
	}

	// Seen markers are local and idempotent.
	if s2, err := s.StoryIsSeen("st-fresh"); err != nil || s2 {
		t.Fatalf("StoryIsSeen before marking = %v, %v", s2, err)
	}
	if err := s.MarkStorySeen("st-fresh"); err != nil {
		t.Fatalf("MarkStorySeen: %v", err)
	}
	if err := s.MarkStorySeen("st-fresh"); err != nil {
		t.Fatalf("MarkStorySeen (again): %v", err)
	}
	if s2, err := s.StoryIsSeen("st-fresh"); err != nil || !s2 {
		t.Fatalf("StoryIsSeen after marking = %v, %v", s2, err)
	}
	_ = s.MarkStorySeen("st-dead")

	// The sweep removes the expired story and its seen marker, and only those.
	n, err := s.DeleteExpiredStories(now)
	if err != nil {
		t.Fatalf("DeleteExpiredStories: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteExpiredStories removed %d, want 1", n)
	}
	if got, _ := s.GuildStories("g1", now); len(got) != 1 {
		t.Fatalf("fresh story lost to the sweep: %+v", got)
	}
	if got, _ := s.GuildStories("g2", now); len(got) != 1 {
		t.Fatalf("other guild's story lost to the sweep: %+v", got)
	}
	if seen, _ := s.StoryIsSeen("st-dead"); seen {
		t.Fatal("seen marker must die with its expired story")
	}
	if seen, _ := s.StoryIsSeen("st-fresh"); !seen {
		t.Fatal("living story's seen marker must survive the sweep")
	}
}

// Open must wait out a lock rather than fail on it. The failure this guards is
// specific: switching a fresh database to WAL needs an exclusive lock, so an
// Open that races another connection dies at "set pragmas: database is locked"
// and the app refuses to start. That is the shape of a self-update relaunch,
// where the incoming process opens the store while the outgoing one is still
// closing (internal/bridge/restart_unix.go).
func TestOpenWaitsOutALockInsteadOfFailing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concord.db")
	key := bytes.Repeat([]byte{0x42}, 32)

	// A rollback-mode database, so the Open below has to perform the WAL switch
	// that needs exclusivity. Opening straight into WAL would make the switch a
	// no-op and the race unreachable.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if _, err := raw.Exec(`PRAGMA journal_mode=DELETE; CREATE TABLE probe (x INTEGER);`); err != nil {
		t.Fatalf("prepare rollback-mode db: %v", err)
	}

	// Hold it exclusively, then let go while Open is mid-flight.
	if _, err := raw.Exec(`BEGIN EXCLUSIVE`); err != nil {
		t.Fatalf("lock: %v", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = raw.Exec(`COMMIT`)
		close(released)
	}()

	s, err := Open(path, key)
	if err != nil {
		t.Fatalf("Open while the database was exclusively locked: %v", err)
	}
	<-released
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// Retention pruning removes old messages but must leave the two kinds somebody
// deliberately kept: pinned (the guild said keep) and saved (this device's
// owner said keep). Getting that wrong eats the messages people care most about.
func TestPruneMessagesBeforeSparesPinnedAndSaved(t *testing.T) {
	s, _ := openTestStore(t)
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)

	save := func(id string, at time.Time) {
		t.Helper()
		if _, err := s.SaveMessage(domain.Message{
			ID: id, ChannelID: "c1", Sender: []byte("k"), Content: "x", Sent: at,
		}); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	save("old-plain", old)
	save("old-pinned", old)
	save("old-saved", old)
	save("recent", recent)
	save("other-channel", old)
	if _, err := s.db.Exec(`UPDATE messages SET channel_id='c2' WHERE id='other-channel'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE messages SET pinned=1 WHERE id='old-pinned'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO saved_messages (message_id, channel_id, at) VALUES ('old-saved','c1',1)`); err != nil {
		t.Fatal(err)
	}
	// A reaction on the row that is about to go: nothing in this schema
	// cascades, so an orphan here would outlive its message forever.
	if _, err := s.db.Exec(`INSERT INTO reactions (message_id, fingerprint, emoji) VALUES ('old-plain','fpr','x')`); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().Add(-24 * time.Hour).UnixNano()
	n, err := s.PruneMessagesBefore([]string{"c1"}, cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d rows, want 1 (only old-plain)", n)
	}

	left := map[string]bool{}
	rows, err := s.db.Query(`SELECT id FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left[id] = true
	}
	for _, want := range []string{"old-pinned", "old-saved", "recent", "other-channel"} {
		if !left[want] {
			t.Errorf("%s was pruned and should not have been", want)
		}
	}
	if left["old-plain"] {
		t.Error("old-plain survived the cutoff")
	}

	var orphans int
	if err := s.db.QueryRow(`SELECT count(*) FROM reactions WHERE message_id='old-plain'`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("left %d orphaned reaction rows", orphans)
	}
}

// An empty channel list must be a no-op, not "prune everything" — the natural
// SQL for it (an empty IN clause) is easy to get wrong in the other direction.
func TestPruneMessagesBeforeIgnoresEmptyChannelList(t *testing.T) {
	s, _ := openTestStore(t)
	if _, err := s.SaveMessage(domain.Message{
		ID: "m", ChannelID: "c1", Sender: []byte("k"), Content: "x",
		Sent: time.Now().Add(-72 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneMessagesBefore(nil, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 0 {
		t.Fatalf("pruned %d rows with no channels given, want 0", n)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("message count %d, want 1 — an empty channel list deleted data", count)
	}
}

// A sweep larger than one batch must delete everything, not stop at the limit.
// The batching exists so a long history does not hold the single connection for
// one enormous transaction; it would be worse than useless if it also left rows
// behind and reported success.
func TestPruneMessagesBeforeLoopsPastOneBatch(t *testing.T) {
	s, _ := openTestStore(t)
	old := time.Now().Add(-48 * time.Hour)
	const n = pruneBatch + 250
	for i := 0; i < n; i++ {
		if _, err := s.SaveMessage(domain.Message{
			ID: fmt.Sprintf("m%05d", i), ChannelID: "c1",
			Sender: []byte("k"), Content: "x", Sent: old.Add(time.Duration(i) * time.Millisecond),
		}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	got, err := s.PruneMessagesBefore([]string{"c1"}, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if got != n {
		t.Fatalf("pruned %d rows, want all %d — the batch loop stopped early", got, n)
	}
	var left int
	if err := s.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d messages survived a sweep that reported completion", left)
	}
}

// Retention must also clear the bodies of soft-deleted messages. Deleting a
// message sets deleted = 1 but deliberately KEEPS content_enc so a moderator can
// reveal the original (see MarkDeleted / EmptyTrash) — which means retained
// deleted content is the one store that grows without bound and holds exactly
// the text people tried to take back. A retention policy that skipped it would
// leave the most sensitive rows as the only permanent ones.
func TestPruneMessagesBeforeClearsRetainedDeletedBodies(t *testing.T) {
	s, _ := openTestStore(t)
	old := time.Now().Add(-48 * time.Hour)
	if _, err := s.SaveMessage(domain.Message{
		ID: "regretted", ChannelID: "c1", Sender: []byte("k"),
		Content: "something they took back", Sent: old,
	}); err != nil {
		t.Fatal(err)
	}
	// Soft delete: flagged, but the body is still on disk by design.
	if _, _, err := s.MarkDeleted("regretted", []byte("k"), true); err != nil {
		t.Fatal(err)
	}
	var body []byte
	if err := s.db.QueryRow(`SELECT content_enc FROM messages WHERE id='regretted'`).Scan(&body); err != nil {
		t.Fatalf("precondition: the body should still be retained after a delete: %v", err)
	}

	if _, err := s.PruneMessagesBefore([]string{"c1"}, time.Now().Add(-24*time.Hour).UnixNano()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM messages WHERE id='regretted'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("a soft-deleted message outlived the retention cutoff, so its retained body did too")
	}
}
