package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Erasing the keystore.
//
// An erase normally takes the keystore lock, so no write can be half-way
// through when the file goes. When the lock cannot be had in time (another
// process holding it, possibly stalled in the middle of a write), the erase
// goes ahead anyway and leaves a tombstone, so that the holder cannot bring
// the keystore back when it resumes: writes check for a tombstone before and
// after they rename (saveKeystoreLocked), reads treat the condemned keystore
// as absent, and whoever takes the lock next finishes the erase.
//
// Two rules keep that machinery from ever deleting what nobody asked to
// erase. A tombstone condemns one file, the one on disk when the erase was
// asked for, identified by the hash of its bytes and its file identity; a
// keystore that is not that file is never deleted by it, and the stale
// tombstone is removed and logged. And an erase that fails leaves no
// tombstone behind: the user was told it failed, and the keystore must keep
// working.

// errErasePending: an erase of this device has not finished and could not be
// finished now (another process holds the keystore).
var errErasePending = errors.New("identity: an erase of this device is still finishing; try again in a moment")

// tombstone names the keystore file an erase condemned.
type tombstone struct {
	SHA256 string `json:"sha256"`
	FileID string `json:"file_id,omitempty"` // device and inode where the platform has them
}

// finishers counts the background goroutines finishing erases, so a test can
// wait for them.
var finishers sync.WaitGroup

func tombstonePath(path string) string { return path + ".erased" }

func tombstoneTempPattern(path string) string { return "." + filepath.Base(path) + ".erased-*" }

func erasePending(path string) bool {
	_, err := os.Lstat(tombstonePath(path))
	return err == nil
}

// identify returns the tombstone that would condemn the keystore now at path,
// and whether there is one.
func identify(path string) (tombstone, bool, error) {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return tombstone{}, false, nil
	}
	if err != nil {
		return tombstone{}, false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return tombstone{}, false, errKeystoreSymlink
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return tombstone{}, false, err
	}
	sum := sha256.Sum256(blob)
	return tombstone{SHA256: hex.EncodeToString(sum[:]), FileID: fileID(fi)}, true, nil
}

// condemned reports whether the keystore now at path is the file a pending
// erase condemned.
func condemned(path string) bool {
	if !erasePending(path) {
		return false
	}
	raw, err := os.ReadFile(tombstonePath(path))
	if err != nil {
		return false
	}
	var t tombstone
	if json.Unmarshal(raw, &t) != nil || t.SHA256 == "" {
		return false
	}
	now, ok, err := identify(path)
	if err != nil || !ok || now.SHA256 != t.SHA256 {
		return false
	}
	return t.FileID == "" || now.FileID == t.FileID
}

// writeTombstone records t durably, without following anything planted at
// the tombstone's name: written to a fresh temp file, synced, renamed into
// place, directory synced.
func writeTombstone(path string, t tombstone) error {
	blob, _ := json.Marshal(t)
	// Not tempPattern: lock holders sweep those, and this is written by an
	// erase that does not hold the lock.
	f, err := os.CreateTemp(filepath.Dir(path), tombstoneTempPattern(path))
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := writeSynced(f, blob); err != nil {
		return err
	}
	if err := renameDurable(tmp, tombstonePath(path)); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// removeKeystoreFiles deletes the keystore and the temp files beside it, the
// keystore last, so a failure part-way leaves the keystore in place. For a
// caller holding the lock: no write of this build has a temp file open then.
func removeKeystoreFiles(path string) error {
	removeStaleTemps(path)
	return removeKeystoreOnly(path)
}

// removeKeystoreOnly is removeKeystoreFiles without sweeping temp files, for
// an erase that does not hold the lock: a temp file then may be the one a
// write is about to rename, and that write must get to its rename to find
// the erase and stand down (saveKeystoreLocked).
func removeKeystoreOnly(path string) error {
	// path+".tmp" is the temp name builds before unique temp files used.
	for _, p := range []string{path + ".tmp", path} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_ = syncDir(filepath.Dir(path))
	return nil
}

// RemoveKeystore erases the keystore at path. See the top of this file for
// what happens when the keystore lock cannot be had in time.
func RemoveKeystore(path string) error {
	if unlock, err := lockKeystore(path); err == nil {
		defer unlock()
		return removeKeystoreFiles(path)
	}
	if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
		return nil // nothing to erase
	}
	t, present, err := identify(path)
	if err != nil {
		return err
	}
	if !present {
		removeStaleTemps(path)
		return nil
	}
	if err := writeTombstone(path, t); err != nil {
		return err
	}
	if err := removeKeystoreOnly(path); err != nil {
		// Reported as failed, so it must not happen later either: an erase
		// left pending here would delete the keystore at some later unlock
		// the user made after being told it was still there.
		_ = os.Remove(tombstonePath(path))
		_ = syncDir(filepath.Dir(path))
		return err
	}
	finishers.Add(1)
	go func() {
		defer finishers.Done()
		// Taking the lock finishes the erase (see lockKeystore). Retried, one
		// bounded attempt at a time so this never sits on keystoreMu, until
		// it happens here or another holder has done it.
		for erasePending(path) {
			if unlock, err := lockKeystore(path); err == nil {
				unlock()
				return
			}
			time.Sleep(time.Second)
		}
	}()
	return nil
}

// finishPendingErase completes an erase that could not take the lock, for a
// caller holding it. Only the file the tombstone condemned is deleted; any
// other keystore at the path is left exactly where it is.
func finishPendingErase(path string) {
	if !erasePending(path) {
		return
	}
	if condemned(path) {
		if removeKeystoreFiles(path) != nil {
			return // the tombstone stays: the erase is still asked for
		}
	} else if _, err := os.Lstat(path); err == nil {
		log.Printf("concord/identity: an unfinished erase named a keystore that is no longer there; the keystore now there is kept")
	}
	_ = os.Remove(tombstonePath(path))
	stale, _ := filepath.Glob(filepath.Join(filepath.Dir(path), tombstoneTempPattern(path)))
	for _, p := range stale {
		_ = os.Remove(p)
	}
	_ = syncDir(filepath.Dir(path))
}

// finishEraseOfOwnWrite is for a write, holding the lock, that renamed its
// file in while an erase was being asked for: the erase wins.
func finishEraseOfOwnWrite(path string) {
	if removeKeystoreFiles(path) == nil {
		_ = os.Remove(tombstonePath(path))
		_ = syncDir(filepath.Dir(path))
	}
}

// finishErase takes the keystore lock just long enough to finish a pending
// erase, or reports that one is still pending.
func finishErase(path string) error {
	unlock, err := lockKeystore(path)
	if err != nil {
		return errErasePending
	}
	unlock()
	return nil
}

// HasKeystore reports whether there is a keystore at path to unlock: one
// that exists and is not condemned by an unfinished erase.
func HasKeystore(path string) bool {
	_, err := os.Stat(path)
	return err == nil && !condemned(path)
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
