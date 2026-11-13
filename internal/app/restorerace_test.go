package app

import (
	"encoding/json"
	"errors"
	"os"
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
