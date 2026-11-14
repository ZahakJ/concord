package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
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

// errCorruptKeystore is a keystore that cannot be what any build wrote. It is
// ErrWrongPassphrase to every caller (errors.Is), which is what a user can act
// on: the recovery phrase.
var errCorruptKeystore = fmt.Errorf("%w (its key derivation settings are out of range)", ErrWrongPassphrase)

// errDamagedKeystore is a keystore file that does not parse (empty, cut
// short, overwritten) or lacks what every build writes. Like a wrong
// passphrase, the recovery phrase is the way back, so it is reported the same
// way (errors.Is ErrWrongPassphrase), which is what the unlock screen routes
// to recovery. A file from a newer build (an unknown version or key
// derivation) is not damaged and keeps its own error.
var errDamagedKeystore = fmt.Errorf("%w (the keystore file is damaged)", ErrWrongPassphrase)

// Bounds on the Argon2id cost a file may ask for. The cost is read from the
// file before anything is verified, and Argon2 panics on zero passes or
// lanes, takes the process down asking for terabytes, or never returns on
// billions of passes: one flipped bit in a stored value used to turn a
// damaged keystore into a crash at every launch.
//
// Every release has written 3 passes, 64 MiB, 4 lanes (28 ms on the desktop
// these were measured on). The ceilings leave room to raise that later (2.7x
// the passes, 4x the memory) while keeping the worst a file can ask for to
// what a phone can afford: 8 passes over 256 MiB took 0.34 s with 4 lanes
// and 0.9 s with one, where the earlier ceiling (16 passes, 1 GiB) took
// 8.3 s and a gigabyte, up to four times over for a wrong non-ASCII
// passphrase and twice under the keystore lock for a passphrase change.
const (
	maxArgonTime    = 8
	maxArgonMemory  = 256 << 10 // KiB => 256 MiB
	maxArgonThreads = 64
)

// validKDF reports whether a stored Argon2id cost and salt are safe to derive
// with.
func validKDF(time, memory uint32, threads uint8, salt []byte) bool {
	return time >= 1 && time <= maxArgonTime &&
		threads >= 1 && threads <= maxArgonThreads &&
		memory >= 8*uint32(threads) && memory <= maxArgonMemory &&
		len(salt) >= saltSize
}

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
	// unlocks. It is advisory, not integrity: nothing authenticates it, so
	// anyone who can write the file can change it, and the comparison defers
	// to the database where that can answer. Absent in files written before
	// it existed; LoadOrCreate adds it, or corrects it, on the next unlock.
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
//
// beforeRename, if set, runs once the new keystore is written and synced to
// its temp file and before it is renamed into place; if it fails, nothing is
// renamed and the old keystore stays. It is how a replacement that must
// erase other data first does so only once the new keystore is safely on
// disk (see ReplaceKeystoreWith).
func saveKeystoreLocked(path, passphrase string, id *Identity, prev []byte, beforeRename ...func() error) error {
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

	// Write to a temp file then rename, so a crash mid-write can never leave a
	// half-written (unopenable) keystore. The sync before the rename is what
	// makes that true across a power cut: without it the rename can reach the
	// disk before the bytes it points at, and a passphrase change interrupted
	// there would leave neither the old file nor a readable new one.
	//
	// The temp file is created fresh with a name nobody can predict (O_EXCL,
	// 0600 from the start). A fixed name was opened with truncation and
	// followed wherever it pointed: a symlink planted there received the
	// keystore and became it, a hard link to the keystore had it rewritten in
	// place, and a second writer without the lock truncated the first one's
	// file under its rename.
	dir := filepath.Dir(path)
	removeStaleTemps(path)
	f, err := os.CreateTemp(dir, tempPattern(path))
	if err != nil {
		return fmt.Errorf("identity: write keystore: %w", err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // a no-op once renamed
	written, err := writeSynced(f, blob)
	if err != nil {
		return fmt.Errorf("identity: write keystore: %w", err)
	}
	// An erase that could not take the lock may have been asked for while this
	// write held it (see RemoveKeystore). It wins: the write stands down
	// before renaming, and a write that renamed as the erase was being asked
	// for takes its own file away again.
	if erasePending(path) {
		return ErrKeystoreChanged
	}
	for _, f := range beforeRename {
		if f != nil {
			if err := f(); err != nil {
				return err
			}
		}
	}
	testHooks.rename()
	if err := renameDurable(tmp, path); err != nil {
		if erasePending(path) {
			return ErrKeystoreChanged
		}
		return fmt.Errorf("identity: finalize keystore: %w", err)
	}
	if erasePending(path) {
		if err := finishEraseOfOwnWrite(path); err != nil {
			return errors.Join(ErrKeystoreChanged, err)
		}
		return ErrKeystoreChanged
	}
	// Only a rename that put this file at the path counts: one that did not
	// (two names for one file make rename a silent no-op) must not be
	// reported as a passphrase changed.
	if now, err := os.Lstat(path); err != nil || !os.SameFile(now, written) {
		return errors.New("identity: the keystore was not replaced")
	}
	// The rename is an entry in the directory, and until the directory is
	// synced a power cut can still undo it: a passphrase change reported done
	// would come back as the old file. Best effort, because the new keystore
	// is already in place and failing here would tell the user a change that
	// took effect did not.
	_ = syncDir(dir)
	return nil
}

// tempPattern names a keystore write's temp file (os.CreateTemp pattern).
func tempPattern(path string) string { return "." + filepath.Base(path) + ".*.tmp" }

// removeStaleTemps deletes temp files a crashed write left, for a caller
// holding the keystore lock (no write of this build has one open then).
// Remove takes a name away and never follows it.
func removeStaleTemps(path string) {
	stale, _ := filepath.Glob(filepath.Join(filepath.Dir(path), tempPattern(path)))
	for _, p := range stale {
		_ = os.Remove(p)
	}
}

// syncDir flushes a directory's entries to disk. A variable so a test can see
// that it runs. Windows cannot sync a directory handle; there the rename is
// made durable by renameDurable itself (write-through) instead.
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
	if !validKDF(env.ArgonTime, env.ArgonMemory, env.ArgonThreads, env.Salt) {
		return kp // never carried forward: it could not be opened again
	}
	kp.time = max(kp.time, env.ArgonTime)
	kp.memory = max(kp.memory, env.ArgonMemory)
	kp.threads = max(kp.threads, env.ArgonThreads)
	return kp
}

func writeSynced(f *os.File, blob []byte) (os.FileInfo, error) {
	if _, err := f.Write(blob); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return fi, f.Close()
}

// ReadAccountPub returns the account public key recorded in the clear in the
// keystore at path, or nil when the file predates that field. It needs no
// passphrase, which is the point: it is how a LOCKED device names the account
// it holds.
func ReadAccountPub(path string) (ed25519.PublicKey, error) {
	blob, err := ReadKeystore(path)
	if err != nil {
		return nil, err
	}
	return accountPubOf(blob)
}

func accountPubOf(blob []byte) (ed25519.PublicKey, error) {
	var env keystoreFile
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, errDamagedKeystore
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
	return ChangePassphraseOf(path, nil, oldPass, newPass)
}

// ErrNotThisAccount: the keystore on disk holds an account other than the
// one the caller is signed in as.
var ErrNotThisAccount = errors.New("identity: the keystore on this device is not the signed-in account")

// ChangePassphraseOf is ChangePassphrase for a signed-in session: it re-seals
// the keystore only if it holds account. Another window or process can have
// restored or linked a different account here under the same passphrase,
// and re-keying that one would tell the user their passphrase changed when
// the account they are using was not touched.
func ChangePassphraseOf(path string, account ed25519.PublicKey, oldPass, newPass string) error {
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
	if account != nil && !bytes.Equal(id.PublicKey(), account) {
		return ErrNotThisAccount
	}
	if err := checkExpected(path, opened); err != nil {
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
	beforeRename  func() // a write has checked for a pending erase and is about to rename
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

func (h resealHooks) rename() {
	if h.beforeRename != nil {
		h.beforeRename()
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

var errKeystoreSymlink = errors.New("identity: the keystore is a symbolic link; Concord will not write through it — replace it with the file it points to")

// ReadKeystore returns the keystore file's bytes: the snapshot a caller
// decides on (decrypts with OpenKeystoreBytes, compares accounts) and then
// hands back to ReplaceKeystore or RemoveKeystoreIfUnchanged as expected.
// A keystore an unfinished erase condemned (see RemoveKeystore) reads as
// absent.
func ReadKeystore(path string) ([]byte, error) {
	if condemned(path) {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	return os.ReadFile(path)
}

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
	return writeKeystore(path, passphrase, id, expected, resealHooks{}, nil)
}

// ReplaceKeystoreWith is ReplaceKeystore for a replacement that must also
// erase what belongs to the account being replaced: eraseOld runs under the
// keystore lock after the check and after the new keystore is written and
// synced beside the old one, and only then is the new one renamed into
// place. If anything fails before the rename, the old keystore is still
// there; the device is never left with neither.
func ReplaceKeystoreWith(path, passphrase string, id *Identity, expected []byte, eraseOld func() error) error {
	return writeKeystore(path, passphrase, id, expected, resealHooks{}, eraseOld)
}

func writeKeystore(path, passphrase string, id *Identity, expected []byte, hooks resealHooks, beforeRename func() error) error {
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
	return saveKeystoreLocked(path, passphrase, id, expected, beforeRename)
}

// checkExpected reports, for a caller holding the keystore lock, whether the
// file at path is still expected (nil: absent).
func checkExpected(path string, expected []byte) error {
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		// Replacing it would swap the link for a file and leave the keystore
		// it names, under its old passphrase, wherever that is.
		return errKeystoreSymlink
	}
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
	return writeKeystore(path, passphrase, id, opened, hooks, nil)
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
		return nil, errDamagedKeystore
	}
	if env.Version < 1 || env.Version > keystoreVersion {
		return nil, fmt.Errorf("identity: unsupported keystore version %d", env.Version)
	}
	if env.KDF != "argon2id" {
		return nil, fmt.Errorf("identity: unsupported kdf %q", env.KDF)
	}
	if len(env.Nonce) != nonceSize {
		return nil, errDamagedKeystore
	}
	if !validKDF(env.ArgonTime, env.ArgonMemory, env.ArgonThreads, env.Salt) {
		return nil, errCorruptKeystore
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
	return loadOrCreate(path, passphrase, false)
}

func loadOrCreate(path, passphrase string, again bool) (*Identity, bool, error) {
	id, opened, err := openKeystore(path, passphrase)
	if err == nil {
		// Transparently upgrade a legacy (v1) keystore: give it a device seed and
		// persist as v2. Non-fatal if the re-save fails — the session runs
		// without a device seed and the upgrade retries next unlock. Like the
		// re-seal below it only replaces the file it decrypted, so a passphrase
		// change that lands between the two is never undone.
		created, derr := id.ensureDeviceSeed()
		hooks := testHooks
		switch {
		case derr == nil && created:
			hooks.before()
			if resealIfUnchanged(path, passphrase, id, opened, hooks) != nil {
				// Not on disk, so not in use either: a device key that
				// exists only in memory would be a different one next
				// session. The next unlock tries the upgrade again.
				id.deviceSeed = nil
			}
			hooks.done()
		case !namesAccount(opened, id.PublicKey()):
			if pub, _ := accountPubOf(opened); pub != nil {
				// Present but not this seed's: written by a bug, or by hand.
				log.Printf("concord/identity: the keystore's clear account key did not match its seed; correcting it")
			}
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
	if _, lerr := os.Lstat(path); lerr == nil {
		// Absent only because an unfinished erase condemned it. Finish that
		// erase before anything is created here, and look again: nothing is
		// ever created over a keystore that is on the disk.
		if again {
			return nil, false, errErasePending
		}
		if err := finishErase(path); err != nil {
			return nil, false, err
		}
		return loadOrCreate(path, passphrase, true)
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
