package identity

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"sync"
)

// The keystore's clear fields.
//
// Some of the keystore is readable without the passphrase on purpose: the
// key derivation settings and salt (needed to derive the key at all) and the
// account's public key (so a locked device can say whose account it holds).
// The sealed seeds are authenticated by secretbox; these fields were
// authenticated by nothing, so anyone who could write the file could make a
// locked device name another account, and nothing would ever notice.
//
// clear_tag is an HMAC over them under a key derived from the
// passphrase-derived key. Every seal writes it, every unlock checks it. A
// mismatch is tampering (or a bug): the unlock logs it, does not trust the
// clear fields, re-seals them correctly from the decrypted seed through the
// compare-and-swap writer, and tells the user once. A keystore without a
// tag (written before it existed) opens as before and gains one at its next
// re-seal.
//
// What it cannot do: verify anything BEFORE an unlock, since the key is the
// passphrase's. Until then the clear fields are a claim, and the flows that
// run without the passphrase treat them as one (see app.MatchDeviceAccount).

// labelState says what an unlock found out about the clear fields.
type labelState uint8

const (
	labelUntagged labelState = iota // no tag: written before tags existed
	labelVerified                   // tag present and correct
	labelAltered                    // tag present and wrong: the clear fields were changed
)

// clearFieldsV1 is what clear_tag covers, in a fixed order. The nonces and
// ciphertexts are not here because secretbox already authenticates them.
type clearFieldsV1 struct {
	Version      int    `json:"version"`
	KDF          string `json:"kdf"`
	Salt         []byte `json:"salt"`
	ArgonTime    uint32 `json:"argon_time"`
	ArgonMemory  uint32 `json:"argon_memory"`
	ArgonThreads uint8  `json:"argon_threads"`
	AccountPub   []byte `json:"account_pub"`
}

func clearTag(key *[keySize]byte, env *keystoreFile) []byte {
	sub := hmac.New(sha256.New, key[:])
	sub.Write([]byte("concord keystore clear fields v1"))
	m := hmac.New(sha256.New, sub.Sum(nil))
	blob, _ := json.Marshal(clearFieldsV1{
		Version: env.Version, KDF: env.KDF, Salt: env.Salt,
		ArgonTime: env.ArgonTime, ArgonMemory: env.ArgonMemory, ArgonThreads: env.ArgonThreads,
		AccountPub: env.AccountPub,
	})
	m.Write(blob)
	return m.Sum(nil)
}

func checkClearTag(key *[keySize]byte, env *keystoreFile) labelState {
	if len(env.ClearTag) == 0 {
		return labelUntagged
	}
	if hmac.Equal(env.ClearTag, clearTag(key, env)) {
		return labelVerified
	}
	return labelAltered
}

// Facts this process has established by unlocking, kept for the flows that
// run without the passphrase. Guarded by unlockedMu.
var (
	unlockedMu sync.Mutex
	// verified maps a keystore path to the account a keystore with exactly
	// these bytes (sha256) was seen to hold, by decrypting it or writing it.
	verified = map[string]verifiedKeystore{}
	// altered marks keystores whose clear fields an unlock found changed,
	// until the user has been told (TakeLabelAltered).
	altered = map[string]bool{}
)

type verifiedKeystore struct {
	sum [32]byte
	pub ed25519.PublicKey
}

func noteVerified(path string, blob []byte, pub ed25519.PublicKey) {
	unlockedMu.Lock()
	verified[path] = verifiedKeystore{sha256.Sum256(blob), pub}
	unlockedMu.Unlock()
}

// VerifiedAccount returns the account the keystore at path holds if this
// process has established it (by unlocking or writing exactly the bytes
// that are there now), or nil. Unlike the clear account key, it cannot have
// been planted by whoever last wrote the file.
func VerifiedAccount(path string) ed25519.PublicKey {
	blob, err := ReadKeystore(path)
	if err != nil {
		return nil
	}
	unlockedMu.Lock()
	defer unlockedMu.Unlock()
	if v, ok := verified[path]; ok && v.sum == sha256.Sum256(blob) {
		return v.pub
	}
	return nil
}

// TakeLabelAltered reports, once, that an unlock in this process found the
// keystore's clear fields altered (and corrected them).
func TakeLabelAltered(path string) bool {
	unlockedMu.Lock()
	defer unlockedMu.Unlock()
	was := altered[path]
	delete(altered, path)
	return was
}

func noteAltered(path string) {
	unlockedMu.Lock()
	altered[path] = true
	unlockedMu.Unlock()
}
