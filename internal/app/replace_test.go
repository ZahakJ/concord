package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// seedDevice writes an account onto dir the way a used install looks while
// locked: a keystore under pass, a database holding a display name and one
// sealed message. legacy writes the keystore without its clear account key,
// as builds before it did.
func seedDevice(t *testing.T, dir, pass, name string, legacy bool) *identity.Identity {
	t.Helper()
	id, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.CreateKeystore(keystorePathIn(dir), pass, id); err != nil {
		t.Fatal(err)
	}
	if legacy {
		stripAccountPub(t, dir)
	}
	st, err := store.Open(dbPathIn(dir), deriveStoreKey(id))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSetting("display_name", name); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveMessage(domain.Message{ID: "m1", ChannelID: "c1", Sender: id.PublicKey(), Content: "a note", Sent: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return id
}

func stripAccountPub(t *testing.T, dir string) {
	t.Helper()
	b, err := os.ReadFile(keystorePathIn(dir))
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	delete(env, "account_pub")
	out, _ := json.Marshal(env)
	if err := os.WriteFile(keystorePathIn(dir), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func phraseOf(t *testing.T, id *identity.Identity) string {
	t.Helper()
	m, err := id.Mnemonic()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func deviceIs(t *testing.T, dir, pass string, want *identity.Identity) {
	t.Helper()
	got, err := identity.LoadKeystore(keystorePathIn(dir), pass)
	if err != nil {
		t.Fatalf("keystore under %q: %v", pass, err)
	}
	if got.Fingerprint() != want.Fingerprint() {
		t.Fatalf("device holds %s, want %s", got.Fingerprint(), want.Fingerprint())
	}
}

func noteSurvives(t *testing.T, dir string, id *identity.Identity) bool {
	t.Helper()
	opens, known := store.KeyOpensMessages(dbPathIn(dir), deriveStoreKey(id))
	return opens && known
}

// A recovery phrase for ANOTHER account must not erase the account on the
// device unless the user confirmed erasing it for exactly that account.
func TestRestoreOverExistingDifferentAccountNeedsConfirmation(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "bea-pass", "Bea", false)
	other, _ := identity.Generate()

	err := RestoreOverExisting(dir, phraseOf(t, other), "new-pass", "")
	var refused *ReplaceRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("different account restored without confirmation: %v", err)
	}
	if refused.Match != MatchDifferent || refused.Device.Name != "Bea" ||
		refused.Device.Fingerprint != bea.Fingerprint() || refused.Incoming != other.Fingerprint() {
		t.Fatalf("refusal does not name both accounts: %+v", refused)
	}
	deviceIs(t, dir, "bea-pass", bea)
	if !noteSurvives(t, dir, bea) {
		t.Fatal("refused restore touched the device's data")
	}

	// Confirmed for some OTHER account (the phrase was edited after agreeing).
	if err := RestoreOverExisting(dir, phraseOf(t, other), "new-pass", ConfirmToken(dir, bea.Fingerprint())); !errors.As(err, &refused) {
		t.Fatalf("a confirmation for a different account was accepted: %v", err)
	}
	deviceIs(t, dir, "bea-pass", bea)

	if err := RestoreOverExisting(dir, phraseOf(t, other), "new-pass", ConfirmToken(dir, other.Fingerprint())); err != nil {
		t.Fatalf("confirmed restore: %v", err)
	}
	deviceIs(t, dir, "new-pass", other)
	if store.PeekSetting(dbPathIn(dir), "display_name") != "" {
		t.Fatal("the erased account's profile survived into the restored one")
	}
}

// The same account on a never-linked install: the phrase re-seals the keystore
// under the new passphrase and everything on the device stays.
func TestRestoreOverExistingSameAccountKeepsData(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "forgotten", "Bea", false)
	if err := RestoreOverExisting(dir, phraseOf(t, bea), "new-pass", ""); err != nil {
		t.Fatalf("same-account restore: %v", err)
	}
	deviceIs(t, dir, "new-pass", bea)
	if _, err := identity.LoadKeystore(keystorePathIn(dir), "forgotten"); err == nil {
		t.Fatal("the forgotten passphrase still opens the device")
	}
	if !noteSurvives(t, dir, bea) || store.PeekSetting(dbPathIn(dir), "display_name") != "Bea" {
		t.Fatal("same-account restore lost the device's data")
	}
}

// A legacy keystore (no clear account key) is matched through its sealed
// messages: the right phrase keeps the data, a wrong one is refused.
// A keystore that names no account in the clear (written before the clear
// key existed, or with it stripped) is a device account nothing can name
// without an unlock: the database may show whose messages it holds, but not
// whose keystore it is. Every restore over it asks first, the account's own
// phrase included.
func TestRestoreOverExistingLegacyKeystore(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "forgotten", "Bea", true)
	other, _ := identity.Generate()
	var refused *ReplaceRefusedError
	if err := RestoreOverExisting(dir, phraseOf(t, other), "new-pass", ""); !errors.As(err, &refused) || refused.Match != MatchUnknown {
		t.Fatalf("legacy device, other phrase: %v", err)
	}
	before, _ := os.ReadFile(keystorePathIn(dir))
	if err := RestoreOverExisting(dir, phraseOf(t, bea), "new-pass", ""); !errors.As(err, &refused) || refused.Match != MatchUnknown {
		t.Fatalf("legacy device, own phrase, went ahead without asking: %v", err)
	}
	// Compared by bytes, not by unlocking: an unlock in this process would
	// itself prove the account and skip the question this test is about.
	if after, _ := os.ReadFile(keystorePathIn(dir)); !bytes.Equal(before, after) {
		t.Fatal("the refused restore touched the keystore")
	}
	// Confirmed, the keystore is replaced, and the data, which the database
	// proves is this account's, is kept.
	if err := RestoreOverExisting(dir, phraseOf(t, bea), "new-pass", refused.Confirm); err != nil {
		t.Fatalf("confirmed restore: %v", err)
	}
	deviceIs(t, dir, "new-pass", bea)
	if !noteSurvives(t, dir, bea) {
		t.Fatal("the confirmed own-phrase restore erased data the database proves is this account's")
	}
}

// Nothing on the device can name its account: refuse, and say we can't tell.
func TestRestoreOverExistingUnknownAccountNeedsConfirmation(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.Generate()
	if err := identity.CreateKeystore(keystorePathIn(dir), "x", id); err != nil {
		t.Fatal(err)
	}
	stripAccountPub(t, dir)
	var refused *ReplaceRefusedError
	if err := RestoreOverExisting(dir, phraseOf(t, id), "new-pass", ""); !errors.As(err, &refused) || refused.Match != MatchUnknown {
		t.Fatalf("unknown device account: %v", err)
	}
	if err := RestoreOverExisting(dir, phraseOf(t, id), "new-pass", ConfirmToken(dir, id.Fingerprint())); err != nil {
		t.Fatal(err)
	}
	deviceIs(t, dir, "new-pass", id)
}

// A LINKED install of the same account cannot keep its data (its device key is
// sealed under the forgotten passphrase), so even the right phrase asks first.
func TestRestoreOverExistingLinkedDeviceNeedsConfirmation(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "forgotten", "Bea", false)
	dev, _ := identity.Generate()
	cert := bea.IssueDeviceCertFor(dev.DevicePublicKey(), "phone", time.Now().Unix())
	if err := saveDeviceMarker(dir, cert); err != nil {
		t.Fatal(err)
	}
	var refused *ReplaceRefusedError
	if err := RestoreOverExisting(dir, phraseOf(t, bea), "new-pass", ""); !errors.As(err, &refused) || refused.Match != MatchSame || !refused.Device.Linked {
		t.Fatalf("linked device, own phrase: %v", err)
	}
	deviceIs(t, dir, "forgotten", bea)
	if err := RestoreOverExisting(dir, phraseOf(t, bea), "new-pass", ConfirmToken(dir, bea.Fingerprint())); err != nil {
		t.Fatal(err)
	}
	deviceIs(t, dir, "new-pass", bea)
	if _, err := os.Stat(deviceMarkerPath(dir)); !os.IsNotExist(err) {
		t.Fatal("stale device marker left behind")
	}
}

// A phrase that is not a phrase touches nothing.
func TestRestoreOverExistingBadPhraseTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	if err := RestoreOverExisting(dir, "not a phrase", "new-pass", "anything"); err == nil {
		t.Fatal("bad phrase accepted")
	}
	deviceIs(t, dir, "pw", bea)
}

// Re-linking a device that holds a different account: refused untouched without
// confirmation; confirmed, the old account is erased and the linked account's
// profile is the one the device presents — the leftover one is never adopted
// or published.
func TestRedeemLinkOverDifferentAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adaDir, cyDir := t.TempDir(), t.TempDir()
	ada := startServiceInDir(t, ctx, adaDir)
	if err := ada.SetProfile(Profile{Name: "Ada", Bio: "the real one"}); err != nil {
		t.Fatal(err)
	}
	cy := startServiceInDir(t, ctx, cyDir)
	time.Sleep(5 * time.Millisecond)
	// Cy's edit is NEWER than Ada's: last-writer-wins would pick it.
	if err := cy.SetProfile(Profile{Name: "Cy", Bio: "leftover"}); err != nil {
		t.Fatal(err)
	}
	cyFP := cy.Fingerprint()
	_ = cy.Close()

	code, err := ada.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	_, err = RedeemLink(ctx, cyDir, code, "new-pass", "")
	var refused *ReplaceRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("link over a different account went ahead unconfirmed: %v", err)
	}
	if refused.Device.Name != "Cy" || refused.IncomingName != "Ada" || refused.Incoming != ada.Fingerprint() {
		t.Fatalf("refusal does not name both accounts: %+v", refused)
	}
	if got, _ := identity.LoadKeystore(keystorePathIn(cyDir), "test-pass"); got == nil || got.Fingerprint() != cyFP {
		t.Fatal("refused link touched the keystore")
	}
	if store.PeekSetting(dbPathIn(cyDir), "display_name") != "Cy" {
		t.Fatal("refused link touched the data")
	}

	code, err = ada.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	res, err := RedeemLink(ctx, cyDir, code, "new-pass", ConfirmToken(cyDir, ada.Fingerprint()))
	if err != nil {
		t.Fatalf("confirmed link: %v", err)
	}
	phone, err := Start(ctx, Config{DataDir: cyDir, Passphrase: "new-pass", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	phone.AdoptLinkedProfile(res.Profile)
	if phone.Fingerprint() != ada.Fingerprint() {
		t.Fatal("device did not take on the linked account")
	}
	if p := phone.SelfProfile(); p.Name != "Ada" || p.Bio != "the real one" {
		t.Fatalf("linked device presents %q / %q, want the account's own profile", p.Name, p.Bio)
	}
	if p := ada.SelfProfile(); p.Name != "Ada" {
		t.Fatalf("the account's profile was overwritten: %q", p.Name)
	}
}
