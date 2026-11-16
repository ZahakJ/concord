package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
)

func codedAs(t *testing.T, err error, code string) *CodedError {
	t.Helper()
	var c *CodedError
	if !errors.As(err, &c) || c.Code != code {
		t.Fatalf("got %v, want code %q", err, code)
	}
	if strings.Contains(c.Msg, "app:") {
		t.Fatalf("the message is not plain: %q", c.Msg)
	}
	return c
}

// The first screen learns, before any passphrase, that this device holds
// data it can no longer open; Login then creates nothing beside it.
func TestTheFirstScreenSeesDataItCannotOpen(t *testing.T) {
	b, _ := newLockedDevice(t, "ada-pass-1", "Ada")
	dir, _ := appsvc.DataDir()
	if err := os.Remove(filepath.Join(dir, "keystore.json")); err != nil {
		t.Fatal(err)
	}
	v, err := b.StrandedData()
	if err != nil || !v.Stranded {
		t.Fatalf("StrandedData = %+v, %v", v, err)
	}
	codedAs(t, b.Login("new-pass"), CodeDataWithoutKeystore)
	if appsvc.HasIdentity(dir) {
		t.Fatal("an account was created beside the data")
	}
}

// A process that starts with an unlink's erase still recorded says so on
// the lock screen, and the start that finishes it says the device was
// unlinked and erased.
func TestALockScreenAfterARestartSaysTheDeviceWasUnlinked(t *testing.T) {
	newLockedDevice(t, "ada-pass-1", "Ada")
	dir, _ := appsvc.DataDir()
	_ = os.Remove(filepath.Join(dir, "keystore.json"))
	if err := os.WriteFile(filepath.Join(dir, "erase-in-progress"), []byte(`{"unlinked":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(context.Background())
	t.Cleanup(b.Close)
	if got := b.SessionState().Reason; got != LockUnlinkedNotErased {
		t.Fatalf("lock reason after a restart: %q", got)
	}
	codedAs(t, b.Login("new-pass"), CodeDeviceUnlinked)
	if got := b.SessionState().Reason; got != LockUnlinked {
		t.Fatalf("lock reason after the erase finished: %q", got)
	}
}

// A file the erase cannot remove is named, with what fixes it.
func TestAHeldFileIsNamedWithWhatFixesIt(t *testing.T) {
	newLockedDevice(t, "ada-pass-1", "Ada")
	dir, _ := appsvc.DataDir()
	_ = os.Remove(filepath.Join(dir, "keystore.json"))
	if err := os.WriteFile(filepath.Join(dir, "erase-in-progress"), []byte(`{"unlinked":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	held := filepath.Join(dir, "concord.db-wal")
	_ = os.Remove(held)
	if err := os.MkdirAll(filepath.Join(held, "held"), 0o700); err != nil {
		t.Fatal(err)
	}
	b := New(context.Background())
	t.Cleanup(b.Close)
	c := codedAs(t, b.Login("new-pass"), CodeEraseUnfinished)
	files := c.Detail.(EraseUnfinishedView).Files
	if len(files) == 0 || !strings.Contains(strings.Join(files, ","), "concord.db-wal") || !strings.Contains(c.Msg, "concord.db-wal") || !strings.Contains(c.Msg, "Close any other Concord windows") {
		t.Fatalf("held file: %q %v", c.Msg, files)
	}
}
