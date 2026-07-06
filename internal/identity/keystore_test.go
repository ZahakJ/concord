package identity

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "keystore.json")
	id, _ := Generate()

	if err := SaveKeystore(path, "correct horse battery staple", id); err != nil {
		t.Fatalf("SaveKeystore: %v", err)
	}

	loaded, err := LoadKeystore(path, "correct horse battery staple")
	if err != nil {
		t.Fatalf("LoadKeystore: %v", err)
	}
	if !bytes.Equal(id.PublicKey(), loaded.PublicKey()) {
		t.Fatal("loaded identity differs from saved identity")
	}
}

func TestKeystoreFileIsEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	if err := SaveKeystore(path, "hunter2", id); err != nil {
		t.Fatalf("SaveKeystore: %v", err)
	}

	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read keystore: %v", err)
	}
	// The raw seed bytes must never appear in the on-disk file.
	if bytes.Contains(blob, id.Seed()) {
		t.Fatal("plaintext seed found in keystore file")
	}

	// File must be 0600.
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("keystore perms = %o, want 600", perm)
	}
}

func TestWrongPassphraseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	if err := SaveKeystore(path, "right", id); err != nil {
		t.Fatalf("SaveKeystore: %v", err)
	}
	if _, err := LoadKeystore(path, "wrong"); err != ErrWrongPassphrase {
		t.Fatalf("LoadKeystore with wrong passphrase: got %v, want ErrWrongPassphrase", err)
	}
}

func TestEmptyPassphraseRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	if err := SaveKeystore(path, "", id); err == nil {
		t.Fatal("expected error saving with empty passphrase")
	}
}

func TestLoadOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")

	// First call creates.
	id1, created, err := LoadOrCreate(path, "pw")
	if err != nil {
		t.Fatalf("LoadOrCreate (create): %v", err)
	}
	if !created {
		t.Fatal("expected created=true on first call")
	}

	// Second call loads the same identity.
	id2, created, err := LoadOrCreate(path, "pw")
	if err != nil {
		t.Fatalf("LoadOrCreate (load): %v", err)
	}
	if created {
		t.Fatal("expected created=false on second call")
	}
	if id1.Fingerprint() != id2.Fingerprint() {
		t.Fatal("identity not stable across LoadOrCreate calls")
	}
}
