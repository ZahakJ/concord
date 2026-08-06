package app

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/zahak/concord/internal/identity"
	"github.com/zahak/concord/internal/store"
)

// signedStory builds a valid record signed by its author, exactly the way
// PostStory does.
func signedStory(t *testing.T, id *identity.Identity, storyID, guildID string, now int64) storyRecord {
	t.Helper()
	rec := storyRecord{
		StoryID: storyID, GuildID: guildID,
		Author: identity.FingerprintOf(id.PublicKey()),
		Preset: "preset:galaxy", Caption: "session zero at nine",
		Color1: "#aabbcc", Color2: "#112233",
		PostedAt: now, ExpiresAt: now + int64(storyLifetime/time.Second),
	}
	rec.Sig = id.Sign(rec.signingBytes())
	return rec
}

// storyTestService is a Service with just a real store behind it — ingestStory
// touches nothing else, so the verification gates can be driven directly the
// way the govstate tests drive replay.
func storyTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "concord.db"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Service{store: st}
}

func TestStorySignVerifyRoundTrip(t *testing.T) {
	author := mustID(t)
	now := time.Now().Unix()
	rec := signedStory(t, author, "st1", "g1", now)

	if !rec.verifySig(author.PublicKey()) {
		t.Fatal("a record signed by its author must verify")
	}
	// Every signed field is load-bearing: changing any of them must kill the
	// signature — including the caption, which is covered via its sha256.
	tampered := rec
	tampered.Caption = "midnight instead"
	if tampered.verifySig(author.PublicKey()) {
		t.Fatal("a tampered caption must break the signature")
	}
	tampered = rec
	tampered.Preset = "preset:dune"
	if tampered.verifySig(author.PublicKey()) {
		t.Fatal("a tampered preset must break the signature")
	}
	tampered = rec
	tampered.ExpiresAt += 60
	if tampered.verifySig(author.PublicKey()) {
		t.Fatal("a stretched expiry must break the signature")
	}
}

func TestForgedAuthorStoryRejected(t *testing.T) {
	alice := mustID(t)
	mallory := mustID(t)
	now := time.Now().Unix()

	// Mallory crafts a "story from Alice": Alice's fingerprint in Author,
	// Mallory's key under the signature — the README §12 forgery shape.
	forged := signedStory(t, mallory, "st-forged", "g1", now)
	forged.Author = identity.FingerprintOf(alice.PublicKey())
	forged.Sig = mallory.Sign(forged.signingBytes())

	// Against Alice's roster key the signature simply doesn't verify…
	if forged.verifySig(alice.PublicKey()) {
		t.Fatal("Mallory's signature must not verify under Alice's key")
	}
	// …and Mallory's own key is no better: verifySig binds key to the CLAIMED
	// author, so a valid signature by the wrong person is equally dead.
	if forged.verifySig(mallory.PublicKey()) {
		t.Fatal("a valid signature by a non-author key must be rejected")
	}

	s := storyTestService(t)
	if s.ingestStory("g1", forged, alice.PublicKey(), now) {
		t.Fatal("ingest must reject the forged record")
	}
	if rows, err := s.GuildStories("g1"); err != nil || len(rows) != 0 {
		t.Fatalf("forged story reached the store: %v, %v", rows, err)
	}
}

func TestExpiredStoryDropped(t *testing.T) {
	author := mustID(t)
	now := time.Now().Unix()
	s := storyTestService(t)

	// Correctly signed, merely old: both arrival paths replay old records
	// (gossip re-delivery, sync snapshots), so expiry is checked at ingest.
	old := signedStory(t, author, "st-old", "g1", now-2*int64(storyLifetime/time.Second))
	if s.ingestStory("g1", old, author.PublicKey(), now) {
		t.Fatal("an expired record must be dropped at ingest")
	}

	// A claim of a longer-than-allowed life is rejected even while "unexpired":
	// a peer must not be able to park a record on our disk past storyLifetime.
	immortal := storyRecord{
		StoryID: "st-immortal", GuildID: "g1",
		Author: identity.FingerprintOf(author.PublicKey()),
		Preset: "preset:galaxy", PostedAt: now, ExpiresAt: now + 10*int64(storyLifetime/time.Second),
	}
	immortal.Sig = author.Sign(immortal.signingBytes())
	if s.ingestStory("g1", immortal, author.PublicKey(), now) {
		t.Fatal("a record claiming more than storyLifetime must be dropped")
	}
	if rows, err := s.GuildStories("g1"); err != nil || len(rows) != 0 {
		t.Fatalf("dropped stories reached the store: %v, %v", rows, err)
	}
}

func TestStoryReplayIdempotence(t *testing.T) {
	author := mustID(t)
	now := time.Now().Unix()
	s := storyTestService(t)
	rec := signedStory(t, author, "st-once", "g1", now)

	if !s.ingestStory("g1", rec, author.PublicKey(), now) {
		t.Fatal("first ingest of a valid record must store it")
	}
	// The same record again — gossip re-delivery, an overlapping sync — is a
	// clean no-op: one row, and "false" so callers don't re-emit events.
	if s.ingestStory("g1", rec, author.PublicKey(), now) {
		t.Fatal("replaying the same record must not report a new story")
	}
	rows, err := s.GuildStories("g1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("replay produced %d rows (err %v), want exactly 1", len(rows), err)
	}
	if rows[0].Caption != rec.Caption || rows[0].Author != rec.Author {
		t.Fatalf("stored story mismatch: %+v", rows[0])
	}

	// The signature covers the guild, so the identical record replayed into
	// another guild's lane is dead on arrival — even from its own author.
	if s.ingestStory("g2", rec, author.PublicKey(), now) {
		t.Fatal("a story signed for g1 must not be accepted under g2")
	}
}
