package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// holdKeystoreInProcess makes every keystore lock attempt fail as busy within
// 100 ms, as a stuck writer would, until the returned release is called.
func holdKeystoreInProcess(t *testing.T) (release func()) {
	t.Helper()
	orig := keystoreLockWait
	keystoreLockWait = 100 * time.Millisecond
	keystoreMu.Lock()
	done := false
	release = func() {
		if !done {
			done = true
			keystoreMu.Unlock()
		}
	}
	t.Cleanup(func() { release(); finishers.Wait(); keystoreLockWait = orig })
	return release
}

// plantTombstone leaves the marker of an unfinished erase that condemned a
// keystore with exactly these bytes.
func plantTombstone(t *testing.T, path string, condemnedBytes []byte) {
	t.Helper()
	sum := sha256.Sum256(condemnedBytes)
	blob := []byte(`{"sha256":"` + hex.EncodeToString(sum[:]) + `"}`)
	if err := os.WriteFile(path+".erased", blob, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A marker left by an erase that never finished (a crash, an older build or
// a backup putting a keystore back afterwards) must not hide or delete a
// keystore it did not condemn: that keystore opens with its passphrase, a
// wrong passphrase is just wrong, and nothing is created over it.
func TestStaleEraseMarkerNeverDeletesAKeystoreItDidNotCondemn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "first"); err != nil {
		t.Fatal(err)
	}
	plantTombstone(t, path, mustRead(t, path))
	// The keystore that comes back is another one (restored from a backup).
	other := filepath.Join(t.TempDir(), "keystore.json")
	theirs, _, err := LoadOrCreate(other, "theirs")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, mustRead(t, other), 0o600); err != nil {
		t.Fatal(err)
	}

	if !HasKeystore(path) {
		t.Fatal("a keystore on disk reads as absent because of a stale erase marker")
	}
	if _, _, err := LoadOrCreate(path, "wrong"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("unlock with a wrong passphrase: %v, want wrong passphrase (and nothing created)", err)
	}
	got, created, err := LoadOrCreate(path, "theirs")
	if err != nil || created || got.Fingerprint() != theirs.Fingerprint() {
		t.Fatalf("unlock with its own passphrase: created %v, %v", created, err)
	}

	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	if err := ChangePassphrase(path, "theirs", "changed"); err != nil {
		t.Fatalf("the next write: %v", err)
	}
	if erasePending(path) {
		t.Fatal("the stale marker outlived the next lock holder")
	}
	if !strings.Contains(logged.String(), "the keystore now there is kept") {
		t.Fatalf("the stale marker was dropped silently: %q", logged.String())
	}
	if _, err := LoadKeystore(path, "changed"); err != nil {
		t.Fatal(err)
	}
}

// An erase that could not take the lock and then failed to remove the
// keystore is reported as failed, and leaves nothing pending that would
// delete the keystore at a later unlock.
func TestFailedEraseLeavesNothingPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	// Something removal cannot take: a non-empty directory at a name the
	// erase removes.
	if err := os.MkdirAll(filepath.Join(path+".tmp", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	release := holdKeystoreInProcess(t)
	if err := RemoveKeystore(path); err == nil {
		t.Fatal("the erase reported success")
	}
	release()
	if _, err := os.Lstat(path + ".erased"); !os.IsNotExist(err) {
		t.Fatal("a failed erase left an erase pending")
	}
	if !HasKeystore(path) {
		t.Fatal("after a failed erase the keystore reads as absent")
	}
	if _, created, err := LoadOrCreate(path, "pw"); err != nil || created {
		t.Fatalf("unlock after a failed erase: created %v, %v", created, err)
	}
}

// A write that renames its file in just as an erase that could not take the
// lock removes the keystore takes its own file away again: the erase wins,
// and nothing is pending afterwards.
func TestWriteRenamingDuringAnEraseTakesItsFileBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	orig := keystoreLockWait
	keystoreLockWait = 100 * time.Millisecond
	t.Cleanup(func() { finishers.Wait(); keystoreLockWait = orig })
	var eraseErr error
	setHooks(t, resealHooks{beforeRename: func() {
		eraseErr = RemoveKeystore(path) // the change holds the lock: this falls back
	}})
	if err := ChangePassphrase(path, "old", "new"); !errors.Is(err, ErrKeystoreChanged) {
		t.Fatalf("the change that lost to an erase: %v", err)
	}
	if eraseErr != nil {
		t.Fatalf("erase: %v", eraseErr)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("the change's file survived the erase")
	}
	if erasePending(path) {
		t.Fatal("an erase is still pending after the write finished it")
	}
}

// An unlock never creates an account over a keystore that is on the disk:
// one an unfinished erase condemned is erased first, and if that cannot
// happen now the unlock says so.
func TestUnlockNeverCreatesOverAnUnfinishedErase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	plantTombstone(t, path, mustRead(t, path))
	before := mustRead(t, path)
	release := holdKeystoreInProcess(t)
	if _, _, err := LoadOrCreate(path, "new"); !errors.Is(err, errErasePending) {
		t.Fatalf("unlock with the erase unfinished: %v", err)
	}
	if !bytes.Equal(mustRead(t, path), before) {
		t.Fatal("something was written over the keystore")
	}
	release()
	if _, created, err := LoadOrCreate(path, "new"); err != nil || !created {
		t.Fatalf("unlock once the erase can finish: created %v, %v", created, err)
	}
}

// The erase marker is written without following anything planted at its
// name.
func TestEraseMarkerNeverFollowsASymlink(t *testing.T) {
	skipWithoutLinks(t)
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, path+".erased"); err != nil {
		t.Fatal(err)
	}
	holdKeystoreInProcess(t)
	_ = RemoveKeystore(path)
	if got, _ := os.ReadFile(victim); string(got) != "keep me" {
		t.Fatalf("the erase wrote through a planted symlink: %q", got)
	}
}

// An erase syncs the directory, so a power cut cannot undo it, nor leave the
// marker without the removal.
func TestEraseSyncsTheDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	count := func() *int {
		n := 0
		orig := syncDir
		syncDir = func(d string) error {
			if d == dir {
				n++
			}
			return orig(d)
		}
		t.Cleanup(func() { syncDir = orig })
		return &n
	}
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	n := count()
	if err := RemoveKeystore(path); err != nil {
		t.Fatal(err)
	}
	if *n < 1 {
		t.Fatal("an erase under the lock did not sync the directory")
	}

	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	*n = 0
	holdKeystoreInProcess(t)
	if err := RemoveKeystore(path); err != nil {
		t.Fatal(err)
	}
	if *n < 2 {
		t.Fatalf("an erase without the lock synced the directory %d times, want the marker and the removal", *n)
	}
}

// Erases that cannot take the lock run one background finisher per keystore,
// not one each, and it gives up after a bounded number of attempts when the
// lock never comes free.
func TestEraseFinisherIsSingleAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	blob := mustRead(t, path)
	origA, origP := finisherAttempts, finisherPause
	finisherAttempts, finisherPause = 3, 10*time.Millisecond
	t.Cleanup(func() { finisherAttempts, finisherPause = origA, origP })
	release := holdKeystoreInProcess(t) // the lock never comes free

	for i := range 3 {
		if err := os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := RemoveKeystore(path); err != nil {
			t.Fatalf("erase %d: %v", i, err)
		}
	}
	running := 0
	finishing.Range(func(any, any) bool { running++; return true })
	if running > 1 {
		t.Fatalf("%d finishers for one keystore", running)
	}
	gaveUp := make(chan struct{})
	go func() { finishers.Wait(); close(gaveUp) }()
	select {
	case <-gaveUp:
	case <-time.After(10 * time.Second):
		release()
		t.Fatal("the finisher never gave up on a lock that never came free")
	}
}

// Erasing a keystore that is a symbolic link is refused with an error naming
// the file it points to; removing only the link would report an erase while
// the sealed keystore survives.
func TestEraseRefusesASymlinkedKeystore(t *testing.T) {
	skipWithoutLinks(t)
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if _, _, err := LoadOrCreate(real, "pw"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "keystore.json")
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	err := RemoveKeystore(path)
	if err == nil || !strings.Contains(err.Error(), real) {
		t.Fatalf("erase of a symlinked keystore: %v, want a refusal naming %s", err, real)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("the link was removed")
	}
	if _, err := LoadKeystore(real, "pw"); err != nil {
		t.Fatalf("the keystore it points to changed: %v", err)
	}
}

// A write that renames in as an erase is asked for, and then cannot remove
// its own file (a scanner holding it, on Windows), must still lose to the
// erase: the file it wrote is marked as erased, reads as absent, and the next
// holder of the lock deletes it. The write reports the failure.
func TestEraseWinsEvenWhenAWriteCannotRemoveItsOwnFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	orig := keystoreLockWait
	keystoreLockWait = 100 * time.Millisecond
	// The seam is installed once and switched with an atomic flag: the
	// erase below starts a background finisher that reads it.
	origRemove := removeFile
	var failing atomic.Bool
	removeFile = func(p string) error {
		if failing.Load() && p == path {
			return os.ErrPermission
		}
		return origRemove(p)
	}
	t.Cleanup(func() { finishers.Wait(); keystoreLockWait = orig; removeFile = origRemove })
	setHooks(t, resealHooks{beforeRename: func() {
		if err := RemoveKeystore(path); err != nil { // falls back: the change holds the lock
			t.Errorf("erase: %v", err)
		}
		failing.Store(true)
	}})
	err := ChangePassphrase(path, "old", "new")
	failing.Store(false)
	if HasKeystore(path) {
		t.Error("the file the change wrote reads as a keystore after the erase")
	}
	unlock, lerr := lockKeystore(path) // the next holder of the lock
	if lerr != nil {
		t.Fatal(lerr)
	}
	unlock()
	if _, serr := os.Lstat(path); !os.IsNotExist(serr) {
		t.Error("the next holder of the lock kept the file the erase should have taken")
	}
	if !errors.Is(err, ErrKeystoreChanged) || err == ErrKeystoreChanged {
		t.Errorf("the change that lost to an erase and could not undo itself: %v, want the failure reported", err)
	}
}

// An old erase marker that names no keystore here and cannot be removed must
// not make every write fail as if an erase had just been asked for.
func TestUnremovableStaleMarkerDoesNotBlockWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	plantTombstone(t, path, []byte("some other keystore"))
	origRemove := removeFile
	removeFile = func(p string) error {
		if p == path+".erased" {
			return os.ErrPermission
		}
		return origRemove(p)
	}
	t.Cleanup(func() { removeFile = origRemove })
	if err := ChangePassphrase(path, "old", "new"); err != nil {
		t.Fatalf("a write with an unremovable stale marker beside it: %v", err)
	}
	if _, err := LoadKeystore(path, "new"); err != nil {
		t.Fatal(err)
	}
}

// An erase that could not take the lock and then could neither remove the
// keystore nor take its marker back will still happen (the marker names the
// keystore that is there), so it is reported as pending, not as failed, and
// it does complete.
func TestEraseThatCannotRollBackIsReportedPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	origRemove := removeFile
	t.Cleanup(func() { removeFile = origRemove })
	var failing atomic.Bool // read by the background finisher too
	failing.Store(true)
	removeFile = func(p string) error {
		if failing.Load() && (p == path || p == path+".erased") {
			return os.ErrPermission
		}
		return origRemove(p)
	}
	release := holdKeystoreInProcess(t)
	err := RemoveKeystore(path)
	if !errors.Is(err, ErrEraseDeferred) {
		t.Fatalf("erase that will complete later: %v, want it reported as pending", err)
	}
	if HasKeystore(path) {
		t.Fatal("a keystore whose erase is pending still reads as present")
	}
	failing.Store(false)
	release()
	finishers.Wait()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("the pending erase never completed")
	}
}

// An old erase marker that can be neither read nor removed condemns nothing
// identifiable, and must not make every write fail either.
func TestUnreadableUnremovableMarkerDoesNotBlockWrites(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	plantTombstone(t, path, []byte("some other keystore"))
	if err := os.Chmod(path+".erased", 0o000); err != nil {
		t.Fatal(err)
	}
	origRemove := removeFile
	removeFile = func(p string) error {
		if p == path+".erased" {
			return os.ErrPermission
		}
		return origRemove(p)
	}
	t.Cleanup(func() { removeFile = origRemove; _ = os.Chmod(path+".erased", 0o600) })
	if err := ChangePassphrase(path, "old", "new"); err != nil {
		t.Fatalf("a write beside an unreadable, unremovable old marker: %v", err)
	}
	if _, err := LoadKeystore(path, "new"); err != nil {
		t.Fatal(err)
	}
}
