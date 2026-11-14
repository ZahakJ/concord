package identity

import (
	"errors"
	"log"
	"os"
	"sync"
	"time"
)

// keystoreMu is held, together with the lock file beside the keystore, by
// everything that replaces or removes the keystore file, so that a write
// conditional on what the file holds (resealIfUnchanged) cannot have another
// write land between its check and its rename.
var keystoreMu sync.Mutex

// keystoreLockWait bounds how long a keystore write waits for another
// process. Every holder is done in about one key derivation; a process stuck
// holding it must not hang a passphrase change, nor keep a background re-seal
// (and the passphrase it holds) alive indefinitely. A variable for tests.
var keystoreLockWait = 10 * time.Second

// tryLock is tryLockFile, a variable so a test can stand in a filesystem
// that cannot lock.
var tryLock = tryLockFile

var lockUnsupported sync.Once

var errKeystoreBusy = errors.New("identity: the keystore is busy in another Concord window or process; try again")

// lockKeystore takes the keystore lock for path: keystoreMu for this process,
// and an advisory lock on path+".lock" for any other process on the same data
// directory (the desktop shell and a web build can both have it unlocked).
// The lock file is never deleted: removing it while another process waits on
// it would let the two lock different files.
//
// The whole wait, for this process's other writers and for other processes,
// is bounded by keystoreLockWait, so a writer queued behind a stuck one fails
// as busy rather than waiting on it indefinitely, holding what it holds.
func lockKeystore(path string) (unlock func(), err error) {
	deadline := time.Now().Add(keystoreLockWait)
	for !keystoreMu.TryLock() {
		if time.Now().After(deadline) {
			return nil, errKeystoreBusy
		}
		time.Sleep(5 * time.Millisecond)
	}
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE|lockOpenFlags, 0o600)
	if err != nil {
		keystoreMu.Unlock()
		// Returned as it is (it names the lock file): wrapped, os.IsNotExist
		// stops recognising a missing data directory.
		return nil, err
	}
	for {
		ok, lerr := tryLock(f)
		if lerr != nil {
			// A filesystem without advisory locks: this process is still
			// ordered by keystoreMu, which is all there was before, and every
			// write still checks the file is what it decided on. Said once, as
			// it means another process on this data directory is not ordered
			// against this one.
			lockUnsupported.Do(func() {
				log.Printf("concord/identity: cannot lock %s (%v); keystore writes are ordered within this process only", path+".lock", lerr)
			})
			ok = true
		}
		if ok {
			// An erase that could not wait for the lock left this to whoever
			// holds it next (see RemoveKeystore). Finish it before anything
			// else, so no holder ever acts on a keystore that was erased.
			finishPendingErase(path)
			return func() {
				delete(staleMarker, path)
				if lerr == nil {
					unlockFile(f)
				}
				_ = f.Close()
				keystoreMu.Unlock()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			keystoreMu.Unlock()
			return nil, errKeystoreBusy
		}
		time.Sleep(10 * time.Millisecond)
	}
}
