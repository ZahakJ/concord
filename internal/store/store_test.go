package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

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
		if err := s.SaveMessage(m); err != nil {
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
	if err := s.SaveMessage(m); err != nil {
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
	if err := s1.SaveMessage(m); err != nil {
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

func TestMarkDeletedAuthorization(t *testing.T) {
	s, _ := openTestStore(t)
	author := []byte("alice-key")
	m, _ := domain.NewMessage("chan-1", author, "secret plan")
	if err := s.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	// A different peer cannot delete it.
	if _, ok, err := s.MarkDeleted(m.ID, []byte("eve-key")); err != nil || ok {
		t.Fatalf("non-author delete: ok=%v err=%v (want ok=false)", ok, err)
	}
	msgs, _ := s.Messages("chan-1", 0)
	if len(msgs) != 1 || msgs[0].Deleted || msgs[0].Content != "secret plan" {
		t.Fatal("message should be intact after unauthorized delete attempt")
	}

	// The author can.
	deleted, ok, err := s.MarkDeleted(m.ID, author)
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
}

func TestSaveMessageIdempotent(t *testing.T) {
	s, _ := openTestStore(t)
	m, _ := domain.NewMessage("chan-1", []byte("alice"), "hi")
	if err := s.SaveMessage(m); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := s.SaveMessage(m); err != nil {
		t.Fatalf("second save (dup): %v", err)
	}
	msgs, _ := s.Messages("chan-1", 0)
	if len(msgs) != 1 {
		t.Fatalf("duplicate message stored: got %d, want 1", len(msgs))
	}
}
