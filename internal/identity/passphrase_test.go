package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/nacl/secretbox"
)

// The same visible passphrase arrives as different code points from different
// input methods. Sealed in one form, it must open in the other.
func TestPassphraseOpensInAnyUnicodeForm(t *testing.T) {
	nfc := "\u00dcn\u00efc\u00f8d\u00e9 \u00e9 \u03bb \u0643\u0644\u0645\u0629"
	nfd := "U\u0308ni\u0308c\u00f8de\u0301 e\u0301 \u03bb \u0643\u0644\u0645\u0629" // decomposed accents
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	if err := SaveKeystore(path, nfc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeystore(path, nfd); err != nil {
		t.Fatalf("decomposed form of the sealing passphrase refused: %v", err)
	}
	if _, err := LoadKeystore(path, "１２３"); err != ErrWrongPassphrase {
		t.Fatalf("an unrelated passphrase opened the keystore: %v", err)
	}

	// Full-width digits from an IME left in wide mode fold to ASCII.
	if err := SaveKeystore(path, "pin 123", id); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeystore(path, "pin １２３"); err != nil {
		t.Fatalf("full-width digits refused: %v", err)
	}
}

// A keystore sealed by an older build holds the RAW bytes that were typed,
// unnormalised. It must keep opening with exactly that input, and with the
// other composition too.
func TestLegacyRawSealedKeystoreStillOpens(t *testing.T) {
	nfd := "e\u0301te\u0301"
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	sealRaw(t, path, nfd, id)
	if _, err := LoadKeystore(path, nfd); err != nil {
		t.Fatalf("raw legacy passphrase refused: %v", err)
	}
	if _, err := LoadKeystore(path, "été"); err != nil {
		t.Fatalf("composed form refused for a decomposed legacy keystore: %v", err)
	}
}

// sealRaw writes a keystore the way builds before normalisation did: the KDF
// fed the typed bytes as they were.
func sealRaw(t *testing.T, path, pass string, id *Identity) {
	t.Helper()
	salt := make([]byte, saltSize)
	_, _ = rand.Read(salt)
	var nonce [nonceSize]byte
	_, _ = rand.Read(nonce[:])
	key := deriveKey(pass, salt)
	env := keystoreFile{
		Version: 1, KDF: "argon2id", Salt: salt,
		ArgonTime: argonTime, ArgonMemory: argonMemory, ArgonThreads: argonThreads,
		Nonce: nonce[:], Ciphertext: secretbox.Seal(nil, id.Seed(), &nonce, &key),
	}
	blob, _ := json.Marshal(env)
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestChangePassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	if err := SaveKeystore(path, "old one", id); err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(path, "not it", "new one"); err != ErrWrongPassphrase {
		t.Fatalf("wrong current passphrase: got %v", err)
	}
	if _, err := LoadKeystore(path, "old one"); err != nil {
		t.Fatalf("a refused change must leave the old passphrase working: %v", err)
	}
	if err := ChangePassphrase(path, "old one", "new one"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeystore(path, "old one"); err != ErrWrongPassphrase {
		t.Fatalf("old passphrase still opens after a change: %v", err)
	}
	got, err := LoadKeystore(path, "new one")
	if err != nil {
		t.Fatalf("new passphrase refused: %v", err)
	}
	if !bytes.Equal(got.Seed(), id.Seed()) || !bytes.Equal(got.DeviceSeed(), id.DeviceSeed()) {
		t.Fatal("change lost the account or device seed")
	}
}

// A change that fails while writing must leave the keystore readable under the
// old passphrase — never a half-written file and never nothing.
func TestChangePassphraseFailureLeavesOldKeystore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	id, _ := Generate()
	if err := SaveKeystore(path, "old one", id); err != nil {
		t.Fatal(err)
	}
	// Block the temp file the new keystore is written to.
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(path, "old one", "new one"); err == nil {
		t.Fatal("change reported success with its temp file blocked")
	}
	if _, err := LoadKeystore(path, "old one"); err != nil {
		t.Fatalf("failed change left the keystore unreadable: %v", err)
	}
}

func TestAccountPubReadableWithoutPassphrase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	sealRaw(t, path, "pw", id)
	if pub, err := ReadAccountPub(path); err != nil || pub != nil {
		t.Fatalf("legacy keystore: got %x, %v; want nil, nil", pub, err)
	}
	// One unlock upgrades the file to name its account.
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	pub, err := ReadAccountPub(path)
	if err != nil || !bytes.Equal(pub, id.PublicKey()) {
		t.Fatalf("after unlock: got %x, %v; want %x", pub, err, id.PublicKey())
	}
}
