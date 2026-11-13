package app

import (
	"bytes"
	"crypto/ed25519"
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
	if pub := lockedAccountPub(dataDir); pub != nil {
		out.Fingerprint = identity.FingerprintOf(pub)
	}
	_, out.Linked = readMarker(dataDir)
	out.Name = strings.TrimSpace(store.PeekSetting(dbPathIn(dataDir), "display_name"))
	return out
}

// MatchDeviceAccount compares the account an account seed belongs to with the
// account on this device.
func MatchDeviceAccount(dataDir string, seed []byte) string {
	if !HasIdentity(dataDir) {
		return MatchNone
	}
	incoming, err := identity.FromSeed(seed)
	if err != nil {
		return MatchUnknown
	}
	// The database's message bodies are sealed under a key derived from the
	// account seed, so a stored message settles it when there is one. The
	// clear key in the keystore is written by Concord but authenticated by
	// nothing, so where the two disagree neither is believed: the device is
	// one Concord cannot name, and erasing it needs the full confirmation.
	opens, known := store.KeyOpensMessages(dbPathIn(dataDir), deriveStoreKey(incoming))
	if pub := lockedAccountPub(dataDir); pub != nil {
		same := bytes.Equal(pub, incoming.PublicKey())
		if known && opens != same {
			return MatchUnknown
		}
		if same {
			return MatchSame
		}
		return MatchDifferent
	}
	if known {
		if opens {
			return MatchSame
		}
		return MatchDifferent
	}
	return MatchUnknown
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
}

func (e *ReplaceRefusedError) Error() string {
	return "this would erase the account on this device; confirm to continue"
}

// replaceGate decides whether an account with this seed may be written over
// whatever is on the device. keep: the device's data stays. Otherwise erasing
// is allowed only when confirm names the incoming account's fingerprint — a
// confirmation is for one specific replacement, so a phrase edited or a new
// code pasted after the user agreed is asked about again.
func replaceGate(dataDir string, seed []byte, confirm, incomingName string) (keep bool, err error) {
	if !HasIdentity(dataDir) {
		return false, nil
	}
	if RestoreKeepsData(dataDir, seed) {
		return true, nil
	}
	incoming, ierr := identity.FromSeed(seed)
	if ierr != nil {
		return false, ierr
	}
	fp := incoming.Fingerprint()
	if confirm != "" && confirm == fp {
		return false, nil
	}
	match, device := MatchDeviceAccount(dataDir, seed), LockedAccount(dataDir)
	if match == MatchUnknown {
		// Whatever the keystore claims about itself is exactly what could not
		// be confirmed; the confirmation must not show it as the account here.
		device.Fingerprint = ""
	}
	return false, &ReplaceRefusedError{
		Match:        match,
		Device:       device,
		Incoming:     fp,
		IncomingName: incomingName,
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
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	keep, err := replaceGate(dataDir, seed, confirm, "")
	if err != nil {
		return err
	}
	if testHookRestoreBeforeWrite != nil {
		testHookRestoreBeforeWrite()
	}
	expected := snap
	if !keep {
		if err := resetIdentityIf(dataDir, snap); err != nil {
			if errors.Is(err, identity.ErrKeystoreChanged) {
				return errRestoreRaced
			}
			return err
		}
		expected = nil
	}
	// The new keystore is written beside the old one and renamed over it, so
	// the kept path is never without a keystore.
	err = identity.RestoreKeystore(ksPath, phrase, passphrase, expected)
	switch {
	case errors.Is(err, identity.ErrKeystoreChanged):
		return errRestoreRaced
	case errors.Is(err, identity.ErrKeystoreExists):
		return errors.New("the account on this device was erased as you confirmed, but another Concord window put a new one here before the restore could; nothing was restored over it")
	}
	return err
}

// errRestoreRaced: the keystore changed between the restore looking at it
// and writing. Nothing on this device was changed.
var errRestoreRaced = errors.New("the account on this device changed while you were restoring it (another Concord window?) — nothing was changed; check what is on this device and try again")

// testHookRestoreBeforeWrite runs after the restore has decided and before it
// erases or writes. Nil outside tests.
var testHookRestoreBeforeWrite func()
