package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// unverifiedKeystore puts an account's keystore on dir as another process
// (or someone with the files) would: same content, but bytes this process
// never unlocked or wrote, so nothing here has verified what it holds.
func unverifiedKeystore(t *testing.T, dir, pass string, id *identity.Identity, claim []byte) {
	t.Helper()
	if err := identity.CreateKeystore(keystorePathIn(dir), pass, id); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(keystorePathIn(dir))
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	if claim != nil {
		env["account_pub"] = claim
	}
	out, _ := json.Marshal(env) // re-encoded: other bytes than were written
	if err := os.WriteFile(keystorePathIn(dir), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// With no stored message to check against and no unlock in this process,
// the clear account key is a claim: a forged one naming the phrase's account
// does not make a restore "the same account" (no confirmation, data kept).
// The refusal shows what the file claims, marked unverified.
func TestForgedClearKeyWithoutMessagesIsOnlyAClaim(t *testing.T) {
	dir := t.TempDir()
	bea, _ := identity.Generate()
	incoming, _ := identity.Generate()
	unverifiedKeystore(t, dir, "pw", bea, incoming.PublicKey())

	err := RestoreOverExisting(dir, phraseOf(t, incoming), "new-pass", "")
	var refused *ReplaceRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("a forged clear key alone made the restore go ahead: %v", err)
	}
	if refused.Match != MatchUnknown || refused.Device.Verified || refused.Device.Fingerprint != incoming.Fingerprint() {
		t.Fatalf("refusal: match %q, device %+v; want unknown, showing the unverified claim", refused.Match, refused.Device)
	}
	deviceIs(t, dir, "pw", bea)
}

// The same goes for a true claim: keeping the data needs proof. After an
// unlock of exactly this keystore in this process, it has it.
func TestKeepingDataNeedsProofNotTheClearKey(t *testing.T) {
	dir := t.TempDir()
	ada, _ := identity.Generate()
	unverifiedKeystore(t, dir, "pw", ada, nil)
	if MatchDeviceAccount(dir, ada.Seed()) != MatchUnknown {
		t.Fatal("the clear key alone confirmed the account")
	}
	if a := LockedAccount(dir); a.Verified || a.Fingerprint != ada.Fingerprint() {
		t.Fatalf("locked account before an unlock: %+v", a)
	}
	if _, err := identity.LoadKeystore(keystorePathIn(dir), "pw"); err != nil {
		t.Fatal(err)
	}
	if MatchDeviceAccount(dir, ada.Seed()) != MatchSame {
		t.Fatal("after an unlock of this keystore the account is still unconfirmed")
	}
	if a := LockedAccount(dir); !a.Verified {
		t.Fatalf("locked account after an unlock: %+v", a)
	}
}

// A group-state directory that cannot be removed is an error, so a restore
// or link stops before its new keystore lands beside the old account's
// group state.
func TestResetDataReportsGroupStateItCannotRemove(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove anything")
	}
	dir := t.TempDir()
	stuck := filepath.Join(dir, "mls", "group")
	if err := os.MkdirAll(stuck, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "state"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stuck, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stuck, 0o700) })
	if err := resetDataIn(dir); err == nil {
		t.Fatal("erasing the data reported success with the group state still there")
	}
}

// A keystore erase that had to be deferred and a data erase that failed are
// both reported: the second alone reads as "nothing happened", while the
// keystore erase still completes later.
func TestDeferredEraseWithDataLeftSaysBoth(t *testing.T) {
	dir := t.TempDir()
	seedDevice(t, dir, "pw", "Bea", false)
	orig := removeKeystore
	removeKeystore = func(string) error { return identity.ErrEraseDeferred }
	t.Cleanup(func() { removeKeystore = orig })
	// Something the data erase cannot take.
	if err := os.Remove(dbPathIn(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dbPathIn(dir), "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := ResetIdentity(dir)
	if !errors.Is(err, identity.ErrEraseDeferred) || !errors.Is(err, ErrDataNotErased) {
		t.Fatalf("deferred keystore erase with data left: %v, want both said", err)
	}
	if !strings.Contains(err.Error(), "concord.db") {
		t.Fatalf("the error does not say what is left: %v", err)
	}
}

// With the keystore gone, restoring the phrase of the account whose data is
// here keeps that data without asking: nothing is erased.
func TestRestoreWithNoKeystoreKeepsTheAccountsOwnData(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	if err := os.Remove(keystorePathIn(dir)); err != nil {
		t.Fatal(err)
	}
	if err := RestoreIdentity(dir, phraseOf(t, bea), "new-pass"); err != nil {
		t.Fatal(err)
	}
	deviceIs(t, dir, "new-pass", bea)
	if store.PeekSetting(dbPathIn(dir), "display_name") != "Bea" {
		t.Fatal("restoring the account's own phrase lost its data")
	}
}

// Restoring a different account's phrase there must not come up beside the
// old account's data: it is asked about, and erased once confirmed.
func TestRestoreWithNoKeystoreAsksBeforeErasingAnotherAccountsData(t *testing.T) {
	dir := t.TempDir()
	seedDevice(t, dir, "pw", "Bea", false)
	if err := os.Remove(keystorePathIn(dir)); err != nil {
		t.Fatal(err)
	}
	other, _ := identity.Generate()
	err := RestoreIdentity(dir, phraseOf(t, other), "new-pass")
	var refused *ReplaceRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("another account restored beside Bea's data without asking: %v", err)
	}
	if store.PeekSetting(dbPathIn(dir), "display_name") != "Bea" {
		t.Fatal("the refusal touched the data")
	}
	if err := RestoreOverExisting(dir, phraseOf(t, other), "new-pass", refused.Confirm); err != nil {
		t.Fatalf("confirmed: %v", err)
	}
	deviceIs(t, dir, "new-pass", other)
	if store.PeekSetting(dbPathIn(dir), "display_name") == "Bea" {
		t.Fatal("the restored account came up beside the old account's data")
	}
}

// No create ever deletes data it finds without a keystore, whatever else is
// on the device, a leftover erase record included (written by an interim
// build): it refuses and leaves everything.
func TestCreateGuardNeverDeletes(t *testing.T) {
	dir := t.TempDir()
	ks := keystorePathIn(dir)
	if err := guardCreate(ks); err != nil {
		t.Fatalf("an empty device: %v", err)
	}
	seedDevice(t, dir, "pw", "Bea", false)
	if err := os.Remove(ks); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data-erase-pending"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guardCreate(ks); !errors.Is(err, ErrDataWithoutKeystore) {
		t.Fatalf("data and an erase record, no keystore: %v", err)
	}
	if store.PeekSetting(dbPathIn(dir), "display_name") != "Bea" {
		t.Fatal("a create deleted data")
	}
}

// The reviewer's probe: a wipe refused (the keystore is a symlink, "nothing
// was erased"), the link then replaced by the file, the account used, and
// later its keystore lost. The unlock that follows deletes nothing.
func TestARefusedWipeNeverDeletesLater(t *testing.T) {
	dir := t.TempDir()
	seedDevice(t, dir, "pw", "Bea", false)
	ks := keystorePathIn(dir)
	real := filepath.Join(t.TempDir(), "keystore.json")
	if err := os.Rename(ks, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, ks); err != nil {
		t.Skip("no symlinks here")
	}
	if err := ResetIdentity(dir); err == nil {
		t.Fatal("a wipe through a symlinked keystore reported success")
	}
	_ = os.Remove(ks)
	if err := os.Rename(real, ks); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.LoadKeystore(ks, "pw"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ks); err != nil { // lost, later
		t.Fatal(err)
	}
	if _, _, err := identity.LoadOrCreate(ks, "new"); !errors.Is(err, ErrDataWithoutKeystore) {
		t.Fatalf("unlock after a refused wipe and a lost keystore: %v", err)
	}
	if store.PeekSetting(dbPathIn(dir), "display_name") != "Bea" {
		t.Fatal("the data was deleted on the say-so of a wipe that never happened")
	}
}

// The reviewer's other probe: a wipe that failed, the keystore later lost,
// and the account's own phrase restored. The data is the account's and is
// kept, not erased without a question.
func TestAFailedWipeNeverErasesAnOwnPhraseRestore(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	orig := removeKeystore
	removeKeystore = func(string) error { return errors.New("held by another program") }
	err := ResetIdentity(dir)
	removeKeystore = orig
	if err == nil {
		t.Fatal("the failing wipe reported success")
	}
	if err := os.Remove(keystorePathIn(dir)); err != nil { // lost, later
		t.Fatal(err)
	}
	if err := RestoreIdentity(dir, phraseOf(t, bea), "new-pass"); err != nil {
		t.Fatalf("own-phrase restore: %v", err)
	}
	if !noteSurvives(t, dir, bea) {
		t.Fatal("the restore erased the account's messages on the say-so of a failed wipe")
	}
}

// With no keystore, a linked install's own data is not kept: it is bound to
// the device key that went with the keystore. The restore asks, as it does
// with the keystore present, and once confirmed the data and the device
// marker go, leaving no dead marker behind.
func TestNoKeystoreLinkedInstallAsksAndLeavesNoDeadMarker(t *testing.T) {
	dir := t.TempDir()
	bea := seedDevice(t, dir, "pw", "Bea", false)
	dev, _ := identity.Generate()
	if err := saveDeviceMarker(dir, bea.IssueDeviceCertFor(dev.DevicePublicKey(), "phone", time.Now().Unix())); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keystorePathIn(dir)); err != nil {
		t.Fatal(err)
	}
	err := RestoreIdentity(dir, phraseOf(t, bea), "new-pass")
	var refused *ReplaceRefusedError
	if !errors.As(err, &refused) || refused.Match != MatchSame || !refused.Device.Linked {
		t.Fatalf("linked install, no keystore, own phrase: %v, want the linked-install question", err)
	}
	if !noteSurvives(t, dir, bea) {
		t.Fatal("the refusal touched the data")
	}
	if err := RestoreOverExisting(dir, phraseOf(t, bea), "new-pass", refused.Confirm); err != nil {
		t.Fatalf("confirmed: %v", err)
	}
	if _, err := os.Stat(deviceMarkerPath(dir)); !os.IsNotExist(err) {
		t.Fatal("a dead device marker was left behind")
	}
	if noteSurvives(t, dir, bea) {
		t.Fatal("a linked install's data was kept")
	}
}

// With no keystore, a yes is bound to the data it was given for: one given
// for Bea's data, replayed after the data was replaced by Carol's, erases
// nothing and asks again.
func TestNoKeystoreConfirmationIsBoundToTheData(t *testing.T) {
	dir := t.TempDir()
	seedDevice(t, dir, "pw", "Bea", false)
	if err := os.Remove(keystorePathIn(dir)); err != nil {
		t.Fatal(err)
	}
	incoming, _ := identity.Generate()
	err := RestoreIdentity(dir, phraseOf(t, incoming), "new-pass")
	var refused *ReplaceRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("restore over Bea's data: %v", err)
	}
	forBea := refused.Confirm
	// Bea's data is replaced by Carol's.
	for _, p := range []string{dbPathIn(dir), dbPathIn(dir) + "-wal", dbPathIn(dir) + "-shm"} {
		_ = os.Remove(p)
	}
	seedDevice(t, dir, "carol", "Carol", false)
	if err := os.Remove(keystorePathIn(dir)); err != nil {
		t.Fatal(err)
	}
	err = RestoreOverExisting(dir, phraseOf(t, incoming), "new-pass", forBea)
	if !errors.As(err, &refused) {
		t.Fatalf("a yes for Bea's data was applied to Carol's: %v", err)
	}
	if store.PeekSetting(dbPathIn(dir), "display_name") != "Carol" {
		t.Fatal("Carol's data was erased on a yes given for Bea's")
	}
}

// A restore onto a device with no data erases nothing that appears between
// its decision and its write (a data directory being copied in): the erase
// is checked under the lock, at the write, against what was decided on.
func TestDataAppearingBeforeTheWriteIsNotErased(t *testing.T) {
	dir := t.TempDir()
	incoming, _ := identity.Generate()
	testHookRestoreBeforeWrite = func() {
		if err := os.WriteFile(dbPathIn(dir), []byte("copied in"), 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookRestoreBeforeWrite = nil })
	if err := RestoreIdentity(dir, phraseOf(t, incoming), "new-pass"); err == nil {
		t.Fatal("the restore went ahead over data that appeared after its decision")
	}
	if got, _ := os.ReadFile(dbPathIn(dir)); string(got) != "copied in" {
		t.Fatal("data that appeared after the decision was erased")
	}
}
