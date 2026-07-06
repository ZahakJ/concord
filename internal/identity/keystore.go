package identity

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/nacl/secretbox"
)

// Keystore persistence: the identity seed is the crown-jewel secret, so it is
// never written to disk in the clear. We derive a symmetric key from a user
// passphrase with Argon2id and seal the seed with NaCl secretbox
// (XSalsa20-Poly1305). The salt, nonce and KDF parameters are stored alongside
// the ciphertext so the file is self-describing on load.

const (
	keystoreVersion = 1
	saltSize        = 16
	nonceSize       = 24 // secretbox nonce
	keySize         = 32 // secretbox key

	// Argon2id parameters. Interactive-login tuned: ~64 MiB, 3 passes. These
	// are recorded in the file so they can be raised later without breaking
	// existing keystores.
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB => 64 MiB
	argonThreads = 4
)

// ErrWrongPassphrase is returned when decryption fails, which for secretbox is
// indistinguishable from a corrupted file — either way the seed can't be
// recovered with the supplied passphrase.
var ErrWrongPassphrase = errors.New("identity: wrong passphrase or corrupted keystore")

// keystoreFile is the on-disk JSON envelope. All binary fields are base64 via
// encoding/json's []byte handling.
type keystoreFile struct {
	Version      int    `json:"version"`
	KDF          string `json:"kdf"`
	Salt         []byte `json:"salt"`
	ArgonTime    uint32 `json:"argon_time"`
	ArgonMemory  uint32 `json:"argon_memory"`
	ArgonThreads uint8  `json:"argon_threads"`
	Nonce        []byte `json:"nonce"`
	Ciphertext   []byte `json:"ciphertext"`
}

// SaveKeystore encrypts id's seed under passphrase and writes it to path,
// creating parent directories as needed. The file is written 0600.
func SaveKeystore(path, passphrase string, id *Identity) error {
	if passphrase == "" {
		return errors.New("identity: passphrase must not be empty")
	}

	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("identity: read salt: %w", err)
	}
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("identity: read nonce: %w", err)
	}

	key := deriveKey(passphrase, salt)
	sealed := secretbox.Seal(nil, id.Seed(), &nonce, &key)

	env := keystoreFile{
		Version:      keystoreVersion,
		KDF:          "argon2id",
		Salt:         salt,
		ArgonTime:    argonTime,
		ArgonMemory:  argonMemory,
		ArgonThreads: argonThreads,
		Nonce:        nonce[:],
		Ciphertext:   sealed,
	}
	blob, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("identity: marshal keystore: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("identity: create keystore dir: %w", err)
		}
	}
	// Write to a temp file then rename, so a crash mid-write can never leave a
	// half-written (unopenable) keystore.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("identity: write keystore: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("identity: finalize keystore: %w", err)
	}
	return nil
}

// LoadKeystore reads and decrypts the identity at path using passphrase.
func LoadKeystore(path, passphrase string) (*Identity, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err // callers use os.IsNotExist to detect first-run
	}

	var env keystoreFile
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, fmt.Errorf("identity: parse keystore: %w", err)
	}
	if env.Version != keystoreVersion {
		return nil, fmt.Errorf("identity: unsupported keystore version %d", env.Version)
	}
	if env.KDF != "argon2id" {
		return nil, fmt.Errorf("identity: unsupported kdf %q", env.KDF)
	}
	if len(env.Nonce) != nonceSize {
		return nil, errors.New("identity: bad nonce length in keystore")
	}

	key := deriveKeyWithParams(passphrase, env.Salt, env.ArgonTime, env.ArgonMemory, env.ArgonThreads)
	var nonce [nonceSize]byte
	copy(nonce[:], env.Nonce)

	seed, ok := secretbox.Open(nil, env.Ciphertext, &nonce, &key)
	if !ok {
		return nil, ErrWrongPassphrase
	}
	return FromSeed(seed)
}

// LoadOrCreate loads the identity at path, or generates and persists a new one
// if the file does not exist. This is the normal app-startup entry point.
// The bool result reports whether a new identity was created.
func LoadOrCreate(path, passphrase string) (*Identity, bool, error) {
	id, err := LoadKeystore(path, passphrase)
	if err == nil {
		return id, false, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, err
	}
	id, err = Generate()
	if err != nil {
		return nil, false, err
	}
	if err := SaveKeystore(path, passphrase, id); err != nil {
		return nil, false, err
	}
	return id, true, nil
}

func deriveKey(passphrase string, salt []byte) [keySize]byte {
	return deriveKeyWithParams(passphrase, salt, argonTime, argonMemory, argonThreads)
}

func deriveKeyWithParams(passphrase string, salt []byte, time, memory uint32, threads uint8) [keySize]byte {
	dk := argon2.IDKey([]byte(passphrase), salt, time, memory, threads, keySize)
	var key [keySize]byte
	copy(key[:], dk)
	return key
}
