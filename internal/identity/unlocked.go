package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"sync"
)

// What this process has established by unlocking, for the flows that run
// without the passphrase. A locked device's files can claim anything about
// whose account they hold (the keystore's clear account key, a linked
// device's certificate): those are advisory, and anyone able to write the
// data directory can change them. The check is the unlock itself, which
// compares the clear account key with the account the sealed seed decrypts
// to (LoadOrCreate) and corrects it, telling the user once.

// Facts this process has established by unlocking, kept for the flows that
// run without the passphrase. Guarded by unlockedMu.
var (
	unlockedMu sync.Mutex
	// verified maps a keystore path to the account a keystore with exactly
	// these bytes (sha256) was seen to hold, by decrypting it or writing it.
	verified = map[string]verifiedKeystore{}
	// altered marks keystores whose clear account key an unlock found not to
	// match the sealed seed, until the user has been told (TakeLabelAltered).
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
// keystore's clear account key not matching its sealed seed (and corrected it).
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
