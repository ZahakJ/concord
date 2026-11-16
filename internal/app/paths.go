// Package app is layer 6 of Concord: it orchestrates the lower layers
// (identity, net, domain, media, store) and is the single surface the UI binds
// to. This file owns the on-disk layout so every entry point — the Wails GUI
// and the headless CLI alike — agrees on where Concord keeps its data.
package app

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

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
	// A device whose erase has not finished (erasePendingIn) has none: nothing
	// of the account it held is shown.
	_, err := os.Stat(keystorePathIn(dataDir))
	return err == nil && !erasePendingIn(dataDir)
}

// ResetIdentity wipes the identity and all data tied to it (keystore, database,
// MLS group state), e.g. for a forgotten passphrase or a corrupted keystore.
//
// The erase record is written before anything is removed (see
// eraserecord.go), the keystore goes first, and the record goes last: an
// erase stopped at any point is finished by the next start, restore or link
// (finishInterruptedErase) before it opens anything. A failure to write the
// record, or to remove the keystore, erases nothing and leaves no record. A
// file that cannot be removed is named in the error (ErrDataNotErased), and
// the record stays.
func ResetIdentity(dataDir string) error { return resetIdentity(dataDir, false) }

// resetIdentity is ResetIdentity, recording whether the erase is an unlink's.
func resetIdentity(dataDir string, unlinked bool) error {
	if err := writeEraseRecord(dataDir, unlinked); err != nil {
		return err
	}
	if err := removeKeystoreFiles(dataDir); err != nil {
		// Nothing was erased, and the erase is not left to be finished later.
		_ = removeEraseRecord(dataDir)
		return err
	}
	return eraseRecordedData(dataDir)
}

// removeKeystoreFiles removes the keystore and its temporary file.
func removeKeystoreFiles(dataDir string) error {
	for _, p := range []string{keystorePathIn(dataDir) + ".tmp", keystorePathIn(dataDir)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_ = syncEraseDir(dataDir)
	return nil
}

// ErrDataNotErased: some of an erased account's data could not be removed;
// the error names what is left. The erase record stays, and the next start
// tries again before it opens anything.
var ErrDataNotErased = errors.New("app: some of this device's data could not be removed")

// accountDataFiles are the files that are an account's data, beside the
// group-state directory (mls/).
func accountDataFiles(dataDir string) []string {
	return []string{
		dbPathIn(dataDir), dbPathIn(dataDir) + "-wal", dbPathIn(dataDir) + "-shm",
		// The remembered-peer cache is plaintext and outlives the keystore, so
		// leaving it would hand the new identity the old one's contact list —
		// and the first launch dials it, from the same address, announcing to
		// every one of those peers that the two identities are the same person.
		// That is the one thing starting over is for.
		peerCachePath(dataDir), peerCachePath(dataDir) + ".tmp",
		// The linked-device marker is the certificate saying "the account key
		// signed FOR this device key". The device key does not survive here —
		// the next unlock finds no device seed and mints a fresh one — so a
		// marker left behind describes a key this install no longer has. It
		// would come back in linked mode, offer that certificate as its MLS
		// credential, and be refused by every peer that binds a credential to
		// the connection it arrived on, with nothing on either side saying why.
		deviceMarkerPath(dataDir), deviceMarkerPath(dataDir) + ".tmp",
	}
}

// eraseRecordedData is the part of a recorded erase after its record: the
// keystore, the account data, then the record. Every item is tried, and what
// could not be removed is named.
func eraseRecordedData(dataDir string) error {
	if err := removeKeystoreFiles(dataDir); err != nil {
		return &DataLeftError{Files: []string{filepath.Base(keystorePathIn(dataDir))}}
	}
	if err := eraseStep("keystore"); err != nil {
		return err
	}
	var left []string
	for _, p := range accountDataFiles(dataDir) {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			left = append(left, filepath.Base(p))
		}
		if err := eraseStep(filepath.Base(p)); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(dataDir, "mls")); err != nil {
		left = append(left, "mls")
	}
	if err := eraseStep("mls"); err != nil {
		return err
	}
	_ = syncEraseDir(dataDir)
	if len(left) > 0 {
		return &DataLeftError{Files: left}
	}
	if err := removeEraseRecord(dataDir); err != nil {
		return &DataLeftError{Files: []string{eraseRecordName}}
	}
	return nil
}

// finishInterruptedErase finishes an erase of dataDir that began and did not
// finish (its record is here): the keystore it named, then the account data,
// then the record. unlinked reports that it was an unlink's erase and it is
// now finished. A record naming a keystore that is not the one here is
// stale: it is removed, and nothing else is.
func finishInterruptedErase(dataDir string) (unlinked bool, err error) {
	if !erasePendingIn(dataDir) {
		return false, nil
	}
	rec, err := readEraseRecord(dataDir)
	if err != nil {
		return false, fmt.Errorf("%w: its record cannot be read: %w", ErrEraseUnfinished, err)
	}
	ok, err := eraseRecordApplies(dataDir, rec)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrEraseUnfinished, err)
	}
	if !ok {
		if err := removeEraseRecord(dataDir); err != nil {
			return false, fmt.Errorf("%w: %w", ErrEraseUnfinished, err)
		}
		log.Printf("concord/app: an unfinished erase named a keystore that is no longer here; the keystore here is kept")
		return false, nil
	}
	if err := eraseRecordedData(dataDir); err != nil {
		err = fmt.Errorf("%w: %w. %s", ErrEraseUnfinished, err, eraseHeldAdvice)
		if rec.Unlinked {
			err = fmt.Errorf("%w: %w", ErrDeviceUnlinkedNotErased, err)
		}
		return false, err
	}
	log.Printf("concord/app: finished an erase of this device that had not finished")
	return rec.Unlinked, nil
}

// RestoreIdentity writes a keystore reconstructed from a recovery phrase,
// sealed under passphrase. Guilds/history/mailbox are recovered afterwards by
// logging in and syncing. The caller must ensure no identity already exists.
func RestoreIdentity(dataDir, phrase, passphrase string) error {
	return identity.RestoreKeystore(keystorePathIn(dataDir), phrase, passphrase)
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
// current opens it. See identity.ChangePassphrase for why a failure part-way
// leaves the old keystore readable.
func ChangePassphrase(dataDir, current, next string) error {
	return identity.ChangePassphrase(keystorePathIn(dataDir), current, next)
}

// ErrDataWithoutKeystore refuses to open or create anything on a device that
// holds an account's data but no keystore to open it. Nothing is read,
// reused or created beside it until the user chooses: erase it, or (on an
// install that was never linked) restore the account from its recovery
// phrase, which keeps the data.
var ErrDataWithoutKeystore = errors.New("app: this device has an account's data but no keystore to open it")

// hasAccountData reports whether any account data is here (accountDataFiles,
// or anything in the group-state directory).
func hasAccountData(dataDir string) bool {
	for _, p := range accountDataFiles(dataDir) {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "mls"))
	return err == nil && len(entries) > 0
}

// DataWithoutKeystore reports, for the first screen, whether this device
// holds account data with no keystore, and whether that data is a linked
// device's (which nobody can open again; a restore erases it).
func DataWithoutKeystore(dataDir string) (stranded, linked bool) {
	if _, err := os.Lstat(keystorePathIn(dataDir)); err == nil || erasePendingIn(dataDir) {
		return false, false
	}
	if !hasAccountData(dataDir) {
		return false, false
	}
	return true, hasDeviceMarker(dataDir)
}
