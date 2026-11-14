package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// changePassphraseDuringLink changes the passphrase of the keystore in dir at
// the moment the link has its answer from the issuer and is about to write:
// what another window or process can do while a link waits on the network.
func changePassphraseDuringLink(t *testing.T, dir, from, to string) {
	t.Helper()
	testHookLinkBeforeWrite = func() {
		if err := identity.ChangePassphrase(keystorePathIn(dir), from, to); err != nil {
			t.Errorf("change during the link: %v", err)
		}
	}
	t.Cleanup(func() { testHookLinkBeforeWrite = nil })
}

func opensOnlyWith(t *testing.T, dir, want, notWant string, account *identity.Identity) {
	t.Helper()
	got, err := identity.LoadKeystore(keystorePathIn(dir), want)
	if err != nil {
		t.Fatalf("passphrase %q no longer opens the keystore: %v", want, err)
	}
	if got.Fingerprint() != account.Fingerprint() {
		t.Fatalf("the keystore holds %s, want %s", got.Fingerprint(), account.Fingerprint())
	}
	if _, err := identity.LoadKeystore(keystorePathIn(dir), notWant); !errors.Is(err, identity.ErrWrongPassphrase) {
		t.Fatalf("passphrase %q opens the keystore again (err %v): the link sealed it back", notWant, err)
	}
}

// Re-linking a never-linked install of the same account keeps its data and
// re-seals its keystore under the link's passphrase. A passphrase change
// that lands while the link is on the network must not be sealed back: the
// link fails and leaves the changed keystore as it is.
func TestRelinkNeverRevertsAPassphraseChangedDuringIt(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adaDir, phoneDir := t.TempDir(), t.TempDir()
	ada := startServiceInDir(t, ctx, adaDir)
	adaID, err := identity.LoadKeystore(keystorePathIn(adaDir), "test-pass")
	if err != nil {
		t.Fatal(err)
	}
	// The same account, never linked, on the phone.
	if err := identity.RestoreKeystore(keystorePathIn(phoneDir), phraseOf(t, adaID), "test-pass", nil); err != nil {
		t.Fatal(err)
	}
	code, err := ada.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	changePassphraseDuringLink(t, phoneDir, "test-pass", "changed")
	if _, err := RedeemLink(ctx, phoneDir, code, "test-pass", ""); err == nil {
		t.Fatal("the link reported success over a keystore that changed under it")
	}
	opensOnlyWith(t, phoneDir, "changed", "test-pass", adaID)
}

// Linking over a different account erases it only once the user confirmed.
// If the keystore changes after the user agreed (here: its passphrase), it
// is no longer exactly what they agreed to erase, and nothing is touched.
func TestConfirmedLinkErasesNothingIfTheKeystoreChangedMeanwhile(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adaDir, cyDir := t.TempDir(), t.TempDir()
	ada := startServiceInDir(t, ctx, adaDir)
	cy := seedDevice(t, cyDir, "test-pass", "Cy", false)
	code, err := ada.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	changePassphraseDuringLink(t, cyDir, "test-pass", "changed")
	if _, err := RedeemLink(ctx, cyDir, code, "test-pass", ConfirmToken(cyDir, ada.Fingerprint())); err == nil {
		t.Fatal("the link reported success over a keystore that changed under it")
	}
	opensOnlyWith(t, cyDir, "changed", "test-pass", cy)
	if store.PeekSetting(dbPathIn(cyDir), "display_name") != "Cy" {
		t.Fatal("the link erased the data of an account that changed after the user agreed")
	}
}

// A link onto a device with no data erases nothing that appears after its
// decision and before its write (a data directory being copied in while the
// link waits): the erase is checked under the lock, at the write.
func TestLinkErasesNothingThatAppearsAfterItsDecision(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adaDir, phoneDir := t.TempDir(), t.TempDir()
	ada := startServiceInDir(t, ctx, adaDir)
	code, err := ada.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	testHookLinkBeforeWrite = func() {
		if err := os.WriteFile(dbPathIn(phoneDir), []byte("copied in"), 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookLinkBeforeWrite = nil })
	if _, err := RedeemLink(ctx, phoneDir, code, "test-pass", ""); err == nil {
		t.Fatal("the link went ahead over data that appeared after its decision")
	}
	if got, _ := os.ReadFile(dbPathIn(phoneDir)); string(got) != "copied in" {
		t.Fatal("data that appeared after the link's decision was erased")
	}
}
