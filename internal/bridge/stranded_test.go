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
	// Never linked, and no message to check a phrase against: not keepable,
	// and not a linked device's data either.
	if v.Keepable || v.Linked {
		t.Fatalf("a never-linked install with no message: %+v, want neither keepable nor linked", v)
	}
	codedAs(t, b.Login("new-pass"), CodeDataWithoutKeystore)
	if appsvc.HasIdentity(dir) {
		t.Fatal("an account was created beside the data")
	}
}

// With a message to check the account's phrase against, never-linked data
// is keepable, and still not a linked device's.
func TestTheFirstScreenSeesKeepableDataAsNotLinked(t *testing.T) {
	t.Setenv("CONCORD_HOME", t.TempDir())
	t.Setenv("CONCORD_DISABLE_MDNS", "1")
	t.Setenv("CONCORD_BOOTSTRAP", "")
	b := New(context.Background())
	t.Cleanup(b.Close)
	if err := b.Login("ada-pass-1"); err != nil {
		t.Fatal(err)
	}
	svc, _ := b.service()
	notes, err := svc.NotesDM()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendMessage(notes.Channels[0].ID, "a note", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.Logout(); err != nil {
		t.Fatal(err)
	}
	dir, _ := appsvc.DataDir()
	if err := os.Remove(filepath.Join(dir, "keystore.json")); err != nil {
		t.Fatal(err)
	}
	v, err := b.StrandedData()
	if err != nil || !v.Stranded || !v.Keepable || v.Linked {
		t.Fatalf("StrandedData = %+v, %v; want stranded, keepable, not linked", v, err)
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

func TestTheHeldFileSentenceAgreesWithHowManyFilesAreHeld(t *testing.T) {
	for _, c := range []struct {
		files []string
		want  string
	}{
		{[]string{"concord.db-wal"}, "The file concord.db-wal is still here, probably because another program is using it."},
		{[]string{"a", "b"}, "The files a, b are still here, probably because another program is using them."},
		{nil, "Some of its files are still here, probably because another program is using them."},
	} {
		want := "Concord couldn't finish erasing this device. " + c.want + " Close any other Concord windows or restart the computer. Concord finishes erasing the next time it starts."
		if got := eraseUnfinishedMsg(c.files); got != want {
			t.Errorf("%v: %q, want %q", c.files, got, want)
		}
	}
}
