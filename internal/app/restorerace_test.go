package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	if err := RestoreOverExisting(dir, phraseOf(t, incoming), "new-pass", ConfirmToken(dir, incoming.Fingerprint())); err == nil {
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

// The clear account key in a keystore is authenticated by nothing. Rewritten
// to name the account a recovery phrase is for, it used to make a restore
// over somebody else's account "the same account": no confirmation, and the
// other account's keystore replaced. Where the database can contradict it,
// the device is one Concord cannot name, and the confirmation does not show
// the forged key as the account here.
func TestForgedClearKeyCannotSkipTheEraseConfirmation(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	incoming, _ := identity.Generate()
	forgeAccountPub(t, dir, incoming.PublicKey())

	err := RestoreOverExisting(dir, phraseOf(t, incoming), "new-pass", "")
	var refused *ReplaceRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("restore over a forged clear key went ahead unconfirmed: %v", err)
	}
	if refused.Match != MatchUnknown || refused.Device.Fingerprint != "" {
		t.Fatalf("refusal trusts the forged key: match %q, device %q", refused.Match, refused.Device.Fingerprint)
	}
	deviceIs(t, dir, "pw", bea)
}

func forgeAccountPub(t *testing.T, dir string, pub []byte) {
	t.Helper()
	b, err := os.ReadFile(keystorePathIn(dir))
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	env["account_pub"] = pub
	out, _ := json.Marshal(env)
	if err := os.WriteFile(keystorePathIn(dir), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A restore over another account erases the device's data only once the
// restored keystore is safely written: if writing it fails, the device still
// has its keystore and its data, never neither.
func TestFailedRestoreLeavesTheDeviceAsItWas(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	incoming, _ := identity.Generate()
	// An empty passphrase cannot be sealed under: the write fails.
	if err := RestoreOverExisting(dir, phraseOf(t, incoming), "", ConfirmToken(dir, incoming.Fingerprint())); err == nil {
		t.Fatal("restore with an unsealable passphrase reported success")
	}
	deviceIs(t, dir, "pw", bea)
	if store.PeekSetting(dbPathIn(dir), "display_name") != "Bea" {
		t.Fatal("a restore that failed to write erased the device's data")
	}
}

// A yes to erasing this device is a yes for the account the user was shown.
// If another window puts a different account on the device between the
// confirmation screen and the restore, that account is not erased on the
// strength of it: the restore is refused again, naming the account now here.
func TestConfirmationOnlyErasesTheAccountTheUserWasShown(t *testing.T) {
	dir := t.TempDir()
	seedDevice(t, dir, "pw", "Bea", false)
	incoming, _ := identity.Generate()
	shown := ConfirmToken(dir, incoming.Fingerprint()) // the screen, showing Bea

	// Another window replaces Bea with Cy.
	if err := ResetIdentity(dir); err != nil {
		t.Fatal(err)
	}
	cy := seedDevice(t, dir, "cy-pass", "Cy", false)

	err := RestoreOverExisting(dir, phraseOf(t, incoming), "new-pass", shown)
	var refused *ReplaceRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("a confirmation given for Bea erased Cy: %v", err)
	}
	if refused.Device.Name != "Cy" || refused.Confirm == shown {
		t.Fatalf("the new refusal does not ask about Cy: %+v", refused)
	}
	deviceIs(t, dir, "cy-pass", cy)
}

// Erasing an account's data syncs the data directory once everything is
// removed, the group-state directory included, so a power cut before the
// new keystore lands cannot bring the old data back beside it.
func TestResetDataSyncsTheDataDirectory(t *testing.T) {
	dir := t.TempDir()
	seedDevice(t, dir, "pw", "Bea", false)
	if _, err := mlsDirIn(dir); err != nil {
		t.Fatal(err)
	}
	synced := 0
	orig := syncDataDir
	syncDataDir = func(d string) error {
		if d == dir {
			if _, err := os.Stat(filepath.Join(dir, "mls")); !os.IsNotExist(err) {
				t.Errorf("synced before the group state was removed (stat %v)", err)
			}
			synced++
		}
		return orig(d)
	}
	t.Cleanup(func() { syncDataDir = orig })
	if err := resetDataIn(dir); err != nil {
		t.Fatal(err)
	}
	if synced == 0 {
		t.Fatal("erasing the data did not sync the data directory")
	}
}
