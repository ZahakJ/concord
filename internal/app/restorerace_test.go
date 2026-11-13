package app

import (
	"errors"
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// A forgotten-passphrase restore over another account erases it only once
// the user has confirmed, for the account the device held when they did. If
// the keystore changes after that (here another window changes its
// passphrase), it is not what they agreed to erase: nothing is touched.
func TestRestoreErasesNothingIfTheKeystoreChangedAfterTheConfirmation(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	incoming, _ := identity.Generate()
	testHookRestoreBeforeWrite = func() {
		if err := identity.ChangePassphrase(keystorePathIn(dir), "pw", "changed"); err != nil {
			t.Errorf("change during the restore: %v", err)
		}
	}
	t.Cleanup(func() { testHookRestoreBeforeWrite = nil })
	if err := RestoreOverExisting(dir, phraseOf(t, incoming), "new-pass", incoming.Fingerprint()); err == nil {
		t.Fatal("the restore reported success over a keystore that changed under it")
	}
	deviceIs(t, dir, "changed", bea)
	if store.PeekSetting(dbPathIn(dir), "display_name") != "Bea" {
		t.Fatal("the restore erased the data of an account that changed after the user agreed")
	}
}

// A restore onto a device the caller saw empty does not overwrite an
// account another window put there since.
func TestRestoreIdentityNeverOverwritesAnAccountThatAppeared(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	incoming, _ := identity.Generate()
	err := RestoreIdentity(dir, phraseOf(t, incoming), "new-pass")
	if err == nil {
		t.Fatal("restore wrote over an account that appeared")
	}
	if errors.Is(err, identity.ErrKeystoreExists) {
		t.Fatalf("raw error reached the caller: %v", err)
	}
	deviceIs(t, dir, "pw", bea)
}
