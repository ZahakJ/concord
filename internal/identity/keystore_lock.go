package identity

import (
	"errors"
	"fmt"
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

var errKeystoreBusy = errors.New("identity: the keystore is busy in another Concord process; try again")

// lockKeystore takes the keystore lock for path: keystoreMu for this process,
// and an advisory lock on path+".lock" for any other process on the same data
// directory (the desktop shell and a web build can both have it unlocked).
// The lock file is never deleted: removing it while another process waits on
// it would let the two lock different files.
func lockKeystore(path string) (unlock func(), err error) {
	keystoreMu.Lock()
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		keystoreMu.Unlock()
		return nil, fmt.Errorf("identity: open keystore lock: %w", err)
	}
	deadline := time.Now().Add(keystoreLockWait)
	for {
		ok, lerr := tryLockFile(f)
		if lerr != nil {
			// A filesystem without advisory locks: this process is still
			// ordered by keystoreMu, which is all there was before.
			ok = true
		}
		if ok {
			// An erase that could not wait for the lock left this to whoever
			// holds it next (see RemoveKeystore). Finish it before anything
			// else, so no holder ever acts on a keystore that was erased.
			finishPendingErase(path)
			return func() {
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
