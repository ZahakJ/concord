// Package app is layer 6 of Concord: it orchestrates the lower layers
// (identity, net, domain, media, store) and is the single surface the UI binds
// to. This file owns the on-disk layout so every entry point — the Wails GUI
// and the headless CLI alike — agrees on where Concord keeps its data.
package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZahakJ/concord/internal/identity"
)

// appDir is the subdirectory under the OS config root where Concord stores all
// per-user state.
const appDir = "concord"

// DataDir returns Concord's per-user data directory, creating it if needed.
// It follows OS conventions via os.UserConfigDir (e.g. ~/.config/concord on
// Linux). CONCORD_HOME overrides it, which is what lets us run several
// isolated instances on one machine for local multi-peer testing.
func DataDir() (string, error) {
	if override := os.Getenv("CONCORD_HOME"); override != "" {
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", err
		}
		return override, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, appDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// KeystorePath returns the path to the encrypted identity keystore in the
// default data directory.
func KeystorePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return keystorePathIn(dir), nil
}

// keystorePathIn returns the keystore path within an explicit data directory.
func keystorePathIn(dataDir string) string {
	return filepath.Join(dataDir, "keystore.json")
}

// dbPathIn returns the encrypted database path within an explicit data directory.
func dbPathIn(dataDir string) string {
	return filepath.Join(dataDir, "concord.db")
}

// HasIdentity reports whether an identity keystore already exists in dataDir,
// so the UI can show "unlock" vs "create a passphrase".
func HasIdentity(dataDir string) bool {
	return identity.HasKeystore(keystorePathIn(dataDir))
}

// ResetIdentity wipes the identity and all data tied to it (keystore, database,
// MLS group state) so a fresh identity can be created — for a forgotten
// passphrase or a corrupted keystore. Irreversible.
func ResetIdentity(dataDir string) error {
	// Through identity, so a keystore write in flight (the re-seal behind an
	// unlock) finishes before the file goes rather than recreating it after.
	// Everything happens now, while the user who confirmed it is here: if
	// any of it fails, what is left stays until they choose again. Nothing
	// later deletes it on this erase's behalf.
	err := removeKeystore(keystorePathIn(dataDir))
	if err != nil && !errors.Is(err, identity.ErrEraseDeferred) {
		return err
	}
	// A deferred keystore erase will complete (the keystore already reads as
	// gone), so the rest is erased now as asked, and the caller is told the
	// keystore's own removal is still to come. If the rest fails too, both
	// are said: reporting only the data failure would read as "nothing
	// happened" while the keystore erase still completes later.
	if derr := resetDataIn(dataDir); derr != nil {
		return errors.Join(err, derr) // derr names what is left (ErrDataNotErased)
	}
	return err
}

// removeKeystore is identity.RemoveKeystore, a variable so a test can make
// the keystore erase defer.
var removeKeystore = identity.RemoveKeystore

// ErrDataNotErased: some of an erased account's data (database, peer cache,
// device marker, group state) could not be removed; the error names what is
// left. Nothing new is created beside it (guardCreate), and nothing removes
// it later on its own: the user chooses again.
var ErrDataNotErased = errors.New("app: some of this device's data could not be removed")

// ErrDataWithoutKeystore refuses to set up a new account on a device that has
// another account's data but no keystore, and no record that its erase was
// asked for. Nothing is deleted on an inference: the keystore may be lost,
// on a drive not mounted, or not copied back yet. The way forward is the
// recovery phrase (which keeps the data if it is that account's) or an
// explicit start over.
var ErrDataWithoutKeystore = errors.New("app: this device has an account's data but no keystore to open it")

func init() { identity.CreateGuard = guardCreate }

// guardCreate is identity.CreateGuard: run under the keystore lock inside a
// first-run create. No account data here: go ahead. Any account data with no
// keystore: refuse, deleting nothing and creating nothing. Data is deleted
// only by an erase the user confirms in that moment (start over, the wipe,
// or a confirmed restore or link), never on anyone's inference.
func guardCreate(keystorePath string) error {
	if hasAccountData(filepath.Dir(keystorePath)) {
		return ErrDataWithoutKeystore
	}
	return nil
}

// dataIdentity names the account data on the device (the files resetDataIn
// removes, the group-state directory's contents included) by what they are
// on disk, without reading them: a confirmation shown for this data, or an
// erase decided on it, is not applied to data that has since been replaced,
// copied in or written. "" when there is none.
func dataIdentity(dataDir string) string {
	// The database's -wal and -shm side files are left out: merely reading
	// the database (the account check, the name the dialog shows) creates
	// or touches them, so they would change the identity by being looked at.
	var parts []string
	for _, p := range []string{dbPathIn(dataDir), peerCachePath(dataDir), deviceMarkerPath(dataDir)} {
		if fi, err := os.Lstat(p); err == nil {
			parts = append(parts, filepath.Base(p)+"="+identity.FileIdentity(fi))
		}
	}
	_ = filepath.WalkDir(filepath.Join(dataDir, "mls"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, ierr := d.Info(); ierr == nil {
			rel, _ := filepath.Rel(dataDir, p)
			parts = append(parts, rel+"="+identity.FileIdentity(fi))
		}
		return nil
	})
	return strings.Join(parts, ";")
}

// errDataChanged: the account data on the device is not what a restore or
// link decided on (it changed, or appeared, in between). Nothing was erased.
var errDataChanged = fmt.Errorf("%w (the data on this device changed)", identity.ErrKeystoreChanged)

// eraseIfUnchanged is the eraseOld of a restore or link that decided, on
// seen, to erase the device's data: under the keystore lock, at the write,
// it erases only if the data is still exactly seen. Data that appeared or
// changed meanwhile was never confirmed for erasing, and is left.
func eraseIfUnchanged(dataDir, seen string) func() error {
	return func() error {
		if dataIdentity(dataDir) != seen {
			return errDataChanged
		}
		return resetDataIn(dataDir)
	}
}

// hasAccountData reports whether any of the data resetDataIn removes is
// present.
func hasAccountData(dataDir string) bool {
	for _, p := range []string{
		dbPathIn(dataDir), dbPathIn(dataDir) + "-wal", dbPathIn(dataDir) + "-shm",
		peerCachePath(dataDir), deviceMarkerPath(dataDir),
	} {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "mls"))
	return err == nil && len(entries) > 0
}

// resetDataIn removes everything ResetIdentity erases except the keystore.
func resetDataIn(dataDir string) error {
	// Every item is tried, and what could not be removed is named: the user
	// is told exactly what is and is not gone.
	var left []string
	for _, p := range []string{
		dbPathIn(dataDir),
		dbPathIn(dataDir) + "-wal",
		dbPathIn(dataDir) + "-shm",
		// The remembered-peer cache is plaintext and outlives the keystore, so
		// leaving it would hand the new identity the old one's contact list —
		// and the first launch dials it, from the same address, announcing to
		// every one of those peers that the two identities are the same person.
		// That is the one thing starting over is for.
		peerCachePath(dataDir),
		// The linked-device marker is the certificate saying "the account key
		// signed FOR this device key". The device key does not survive here —
		// the next unlock finds no device seed and mints a fresh one — so a
		// marker left behind describes a key this install no longer has. It
		// would come back in linked mode, offer that certificate as its MLS
		// credential, and be refused by every peer that binds a credential to
		// the connection it arrived on, with nothing on either side saying why.
		deviceMarkerPath(dataDir),
		deviceMarkerPath(dataDir) + ".tmp",
	} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			left = append(left, filepath.Base(p))
		}
	}
	// MLS state directory. A failure here must stop a restore or link before
	// its new keystore lands beside part of the old account's group state.
	if err := os.RemoveAll(filepath.Join(dataDir, "mls")); err != nil {
		left = append(left, "mls")
	}
	// Removals are entries in the data directory, and a power cut can undo
	// them until it is synced. This runs before a restored or linked
	// keystore is renamed into place, and without the sync the new account
	// could come up beside the old one's database or group state. The mls
	// directory is gone as a whole, so syncing the data directory that held
	// it is what makes that stick. Best effort: Windows cannot sync a
	// directory.
	_ = syncDataDir(dataDir)
	if len(left) > 0 {
		return fmt.Errorf("%w: still here: %s", ErrDataNotErased, strings.Join(left, ", "))
	}
	return nil
}

// syncDataDir is identity.SyncDir, a variable so a test can see it run.
var syncDataDir = identity.SyncDir

// RestoreIdentity writes a keystore reconstructed from a recovery phrase,
// sealed under passphrase. Guilds/history/mailbox are recovered afterwards by
// logging in and syncing. The caller must ensure no identity already exists.
func RestoreIdentity(dataDir, phrase, passphrase string) error {
	// Through the same gate as a restore over an account: data already here
	// is kept only if it is the phrase's account's, and otherwise asked about
	// and erased, never kept beside the restored account.
	return RestoreOverExisting(dataDir, phrase, passphrase, "")
}

// mlsDirIn returns the directory holding persistent MLS group state, creating
// it if needed.
func mlsDirIn(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "mls")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// ChangePassphrase re-seals the keystore in dataDir under next after proving
// current opens it and that it holds account, the signed-in one. See identity.ChangePassphrase for why a failure part-way
// leaves the old keystore readable.
func ChangePassphrase(dataDir string, account []byte, current, next string) error {
	return identity.ChangePassphraseOf(keystorePathIn(dataDir), account, current, next)
}
