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

func rewriteField(t *testing.T, path, field, value string) {
	t.Helper()
	var env map[string]json.RawMessage
	if err := json.Unmarshal(mustRead(t, path), &env); err != nil {
		t.Fatal(err)
	}
	env[field] = json.RawMessage(value)
	out, _ := json.Marshal(env)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func labelOf(t *testing.T, path, pass string) labelState {
	t.Helper()
	id, err := LoadKeystore(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	return id.label
}

// A clear field changed by someone with the device's files is noticed at the
// next unlock: the user is told once, and the field is corrected from what
// the keystore seals. The version is changed here because nothing but the
// tag covers it (the account key is also compared with the seed).
func TestAlteredClearFieldIsDetectedAtUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	if got := labelOf(t, path, "pw"); got != labelVerified {
		t.Fatalf("a new keystore's clear fields are not verified: %v", got)
	}
	rewriteField(t, path, "version", "1")
	unlockWithReseal(t, path, "pw", nil, resealHooks{})
	if !TakeLabelAltered(path) {
		t.Fatal("the unlock did not notice the altered clear field")
	}
	if TakeLabelAltered(path) {
		t.Fatal("the user is told more than once")
	}
	if got := labelOf(t, path, "pw"); got != labelVerified {
		t.Fatalf("the altered field was not corrected: %v", got)
	}
}

// v060Keystore writes a keystore exactly as v0.60.0 did: NFKC passphrase,
// v2 with the device seed, the clear account key, no tag.
func v060Keystore(t *testing.T, path, pass string, id *Identity) {
	t.Helper()
	salt := make([]byte, saltSize)
	_, _ = rand.Read(salt)
	var nonce [nonceSize]byte
	_, _ = rand.Read(nonce[:])
	key := deriveKeyWithParams(NormalizePassphrase(pass), salt, 3, 64*1024, 4)
	env := keystoreFile{
		Version: 2, KDF: "argon2id", Salt: salt,
		ArgonTime: 3, ArgonMemory: 64 * 1024, ArgonThreads: 4,
		Nonce: nonce[:], Ciphertext: secretbox.Seal(nil, id.Seed(), &nonce, &key),
		AccountPub: id.PublicKey(),
	}
	var dnonce [nonceSize]byte
	_, _ = rand.Read(dnonce[:])
	env.DeviceNonce = dnonce[:]
	env.DeviceCiphertext = secretbox.Seal(nil, id.DeviceSeed(), &dnonce, &key)
	blob, _ := json.MarshalIndent(env, "", "  ")
	if bytes.Contains(blob, []byte("clear_tag")) {
		t.Fatal("the v0.60.0 writer does not write a tag")
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A keystore written by v0.60.0 (no tag) opens, is not reported as altered,
// and gains the tag at its first unlock's re-seal.
func TestKeystoreWithoutTagOpensAndGainsOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	v060Keystore(t, path, "pässword", id)
	if got := labelOf(t, path, "pässword"); got != labelUntagged {
		t.Fatalf("a v0.60.0 keystore reads as %v", got)
	}
	unlockWithReseal(t, path, "pässword", nil, resealHooks{})
	if TakeLabelAltered(path) {
		t.Fatal("a keystore that simply predates the tag was reported as altered")
	}
	if got := labelOf(t, path, "pässword"); got != labelVerified {
		t.Fatalf("the re-seal did not add the tag: %v", got)
	}
	got, err := LoadKeystore(path, "pässword")
	if err != nil || got.Fingerprint() != id.Fingerprint() {
		t.Fatalf("after the upgrade: %v", err)
	}
}

// The tag is re-made under the new passphrase when it changes.
func TestTagSurvivesAPassphraseChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(path, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if got := labelOf(t, path, "new"); got != labelVerified {
		t.Fatalf("after a passphrase change the clear fields read as %v", got)
	}
}
