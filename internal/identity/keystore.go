package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

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

// CreateKeystore seals id under passphrase and writes it to path, creating
// parent directories as needed, but only if no keystore is there:
// otherwise ErrKeystoreExists and nothing is written. The file is 0600.
// There is deliberately no unconditional write: replacing a keystore goes
// through ReplaceKeystore with the bytes the caller decided on.
func CreateKeystore(path, passphrase string, id *Identity) error {
	return ReplaceKeystore(path, passphrase, id, nil)
}

// saveKeystoreLocked writes the keystore, for a caller holding the keystore
// lock that has checked the file is what it expected.
// prev is the keystore being replaced, as decrypted, or nil: its Argon2 cost
// is a floor for the new one (see sealParams).
func saveKeystoreLocked(path, passphrase string, id *Identity, prev []byte) error {
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

	kp := sealParams(prev)
	key := deriveKeyWithParams(NormalizePassphrase(passphrase), salt, kp.time, kp.memory, kp.threads)
	sealed := secretbox.Seal(nil, id.Seed(), &nonce, &key)

	env := keystoreFile{
		Version:      keystoreVersion,
		KDF:          "argon2id",
		Salt:         salt,
		ArgonTime:    kp.time,
		ArgonMemory:  kp.memory,
		ArgonThreads: kp.threads,
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
	// The rename is an entry in the directory, and until the directory is
	// synced a power cut can still undo it: a passphrase change reported done
	// would come back as the old file. Best effort, because the new keystore
	// is already in place and failing here would tell the user a change that
	// took effect did not.
	_ = syncDir(filepath.Dir(path))
	return nil
}

// syncDir flushes a directory's entries to disk. A variable so a test can see
// that it runs; Windows cannot sync a directory handle and needs no sync for
// the rename to be durable there.
var syncDir = func(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// kdfParams is a keystore's Argon2id cost.
type kdfParams struct {
	time, memory uint32
	threads      uint8
}

// sealParams is the cost to seal a replacement for prev under: this build's
// defaults, raised to whatever prev was sealed with. A re-seal or passphrase
// change must never weaken a file that a later build (or a deliberate
// setting) made more expensive to guess at.
func sealParams(prev []byte) kdfParams {
	kp := kdfParams{argonTime, argonMemory, argonThreads}
	var env keystoreFile
	if len(prev) == 0 || json.Unmarshal(prev, &env) != nil {
		return kp
	}
	kp.time = max(kp.time, env.ArgonTime)
	kp.memory = max(kp.memory, env.ArgonMemory)
	kp.threads = max(kp.threads, env.ArgonThreads)
	return kp
}

func writeSynced(path string, blob []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// The mode above applies only when the file is created. A temp file left
	// behind by a crash or another tool keeps whatever mode it had, and the
	// rename hands that mode to the keystore.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
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

// namesAccount reports whether the keystore bytes blob carry pub, the
// public key of the seed they seal, as their account in the clear. A key
// that is present but different is as good as absent: the locked-device
// restore and re-link trust it to tell the same account from another.
func namesAccount(blob []byte, pub ed25519.PublicKey) bool {
	var env keystoreFile
	return json.Unmarshal(blob, &env) == nil && bytes.Equal(env.AccountPub, pub)
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
	unlock, err := lockKeystore(path)
	if err != nil {
		return err
	}
	defer unlock()
	id, opened, err := openKeystore(path, oldPass)
	if err != nil {
		return err
	}
	return saveKeystoreLocked(path, newPass, id, opened)
}

// Test seams, nil outside tests. LoadOrCreate copies them when it decides to
// re-seal, so a test that sets them does not race a re-seal it did not start.
type resealHooks struct {
	beforeReseal  func() // the unlock has decrypted the file and decided to re-seal
	resealChecked func() // under the lock, the re-seal found the file unchanged
	resealDone    func() // the re-seal has written, or declined to
	beforeCreate  func() // first run found no keystore and is about to create one
}

var testHooks resealHooks

func (h resealHooks) before() {
	if h.beforeReseal != nil {
		h.beforeReseal()
	}
}

func (h resealHooks) checked() {
	if h.resealChecked != nil {
		h.resealChecked()
	}
}

func (h resealHooks) create() {
	if h.beforeCreate != nil {
		h.beforeCreate()
	}
}

func (h resealHooks) done() {
	if h.resealDone != nil {
		h.resealDone()
	}
}

// ErrKeystoreChanged is returned by a conditional keystore write or erase
// when the file on disk is no longer the one the caller read: another window
// or process changed the passphrase, restored, linked or erased in between.
// Nothing was written.
var ErrKeystoreChanged = errors.New("identity: the keystore on this device changed while this was in progress")

// ErrKeystoreExists is returned by a create-only keystore write when a
// keystore appeared at the path after the caller found none. Nothing was
// written.
var ErrKeystoreExists = errors.New("identity: a keystore already exists on this device")

// ReadKeystore returns the keystore file's bytes: the snapshot a caller
// decides on (decrypts with OpenKeystoreBytes, compares accounts) and then
// hands back to ReplaceKeystore or RemoveKeystoreIfUnchanged as expected.
func ReadKeystore(path string) ([]byte, error) { return os.ReadFile(path) }

// ReplaceKeystore seals id under passphrase and writes it to path, if and
// only if the file there is still expected: byte for byte the snapshot the
// caller read and decided on, or, when expected is nil, no file at all
// (create only). Otherwise it returns ErrKeystoreChanged or
// ErrKeystoreExists and writes nothing. Every write of the keystore goes
// through this check under the keystore lock, so a decision taken on what
// the file held, however long ago, can never be applied to a file that has
// since become something else: a passphrase changed since, an account
// restored or linked since, a keystore created by another window.
func ReplaceKeystore(path, passphrase string, id *Identity, expected []byte) error {
	return writeKeystore(path, passphrase, id, expected, resealHooks{})
}

func writeKeystore(path, passphrase string, id *Identity, expected []byte, hooks resealHooks) error {
	if passphrase == "" {
		return errors.New("identity: passphrase must not be empty")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("identity: create keystore dir: %w", err)
		}
	}
	unlock, err := lockKeystore(path)
	if err != nil {
		return err
	}
	defer unlock()
	if err := checkExpected(path, expected); err != nil {
		return err
	}
	hooks.checked()
	return saveKeystoreLocked(path, passphrase, id, expected)
}

// checkExpected reports, for a caller holding the keystore lock, whether the
// file at path is still expected (nil: absent).
func checkExpected(path string, expected []byte) error {
	now, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err) && expected == nil:
		return nil
	case os.IsNotExist(err):
		return ErrKeystoreChanged
	case err != nil:
		return err
	case expected == nil:
		return ErrKeystoreExists
	case !bytes.Equal(now, expected):
		return ErrKeystoreChanged
	}
	return nil
}

// resealIfUnchanged rewrites the keystore under passphrase, but only if the
// file is still byte for byte the one that passphrase was proven against:
// opened must be the exact bytes that were decrypted, never a second read of
// the path. A second read can already be another passphrase's file, and
// re-sealing that under this passphrase would revert the change. Anything
// that does not match — a changed passphrase, a restored or re-linked
// account, a device erased through RemoveKeystore — is left as it is, and
// so is a failed write: the rename never happened, and the next unlock
// tries again.
func resealIfUnchanged(path, passphrase string, id *Identity, opened []byte, hooks resealHooks) error {
	if len(opened) == 0 {
		return ErrKeystoreChanged
	}
	return writeKeystore(path, passphrase, id, opened, hooks)
}

// RemoveKeystoreIfUnchanged erases the keystore at path only if it is still
// expected, the snapshot on which the caller (and the user) decided that it
// may go. A keystore that became another account's, or the same account's
// under a newer passphrase, is not the one that was agreed to and is left
// alone with ErrKeystoreChanged.
func RemoveKeystoreIfUnchanged(path string, expected []byte) error {
	unlock, err := lockKeystore(path)
	if err != nil {
		return err
	}
	defer unlock()
	if expected == nil {
		return ErrKeystoreChanged
	}
	if err := checkExpected(path, expected); err != nil {
		return err
	}
	return removeKeystoreFiles(path)
}

func removeKeystoreFiles(path string) error {
	for _, p := range []string{path, path + ".tmp"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// RemoveKeystore deletes the keystore at path, and any temp file a write left
// beside it, under the keystore lock (see lockKeystore). An erase has to wait for a re-seal that
// is already past its check: deleting the file in the ~200 ms that re-seal
// spends deriving its key let its rename bring the erased account back.
func RemoveKeystore(path string) error {
	if unlock, err := lockKeystore(path); err == nil {
		defer unlock()
	} else {
		// Unable to lock (another process stuck holding it, or a data
		// directory that is already gone): erase anyway. Leaving the
		// keystore behind is the worse failure for an erase.
		keystoreMu.Lock()
		defer keystoreMu.Unlock()
	}
	return removeKeystoreFiles(path)
}

// LoadKeystore reads and decrypts the identity at path using passphrase.
func LoadKeystore(path, passphrase string) (*Identity, error) {
	id, _, err := openKeystore(path, passphrase)
	return id, err
}

// openKeystore is LoadKeystore that also returns the bytes it decrypted, so a
// later write can be made conditional on the file still being exactly those.
func openKeystore(path, passphrase string) (*Identity, []byte, error) {
	blob, err := ReadKeystore(path)
	if err != nil {
		return nil, nil, err // callers use os.IsNotExist to detect first-run
	}
	id, err := OpenKeystoreBytes(blob, passphrase)
	if err != nil {
		return nil, nil, err
	}
	return id, blob, nil
}

// OpenKeystoreBytes decrypts a keystore snapshot taken with ReadKeystore.
func OpenKeystoreBytes(blob []byte, passphrase string) (*Identity, error) {
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
	id, opened, err := openKeystore(path, passphrase)
	if err == nil {
		// Transparently upgrade a legacy (v1) keystore: give it a device seed and
		// persist as v2. Non-fatal if the re-save fails — the device seed lives in
		// memory for this session and the upgrade retries next unlock. Like the
		// re-seal below it only replaces the file it decrypted, so a passphrase
		// change that lands between the two is never undone.
		created, derr := id.ensureDeviceSeed()
		hooks := testHooks
		switch {
		case derr == nil && created:
			hooks.before()
			_ = resealIfUnchanged(path, passphrase, id, opened, hooks)
			hooks.done()
		case !namesAccount(opened, id.PublicKey()):
			// Also re-sealed when the file does not yet name its account in the
			// clear (see keystoreFile.AccountPub), or names another, so a device
			// that has been unlocked once by this build can say whose it is
			// while locked. That is a second key derivation (about 200 ms) the
			// unlock does not need to wait for, so it happens behind it; it only
			// replaces the bytes it decrypted, so a passphrase change in between
			// always wins, and a re-seal lost to an exit is simply retried at
			// the next unlock.
			hooks.before()
			go func() {
				_ = resealIfUnchanged(path, passphrase, id, opened, hooks)
				hooks.done()
			}()
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
	testHooks.create()
	err = CreateKeystore(path, passphrase, id)
	if errors.Is(err, ErrKeystoreExists) {
		// Another window or process created (or restored) an account here
		// between our finding none and our writing. Theirs is on disk; run as
		// that one if this passphrase opens it, and never as the account we
		// just generated, which exists nowhere.
		id, _, err = openKeystore(path, passphrase)
		if err != nil {
			return nil, false, err
		}
		return id, false, nil
	}
	if err != nil {
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
