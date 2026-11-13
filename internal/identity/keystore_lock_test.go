package identity

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// When CONCORD_TEST_HOLD_KEYSTORE names a keystore, this test binary is the
// second process: it takes the keystore lock, says so, and holds it until its
// stdin closes.
func TestMain(m *testing.M) {
	if path := os.Getenv("CONCORD_TEST_HOLD_KEYSTORE"); path != "" {
		unlock, err := lockKeystore(path)
		if err != nil {
			os.Exit(2)
		}
		// In resurrect mode the holder stands for a write that passed its
		// check before an erase and renames its file in after it.
		blob, _ := os.ReadFile(path)
		_, _ = os.Stdout.WriteString("held\n")
		_, _ = io.Copy(io.Discard, os.Stdin)
		if os.Getenv("CONCORD_TEST_RESURRECT") != "" {
			_ = os.WriteFile(path+".resurrect", blob, 0o600)
			_ = os.Rename(path+".resurrect", path)
		}
		unlock()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A second process on the same data directory (the desktop shell and a web
// build) has its own copy of the in-process lock. A passphrase change in one
// must still wait for a keystore write in the other, or the other's re-seal
// can rename the old passphrase over it.
func TestKeystoreWritesWaitForAnotherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	stdin, cmd := holdInAnotherProcess(t, path, false)
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()

	changed := make(chan error, 1)
	go func() { changed <- ChangePassphrase(path, "old", "new") }()
	select {
	case err := <-changed:
		t.Fatalf("the change went ahead while another process held the keystore (err %v)", err)
	case <-time.After(1500 * time.Millisecond):
	}
	_ = stdin.Close()
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(keystoreLockWait):
		t.Fatal("the change never went ahead after the other process let go")
	}
	assertOpensWithOnly(t, path, "new", "old")
}

// holdInAnotherProcess starts this test binary again as a second process
// holding the keystore lock for path until the returned stdin is closed.
func holdInAnotherProcess(t *testing.T, path string, resurrect bool) (io.WriteCloser, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "CONCORD_TEST_HOLD_KEYSTORE="+path)
	if resurrect {
		cmd.Env = append(cmd.Env, "CONCORD_TEST_RESURRECT=1")
	}
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "held\n" {
		t.Fatalf("helper process did not take the lock: %q %v", line, err)
	}
	return stdin, cmd
}

// An erase that cannot get the lock (another process holds it past the
// wait, stalled in the middle of a write) still erases, and stays an erase
// when that process finally renames its file back in: the keystore never
// reads as present again, and it is gone from the disk once the lock is
// free.
func TestEraseStaysFinalWhenAnotherProcessHoldsTheLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	stdin, cmd := holdInAnotherProcess(t, path, true)
	orig := keystoreLockWait
	keystoreLockWait = 300 * time.Millisecond
	t.Cleanup(func() { keystoreLockWait = orig })

	if err := RemoveKeystore(path); err != nil {
		t.Fatalf("erase with the lock held elsewhere: %v", err)
	}
	if HasKeystore(path) {
		t.Fatal("the keystore is still there right after the erase")
	}
	// The other process finishes its write after the erase, then lets go.
	_ = stdin.Close()
	_ = cmd.Wait()
	if _, err := LoadKeystore(path, "pw"); !os.IsNotExist(err) {
		t.Fatalf("the erased keystore opens again after the other process wrote it back (err %v)", err)
	}
	if HasKeystore(path) {
		t.Fatal("the erased keystore is back")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, kerr := os.Lstat(path)
		if os.IsNotExist(kerr) && !erasePending(path) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the written-back keystore was never removed (stat %v, pending %v)", kerr, erasePending(path))
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A new account can be created here afterwards, as after any erase.
	if _, created, err := LoadOrCreate(path, "next"); err != nil || !created {
		t.Fatalf("create after the erase: created %v, %v", created, err)
	}
}

// On a filesystem that cannot lock, writes still go ahead (ordered within the
// process, and each still checks the file), and the log says so once.
func TestUnlockableFilesystemIsLoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	origLock := tryLock
	tryLock = func(*os.File) (bool, error) { return false, errors.New("locks not supported") }
	lockUnsupported = sync.Once{}
	t.Cleanup(func() { tryLock = origLock; lockUnsupported = sync.Once{} })

	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(path, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "keystore writes are ordered within this process only"); n != 1 {
		t.Fatalf("logged %d times, want once: %q", n, buf.String())
	}
}
