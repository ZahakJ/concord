package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// Two flows put an account onto a device that may already hold one: the
// forgotten-passphrase restore from a recovery phrase, and re-linking from
// another device. Both run while the device is LOCKED, so they cannot open
// what is there — and both used to overwrite it without looking. With a phrase
// or code from a different account that silently erased the account on the
// device, and the re-link then published that erased account's leftover
// profile over the real one on every device it had. This file is what lets
// them look first.

// DeviceAccount is what a locked device can say about the account it holds.
type DeviceAccount struct {
	// Fingerprint of the account on this device; "" when it cannot be read
	// without the passphrase (a keystore no build since the clear account key
	// was added has unlocked, on a device that was never linked).
	Fingerprint string `json:"fingerprint"`
	// Verified: Fingerprint is known to be the account inside the keystore,
	// because this process unlocked or wrote exactly the keystore that is
	// there. Otherwise it is what the device's files CLAIM: the clear account
	// key, or a linked device's certificate, which anyone able to write the
	// data directory can change, and which only an unlock can check.
	Verified bool `json:"verified"`
	// Name is the display name stored on this device, "" if none.
	Name string `json:"name"`
	// Linked: this install joined its account by linking, so its device key —
	// sealed under the passphrase — is part of how it takes part in its guilds.
	Linked bool `json:"linked"`
}

// Account match results for MatchDeviceAccount.
const (
	MatchNone      = "none"      // no identity on this device
	MatchSame      = "same"      // the account on the device is this one
	MatchDifferent = "different" // the device holds another account
	MatchUnknown   = "unknown"   // the device holds an account we cannot name
)

// lockedAccountPub reads the account key of the identity on this device
// without the passphrase: from the keystore's clear copy, else from a linked
// device's certificate.
func lockedAccountPub(dataDir string) ed25519.PublicKey {
	if pub, err := identity.ReadAccountPub(keystorePathIn(dataDir)); err == nil && pub != nil {
		return pub
	}
	if m, ok := readMarker(dataDir); ok {
		return m.Cert.AccountPub
	}
	return nil
}

func readMarker(dataDir string) (*deviceMarker, bool) {
	b, err := os.ReadFile(deviceMarkerPath(dataDir))
	if err != nil {
		return nil, false
	}
	var m deviceMarker
	if json.Unmarshal(b, &m) != nil || m.Cert == nil || !m.Cert.Verify() {
		return nil, false
	}
	return &m, true
}

// LockedAccount describes the account on this device without unlocking it.
func LockedAccount(dataDir string) DeviceAccount {
	var out DeviceAccount
	if !HasIdentity(dataDir) {
		return out
	}
	if pub := identity.VerifiedAccount(keystorePathIn(dataDir)); pub != nil {
		out.Fingerprint, out.Verified = identity.FingerprintOf(pub), true
	} else if pub := lockedAccountPub(dataDir); pub != nil {
		out.Fingerprint = identity.FingerprintOf(pub)
	}
	_, out.Linked = readMarker(dataDir)
	out.Name = strings.TrimSpace(store.PeekSetting(dbPathIn(dataDir), "display_name"))
	return out
}

// MatchDeviceAccount compares the account an account seed belongs to with the
// account on this device, believing only what can be checked without the
// passphrase. "same" (which keeps the device's data without a confirmation)
// needs proof: an unlock of exactly this keystore in this process, or the
// database, whose message bodies are sealed under a key derived from the
// account seed. The clear account key in the keystore, and a linked device's
// certificate, are claims anyone able to write the data directory can make;
// on their own they make the device one Concord cannot confirm, and erasing
// it needs the full confirmation.
func MatchDeviceAccount(dataDir string, seed []byte) string {
	m, _ := matchDevice(dataDir, seed)
	return m
}

// matchDevice is MatchDeviceAccount, also reporting whether the device's
// claim about its account is contradicted by the evidence.
func matchDevice(dataDir string, seed []byte) (match string, contradicted bool) {
	if !HasIdentity(dataDir) {
		return MatchNone, false
	}
	incoming, err := identity.FromSeed(seed)
	if err != nil {
		return MatchUnknown, false
	}
	if pub := identity.VerifiedAccount(keystorePathIn(dataDir)); pub != nil {
		if bytes.Equal(pub, incoming.PublicKey()) {
			return MatchSame, false
		}
		return MatchDifferent, false
	}
	claimed := lockedAccountPub(dataDir)
	if claimed == nil {
		// Nothing on the device even claims an account. The database may show
		// whose messages it holds, but not whose keystore this is: replacing
		// it without asking could replace an account that is not the
		// database's. Only an unlock (above) can say whose keystore it is.
		return MatchUnknown, false
	}
	opens, known := store.KeyOpensMessages(dbPathIn(dataDir), deriveStoreKey(incoming))
	if !known {
		return MatchUnknown, false
	}
	if claimed != nil && opens != bytes.Equal(claimed, incoming.PublicKey()) {
		return MatchUnknown, true
	}
	if opens {
		return MatchSame, false
	}
	return MatchDifferent, false
}

// RestoreKeepsData reports whether restoring this seed over the identity on
// the device can keep the device's guilds and history. Only for the same
// account on an install that was never linked: there every key the data is
// bound to (database, MLS signing, network identity) derives from the account
// seed. A linked install also depends on its device key, which is sealed under
// the passphrase that has been forgotten, so its data cannot be carried over.
func RestoreKeepsData(dataDir string, seed []byte) bool {
	if MatchDeviceAccount(dataDir, seed) != MatchSame {
		return false
	}
	_, linked := readMarker(dataDir)
	return !linked
}

// ReplaceRefusedError is returned when putting an account on this device would
// erase the one already here and the caller has not confirmed erasing it FOR
// THIS INCOMING ACCOUNT. Nothing on disk has been touched.
type ReplaceRefusedError struct {
	Match    string        // MatchDifferent, MatchUnknown, or MatchSame on a linked install
	Device   DeviceAccount // what is on the device now
	Incoming string        // fingerprint of the account that would replace it
	// IncomingName is the incoming account's display name when known (a link
	// carries the account's profile; a recovery phrase carries none).
	IncomingName string
	// Confirm is what to pass back as confirm to agree to exactly this
	// replacement (see ConfirmToken).
	Confirm string
}

func (e *ReplaceRefusedError) Error() string {
	return "this would erase the account on this device; confirm to continue"
}

// ConfirmToken is what a yes to erasing this device is bound to: the incoming
// account, and the keystore on the device when the user was shown what it
// holds. The screen that asks hands it back unchanged, so a confirmation is
// for one specific replacement of one specific account: a phrase edited, a
// new code pasted, or the device's account replaced from another window after
// the user agreed is asked about again rather than erased on the strength of
// a yes given for something else.
func ConfirmToken(dataDir, incomingFingerprint string) string {
	snap, _ := identity.ReadKeystore(keystorePathIn(dataDir))
	return confirmToken(dataDir, incomingFingerprint, snap)
}

func confirmToken(dataDir, fp string, snap []byte) string {
	var sum [32]byte
	if snap != nil {
		sum = sha256.Sum256(snap)
	} else {
		// No keystore: what the user is shown, and agrees to erase, is the
		// data on the device, so the yes is bound to that data.
		sum = sha256.Sum256([]byte("data:" + dataIdentity(dataDir)))
	}
	return fp + "@" + hex.EncodeToString(sum[:16])
}

// replaceGate decides whether an account with this seed may be written over
// snap, the keystore on the device as the caller read it. keep: the device's
// data stays. Otherwise erasing is allowed only when confirm is the
// ConfirmToken for the incoming account and exactly that keystore.
func replaceGate(dataDir string, seed []byte, confirm, incomingName string, snap []byte) (keep bool, err error) {
	if !HasIdentity(dataDir) {
		return noKeystoreGate(dataDir, seed, confirm, incomingName, snap)
	}
	if RestoreKeepsData(dataDir, seed) {
		return true, nil
	}
	incoming, ierr := identity.FromSeed(seed)
	if ierr != nil {
		return false, ierr
	}
	fp := incoming.Fingerprint()
	token := confirmToken(dataDir, fp, snap)
	if confirm != "" && confirm == token {
		// Agreed to replace the keystore. Whether the data goes is a separate
		// question: if it opens under the incoming seed it is this account's,
		// and on a never-linked install every key it is bound to comes from
		// that seed, so it is kept. Asking was for the keystore, which
		// nothing could name; the data was never in doubt.
		opens, known := store.KeyOpensMessages(dbPathIn(dataDir), deriveStoreKey(incoming))
		_, linked := readMarker(dataDir)
		return known && opens && !linked, nil
	}
	match, contradicted := matchDevice(dataDir, seed)
	device := LockedAccount(dataDir)
	if contradicted {
		// The device's claim about its account is contradicted by its own
		// data: the confirmation must not show it, not even as a claim.
		device.Fingerprint, device.Verified = "", false
	}
	return false, &ReplaceRefusedError{
		Match:        match,
		Device:       device,
		Incoming:     fp,
		IncomingName: incomingName,
		Confirm:      token,
	}
}

// noKeystoreGate is replaceGate for a device with no keystore. Nothing here:
// go ahead. Data that opens under the incoming seed is this account's: keep it,
// no question, since nothing is erased. Any other data is another account's,
// or nobody's that Concord can prove: the standard confirmation, then it is
// erased before the new keystore lands, never kept beside it.
func noKeystoreGate(dataDir string, seed []byte, confirm, incomingName string, snap []byte) (bool, error) {
	if !hasAccountData(dataDir) {
		return false, nil
	}
	incoming, err := identity.FromSeed(seed)
	if err != nil {
		return false, err
	}
	opens, known := store.KeyOpensMessages(dbPathIn(dataDir), deriveStoreKey(incoming))
	// The one keep rule, as with a keystore present (RestoreKeepsData): the
	// data is this account's AND the install was never linked. A linked
	// install's data is also bound to its device key, which went with the
	// keystore; kept, it would come up on group state for a key it no
	// longer has, beside a device marker that no longer loads.
	_, linked := readMarker(dataDir)
	if known && opens && !linked {
		return true, nil
	}
	fp := incoming.Fingerprint()
	token := confirmToken(dataDir, fp, snap)
	if confirm != "" && confirm == token {
		return false, nil
	}
	match := MatchUnknown
	switch {
	case known && opens: // linked: the account's own, but not keepable
		match = MatchSame
	case known:
		match = MatchDifferent
	}
	return false, &ReplaceRefusedError{
		Match:        match,
		Device:       DeviceAccount{Name: strings.TrimSpace(store.PeekSetting(dbPathIn(dataDir), "display_name")), Linked: linked},
		Incoming:     fp,
		IncomingName: incomingName,
		Confirm:      token,
	}
}

// RestoreOverExisting is the forgotten-passphrase restore. The phrase is
// validated before anything is touched. The same account on a never-linked
// install keeps everything on the device: only the keystore is re-sealed under
// the new passphrase. Anything else erases the device first, and only when
// confirm is the fingerprint of the phrase's account.
func RestoreOverExisting(dataDir, phrase, passphrase, confirm string) error {
	seed, err := identity.SeedFromMnemonic(phrase)
	if err != nil {
		return err
	}
	// The keystore as it is now. The gate below looks at it, the user's
	// confirmation names the account it holds, and both the erase and the
	// write go through only while the file is still exactly this: another
	// window may restore, link or change the passphrase in the meantime, and
	// what it leaves is not what the user agreed to replace.
	ksPath := keystorePathIn(dataDir)
	snap, err := identity.ReadKeystore(ksPath)
	seen := dataIdentity(dataDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	keep, err := replaceGate(dataDir, seed, confirm, "", snap)
	if err != nil {
		return err
	}
	if testHookRestoreBeforeWrite != nil {
		testHookRestoreBeforeWrite()
	}
	// Same account, data kept: only the keystore is replaced. Otherwise the
	// device's data goes too, but only after the restored keystore is safely
	// beside the old one, and in the same hold of the keystore lock: a failure
	// anywhere before the new keystore is in place leaves the old one there,
	// never a device with neither.
	var eraseOld func() error
	if !keep {
		eraseOld = eraseIfUnchanged(dataDir, seen)
	}
	err = identity.RestoreKeystoreWith(ksPath, phrase, passphrase, snap, eraseOld)
	if errors.Is(err, identity.ErrKeystoreChanged) || errors.Is(err, identity.ErrKeystoreExists) {
		return errRestoreRaced
	}
	return err
}

// errRestoreRaced: the keystore changed between the restore looking at it
// and writing. Nothing on this device was changed.
var errRestoreRaced = errors.New("the account on this device changed while you were restoring it (another Concord window?) — nothing was changed; check what is on this device and try again")

// testHookRestoreBeforeWrite runs after the restore has decided and before it
// erases or writes. Nil outside tests.
var testHookRestoreBeforeWrite func()
