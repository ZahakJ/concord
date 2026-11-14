package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// ErrEraseDeferred: the keystore could not be removed now, but the erase is
// recorded and will complete: the keystore already reads as gone, and it is
// deleted the next time Concord takes the keystore lock (at the latest, the
// next unlock or create on this device).
var ErrEraseDeferred = errors.New("identity: the erase could not finish now; it will complete the next time Concord opens this device's keystore")

// staleMarker holds, for the current holder of the keystore lock, an erase
// marker that names no file here and could not be removed (see
// finishPendingErase); writes ignore exactly that marker. Guarded by
// keystoreMu, cleared when the lock is let go.
var (
	staleMarker       = map[string]string{} // path -> markerIdentity
	staleMarkerLogged sync.Once
)

// newErasePending reports, for a writer holding the lock, whether an erase
// was asked for during this hold: a marker other than a stale one the lock
// holder already found and could not remove.
func newErasePending(path string) bool {
	id, ok := markerIdentity(path)
	if !ok {
		return false // no marker
	}
	stale, known := staleMarker[path]
	return !known || stale != id
}

// markerIdentity names the erase marker beside path so a stale one can be
// recognised: always by size, time and file identity, never by content, so
// that the same file has one identity whether or not it can be read at the
// moment (a scanner letting go of it, its permissions changed). A new erase
// writes a fresh marker, a new file, so it never matches a stale one.
func markerIdentity(path string) (string, bool) {
	fi, err := os.Lstat(tombstonePath(path))
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%s", fi.Size(), fi.ModTime().UnixNano(), fileID(fi)), true
}

// errErasePending: an erase of this device has not finished and could not be
// finished now (another process holds the keystore).
var errErasePending = errors.New("identity: an erase of this device is still finishing; try again in a moment")

// tombstone names the keystore file an erase condemned.
type tombstone struct {
	SHA256 string `json:"sha256"`
	FileID string `json:"file_id,omitempty"` // device and inode where the platform has them
}

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
		if err := removeFile(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_ = syncDir(filepath.Dir(path))
	return nil
}

// RemoveKeystore erases the keystore at path. See the top of this file for
// what happens when the keystore lock cannot be had in time.
func RemoveKeystore(path string) error {
	if err := refuseSymlinkErase(path); err != nil {
		return err
	}
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
		return nil // temp files are left to a lock holder: one may be in use
	}
	if err := writeTombstone(path, t); err != nil {
		return err
	}
	if err := removeKeystoreOnly(path); err != nil {
		// Reported as failed, so it must not happen later either: an erase
		// left pending here would delete the keystore at some later unlock
		// the user made after being told it was still there. If the marker
		// cannot be taken back either, the erase WILL happen (the marker
		// names the file that is still here), and that is what is reported.
		if rerr := removeFile(tombstonePath(path)); rerr != nil && !os.IsNotExist(rerr) {
			_ = syncDir(filepath.Dir(path))
			startFinisher(path)
			return fmt.Errorf("%w (%v)", ErrEraseDeferred, err)
		}
		_ = syncDir(filepath.Dir(path))
		return err
	}
	startFinisher(path)
	return nil
}

// Finishing an erase in the background: at most one goroutine per keystore,
// and it gives up after finisherAttempts tries (about five minutes). Giving
// up is safe: the marker only ever condemns the file it names, and the next
// holder of the lock still finishes it. Variables for tests.
var (
	finisherAttempts = 30
	finisherPause    = time.Second
	finishing        sync.Map // path -> struct{}
	// finishers counts the running goroutines, so a test can wait for them.
	finishers sync.WaitGroup
)

func startFinisher(path string) {
	if _, running := finishing.LoadOrStore(path, struct{}{}); running {
		return
	}
	attempts, pause := finisherAttempts, finisherPause
	finishers.Add(1)
	go func() {
		defer finishers.Done()
		defer finishing.Delete(path)
		// Taking the lock finishes the erase (see lockKeystore), one bounded
		// attempt at a time so this never sits on keystoreMu.
		for i := 0; i < attempts && erasePending(path); i++ {
			if unlock, err := lockKeystore(path); err == nil {
				unlock()
				return
			}
			time.Sleep(pause)
		}
	}()
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
	if id, ok := markerIdentity(path); ok && removeFile(tombstonePath(path)) != nil {
		// A marker that condemns nothing here and cannot be removed must not
		// make every write stand down as if an erase had just been asked for:
		// writes during this hold of the lock ignore exactly this marker, and
		// only a different one (a new erase) stops them. One that cannot even
		// be read condemns nothing identifiable, and is treated the same way.
		staleMarker[path] = id
		staleMarkerLogged.Do(func() {
			log.Printf("concord/identity: an old erase marker beside the keystore cannot be removed; it is ignored")
		})
	}
	stale, _ := filepath.Glob(filepath.Join(filepath.Dir(path), tombstoneTempPattern(path)))
	for _, p := range stale {
		_ = os.Remove(p)
	}
	_ = syncDir(filepath.Dir(path))
}

// finishEraseOfOwnWrite is for a write, holding the lock, that renamed its
// file in while an erase was being asked for: the erase wins. If its file
// cannot be removed now (on Windows a scanner can hold a file just written),
// the marker is rebound to that file, so the next holder of the lock deletes
// it instead of keeping it as a keystore nobody asked to erase, and the
// failure is reported.
func finishEraseOfOwnWrite(path string) error {
	err := removeKeystoreFiles(path)
	if err == nil {
		_ = os.Remove(tombstonePath(path))
		_ = syncDir(filepath.Dir(path))
		return nil
	}
	if t, present, ierr := identify(path); ierr == nil && present {
		if werr := writeTombstone(path, t); werr != nil {
			return fmt.Errorf("identity: an erase is pending and the keystore just written could neither be removed (%v) nor marked for removal (%v)", err, werr)
		}
	}
	return fmt.Errorf("identity: an erase is pending; the keystore just written could not be removed yet and will be at the next unlock: %w", err)
}

// removeFile is os.Remove, a variable so a test can make a removal fail.
var removeFile = os.Remove

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

// refuseSymlinkErase: removing a symlinked keystore removes the link and
// leaves the sealed seeds wherever it points, which is not an erase. The
// erase refuses and names the file, rather than reporting success.
func refuseSymlinkErase(path string) error {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	target, _ := os.Readlink(path)
	return fmt.Errorf("identity: the keystore is a symbolic link to %s; erasing the link would leave the keystore there, so nothing was erased — delete that file, or replace the link with it, and try again", target)
}
