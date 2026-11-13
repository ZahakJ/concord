package identity

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		_, _ = os.Stdout.WriteString("held\n")
		_, _ = io.Copy(io.Discard, os.Stdin)
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
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "CONCORD_TEST_HOLD_KEYSTORE="+path)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "held\n" {
		t.Fatalf("helper process did not take the lock: %q %v", line, err)
	}

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
