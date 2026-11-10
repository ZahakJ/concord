package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/text/unicode/norm"
)

// Keystore persistence: the identity seed is the crown-jewel secret, so it is
// never written to disk in the clear. We derive a symmetric key from a user
// passphrase with Argon2id and seal the seed with NaCl secretbox
// (XSalsa20-Poly1305). The salt, nonce and KDF parameters are stored alongside
// the ciphertext so the file is self-describing on load.

const (
	// keystoreVersion 2 adds an optional sealed device seed alongside the account
	// seed (see identity.go). v1 files (account seed only) still load; they are
	// upgraded to v2 on first unlock (LoadOrCreate generates a device seed and
	// re-saves). Both versions are accepted on load.
	keystoreVersion = 2
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
	// v2: the per-device seed, sealed under the same passphrase-derived key with
	// its own nonce. Absent in v1 files (and in v2 files that predate the device
	// seed being set).
	DeviceNonce      []byte `json:"device_nonce,omitempty"`
	DeviceCiphertext []byte `json:"device_ciphertext,omitempty"`
	// AccountPub is the account's PUBLIC key, in the clear. It is the only
	// thing a locked device can say about whose account it holds, and the
	// flows that replace the identity on a locked device (recovery phrase,
	// re-link) need exactly that to tell "the same account under a new
	// passphrase" from "a different account that would erase this one". It is
	// no secret — every peer the account talks to holds it — and it is never
	// trusted for anything but that comparison: the seed is still what
	// unlocks. Absent in files written before it existed; LoadOrCreate adds it
	// on the next unlock.
	AccountPub []byte `json:"account_pub,omitempty"`
}

// NormalizePassphrase is the form a passphrase is sealed under. The same
// visible text reaches us as different code points depending on the input
// method — a composed "é" from one keyboard, "e" plus a combining accent from
// another, a full-width digit from an IME left in wide mode — and Argon2 sees
// bytes, so a passphrase typed correctly on a new phone used to be "wrong".
// NFKC folds all of those to one form.
func NormalizePassphrase(p string) string { return norm.NFKC.String(p) }

// passphraseForms lists what to try when opening a keystore, raw input first.
// Keystores sealed before normalisation hold whatever bytes were typed then,
// so the raw form has to keep working, and the decomposed and composed forms
// cover a file sealed from one input method and opened from the other. An
// ASCII passphrase is the same in every form and costs a single derivation.
func passphraseForms(p string) []string {
	out := []string{p}
	for _, f := range []string{norm.NFKC.String(p), norm.NFC.String(p), norm.NFD.String(p)} {
		dup := false
		for _, o := range out {
			if o == f {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, f)
		}
	}
	return out
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

	key := deriveKey(NormalizePassphrase(passphrase), salt)
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
		AccountPub:   id.PublicKey(),
	}
	// Seal the device seed too, under the same derived key with a fresh nonce.
	if ds := id.DeviceSeed(); ds != nil {
		var dnonce [nonceSize]byte
		if _, err := rand.Read(dnonce[:]); err != nil {
			return fmt.Errorf("identity: read device nonce: %w", err)
		}
		env.DeviceNonce = dnonce[:]
		env.DeviceCiphertext = secretbox.Seal(nil, ds, &dnonce, &key)
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
	// half-written (unopenable) keystore. The sync before the rename is what
	// makes that true across a power cut: without it the rename can reach the
	// disk before the bytes it points at, and a passphrase change interrupted
	// there would leave neither the old file nor a readable new one.
	tmp := path + ".tmp"
	if err := writeSynced(tmp, blob); err != nil {
		return fmt.Errorf("identity: write keystore: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("identity: finalize keystore: %w", err)
	}
	return nil
}

func writeSynced(path string, blob []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(blob); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ReadAccountPub returns the account public key recorded in the clear in the
// keystore at path, or nil when the file predates that field. It needs no
// passphrase, which is the point: it is how a LOCKED device names the account
// it holds.
func ReadAccountPub(path string) (ed25519.PublicKey, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env keystoreFile
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, fmt.Errorf("identity: parse keystore: %w", err)
	}
	if len(env.AccountPub) != ed25519.PublicKeySize {
		return nil, nil
	}
	return ed25519.PublicKey(env.AccountPub), nil
}

// ChangePassphrase re-seals the keystore at path under newPass, after proving
// oldPass opens it. The account and device seeds are carried over unchanged.
// The new file is written beside the old one and renamed over it, so at every
// instant exactly one complete keystore is on disk, sealed under one of the
// two passphrases.
func ChangePassphrase(path, oldPass, newPass string) error {
	if newPass == "" {
		return errors.New("identity: passphrase must not be empty")
	}
	id, err := LoadKeystore(path, oldPass)
	if err != nil {
		return err
	}
	return SaveKeystore(path, newPass, id)
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
	if env.Version < 1 || env.Version > keystoreVersion {
		return nil, fmt.Errorf("identity: unsupported keystore version %d", env.Version)
	}
	if env.KDF != "argon2id" {
		return nil, fmt.Errorf("identity: unsupported kdf %q", env.KDF)
	}
	if len(env.Nonce) != nonceSize {
		return nil, errors.New("identity: bad nonce length in keystore")
	}

	var nonce [nonceSize]byte
	copy(nonce[:], env.Nonce)

	var key [keySize]byte
	var seed []byte
	ok := false
	for _, form := range passphraseForms(passphrase) {
		key = deriveKeyWithParams(form, env.Salt, env.ArgonTime, env.ArgonMemory, env.ArgonThreads)
		if seed, ok = secretbox.Open(nil, env.Ciphertext, &nonce, &key); ok {
			break
		}
	}
	if !ok {
		return nil, ErrWrongPassphrase
	}
	id, err := FromSeed(seed)
	if err != nil {
		return nil, err
	}
	// Unseal the device seed if this file has one (v2). A wrong device nonce/
	// ciphertext shouldn't strand the account, so a failure here just leaves the
	// device seed unset (it'll be regenerated + re-saved by LoadOrCreate).
	if len(env.DeviceCiphertext) > 0 && len(env.DeviceNonce) == nonceSize {
		var dnonce [nonceSize]byte
		copy(dnonce[:], env.DeviceNonce)
		if ds, ok := secretbox.Open(nil, env.DeviceCiphertext, &dnonce, &key); ok && len(ds) == SeedSize {
			id.deviceSeed = ds
		}
	}
	return id, nil
}

// LoadOrCreate loads the identity at path, or generates and persists a new one
// if the file does not exist. This is the normal app-startup entry point.
// The bool result reports whether a new identity was created.
func LoadOrCreate(path, passphrase string) (*Identity, bool, error) {
	id, err := LoadKeystore(path, passphrase)
	if err == nil {
		// Transparently upgrade a legacy (v1) keystore: give it a device seed and
		// persist as v2. Non-fatal if the re-save fails — the device seed lives in
		// memory for this session and the upgrade retries next unlock.
		created, derr := id.ensureDeviceSeed()
		pub, _ := ReadAccountPub(path)
		// Also re-sealed when the file does not yet name its account in the
		// clear (see keystoreFile.AccountPub), so a device that has been
		// unlocked once by this build can say whose it is while locked.
		if (derr == nil && created) || pub == nil {
			_ = SaveKeystore(path, passphrase, id)
		}
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
