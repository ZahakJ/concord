package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The erase record.
//
// An erase of this device (an unlink, the Settings wipe, starting over)
// writes a record into the data directory before it removes anything, and
// removes the record only after everything else is gone and the directory
// is synced. Between the two the keystore goes first, so whatever an
// interruption leaves behind cannot be opened. A start, a restore or a link
// that finds the record finishes that erase before it opens or creates
// anything, and a device with the record shows no account. A removal that
// fails leaves the record, and the erase is tried again.
//
// The record names the keystore its erase removes, by the hash of its bytes.
// A keystore that is not that file (one an older build created after the
// erase stopped) is never removed on the record's say-so: the record is
// stale, and is removed instead.

// eraseRecordName is the record's file name in the data directory.
const eraseRecordName = "erase-in-progress"

func eraseRecordPath(dataDir string) string { return filepath.Join(dataDir, eraseRecordName) }

// eraseRecord is the record's content.
type eraseRecord struct {
	// Keystore is the hex SHA-256 of the keystore the erase removes; "" when
	// there was none.
	Keystore string `json:"keystore,omitempty"`
	// Unlinked: the erase is the one an unlink of this device asked for.
	Unlinked bool `json:"unlinked,omitempty"`
}

// ErrEraseUnfinished: an erase of this device has not finished. Nothing on
// the device is opened, and no account is created, until it has; the error
// says what is still in the way.
var ErrEraseUnfinished = errors.New("Erasing this device hasn't finished, so Concord won't open anything on it yet")

// eraseHeldAdvice is said when a file could not be removed.
const eraseHeldAdvice = "Another program may be using it. Close any other Concord windows, or restart the computer; Concord finishes erasing the next time it starts"

// eraseStep runs after each step of an erase, named. An error stops the
// erase there, as if the process had died. A variable for tests.
var eraseStep = func(step string) error { return nil }

// keystoreDigest is the hex SHA-256 of the keystore at path, "" when there
// is none.
func keystoreDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// erasePendingIn reports whether an erase of dataDir has begun and not
// finished.
func erasePendingIn(dataDir string) bool {
	_, err := os.Lstat(eraseRecordPath(dataDir))
	return err == nil
}

// writeEraseRecord records, durably, that an erase of dataDir begins: written
// to a temporary file, synced, renamed into place, directory synced. Nothing
// is removed unless this succeeds.
func writeEraseRecord(dataDir string, unlinked bool) error {
	digest, err := keystoreDigest(keystorePathIn(dataDir))
	if err != nil {
		return fmt.Errorf("app: the keystore cannot be read, so nothing was erased: %w", err)
	}
	blob, _ := json.Marshal(eraseRecord{Keystore: digest, Unlinked: unlinked})
	f, err := os.CreateTemp(dataDir, "."+eraseRecordName+"-*")
	if err != nil {
		return fmt.Errorf("app: nothing was erased: %w", err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	_, err = f.Write(blob)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, eraseRecordPath(dataDir))
	}
	if err == nil {
		err = syncEraseDir(dataDir)
	}
	if err != nil {
		_ = os.Remove(eraseRecordPath(dataDir))
		return fmt.Errorf("app: nothing was erased: %w", err)
	}
	return eraseStep("record")
}

// readEraseRecord reads the record. One that does not parse is a record of
// nothing: it applies only to a device with no keystore.
func readEraseRecord(dataDir string) (eraseRecord, error) {
	var rec eraseRecord
	b, err := os.ReadFile(eraseRecordPath(dataDir))
	if err != nil {
		return rec, err
	}
	if json.Unmarshal(b, &rec) != nil {
		return eraseRecord{}, nil
	}
	return rec, nil
}

// eraseRecordApplies reports whether the keystore now in dataDir is the one
// rec's erase removes, or there is none.
func eraseRecordApplies(dataDir string, rec eraseRecord) (bool, error) {
	digest, err := keystoreDigest(keystorePathIn(dataDir))
	if err != nil {
		return false, err
	}
	return digest == "" || digest == rec.Keystore, nil
}

// removeEraseRecord removes the record and any temporary file a write of it
// left, then syncs the directory.
func removeEraseRecord(dataDir string) error {
	if err := os.Remove(eraseRecordPath(dataDir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	tmps, _ := filepath.Glob(filepath.Join(dataDir, "."+eraseRecordName+"-*"))
	for _, p := range tmps {
		_ = os.Remove(p)
	}
	return syncEraseDir(dataDir)
}

// syncEraseDir makes the entries of dir durable. Windows cannot sync a
// directory.
func syncEraseDir(dir string) error {
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

// DataLeftError is ErrDataNotErased naming the files still here.
type DataLeftError struct{ Files []string }

func (e *DataLeftError) Error() string {
	return ErrDataNotErased.Error() + ": still here: " + strings.Join(e.Files, ", ")
}

func (e *DataLeftError) Unwrap() error { return ErrDataNotErased }

// LeftFiles names the files an erase error says are still here; nil when it
// names none.
func LeftFiles(err error) []string {
	var left *DataLeftError
	if errors.As(err, &left) {
		return left.Files
	}
	return nil
}

// PendingEraseIsUnlink reports whether an unfinished erase is recorded here,
// and that it is the one an unlink asked for.
func PendingEraseIsUnlink(dataDir string) bool {
	if !erasePendingIn(dataDir) {
		return false
	}
	rec, err := readEraseRecord(dataDir)
	if err != nil || !rec.Unlinked {
		return false
	}
	applies, err := eraseRecordApplies(dataDir, rec)
	return err == nil && applies
}
