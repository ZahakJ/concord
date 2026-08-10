package identity

import (
	"crypto/rand"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"
)

// sealedBoxVersion is the envelope format, separate from the keystore's own
// version: the two happen to share a construction, not a lifecycle.
const sealedBoxVersion = 1

// sealedBox is a self-describing passphrase-sealed envelope. The KDF parameters
// travel with the ciphertext for the same reason the keystore records them —
// they can be raised later without stranding anything already written.
type sealedBox struct {
	Version      int    `json:"version"`
	KDF          string `json:"kdf"`
	Salt         []byte `json:"salt"`
	ArgonTime    uint32 `json:"argon_time"`
	ArgonMemory  uint32 `json:"argon_memory"`
	ArgonThreads uint8  `json:"argon_threads"`
	Nonce        []byte `json:"nonce"`
	Ciphertext   []byte `json:"ciphertext"`
}

// SealWithPassphrase seals arbitrary bytes under a passphrase, using the same
// Argon2id-then-secretbox construction that protects the identity keystore.
//
// It exists so that anything else needing passphrase-encrypted data at rest —
// a history archive, for instance — cannot quietly grow a second, weaker
// scheme, or pin the Argon2 parameters at whatever they happened to be on the
// day it was written. There is one construction here and one place to raise it.
func SealWithPassphrase(passphrase string, plaintext []byte) ([]byte, error) {
	var salt [saltSize]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return nil, err
	}
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	key := deriveKeyWithParams(passphrase, salt[:], argonTime, argonMemory, argonThreads)
	box := sealedBox{
		Version:      sealedBoxVersion,
		KDF:          "argon2id",
		Salt:         salt[:],
		ArgonTime:    argonTime,
		ArgonMemory:  argonMemory,
		ArgonThreads: argonThreads,
		Nonce:        nonce[:],
		Ciphertext:   secretbox.Seal(nil, plaintext, &nonce, &key),
	}
	return json.Marshal(box)
}

// OpenWithPassphrase reverses SealWithPassphrase. A wrong passphrase and a
// corrupted envelope are indistinguishable to secretbox, so both come back as
// ErrWrongPassphrase.
func OpenWithPassphrase(passphrase string, sealed []byte) ([]byte, error) {
	var box sealedBox
	if err := json.Unmarshal(sealed, &box); err != nil {
		return nil, fmt.Errorf("identity: not a sealed envelope: %w", err)
	}
	if box.KDF != "argon2id" {
		return nil, fmt.Errorf("identity: unsupported key derivation %q", box.KDF)
	}
	if len(box.Nonce) != nonceSize {
		return nil, ErrWrongPassphrase
	}
	key := deriveKeyWithParams(passphrase, box.Salt, box.ArgonTime, box.ArgonMemory, box.ArgonThreads)
	var nonce [nonceSize]byte
	copy(nonce[:], box.Nonce)
	out, ok := secretbox.Open(nil, box.Ciphertext, &nonce, &key)
	if !ok {
		return nil, ErrWrongPassphrase
	}
	return out, nil
}
