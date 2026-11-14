package bridge

import (
	"errors"
	"strings"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

// CodedError is an error the UI must branch on rather than print. Its Code
// travels beside the message in the RPC envelope (see httpapi), so the shell
// never has to recognise an outcome by the wording of a sentence — wording
// changes, and a regex over it once told users their data had been erased
// when the core had merely restarted.
type CodedError struct {
	Code   string
	Msg    string
	Detail any
}

func (e *CodedError) Error() string     { return e.Msg }
func (e *CodedError) ErrorCode() string { return e.Code }
func (e *CodedError) ErrorDetail() any  { return e.Detail }

// Error codes the UI branches on.
const (
	CodeLocked              = "locked"                // no unlocked session; ask SessionState why
	CodeReplaceRefused      = "replace-refused"       // would erase the account on this device; detail says whose
	CodeWrongPassphrase     = "wrong-passphrase"      // the passphrase does not open this device's keystore
	CodeErasePending        = "erase-pending"         // erased; the keystore file itself goes at the next chance (not a failure)
	CodeDataWithoutKeystore = "data-without-keystore" // an account's data but no keystore: restore or start over; nothing was created
	CodeConfirmWord         = "confirm-word"          // start over needs StartOverWord typed; nothing was deleted
)

// Why a running core has no session, for a window that had one. A fresh
// process is locked by construction, so "restarted" is what the core can say
// when nothing in THIS process ended the session.
const (
	LockRestarted = "restarted"  // the core started (crash, update, OS restart) and was never unlocked
	LockSignedOut = "signed-out" // someone signed out (another window, or this one)
	LockUnlinked  = "unlinked"   // the account unlinked this device and it erased itself
	LockWiped     = "wiped"      // "Delete everything on this device" ran
)

// SessionView answers "what happened to my session?" for a window that just
// got CodeLocked.
type SessionView struct {
	Unlocked    bool   `json:"unlocked"`
	HasIdentity bool   `json:"hasIdentity"`
	Reason      string `json:"reason,omitempty"` // a Lock* value; "" while unlocked
}

// SessionState reports whether a session is open and, if not, why not.
func (b *Bridge) SessionState() SessionView {
	b.mu.Lock()
	unlocked, reason := b.svc != nil, b.lockReason
	b.mu.Unlock()
	v := SessionView{Unlocked: unlocked}
	if has, err := b.HasIdentity(); err == nil {
		v.HasIdentity = has
	}
	if !unlocked {
		v.Reason = reason
		if v.Reason == "" {
			v.Reason = LockRestarted
		}
	}
	return v
}

// LockedAccount names the account on this device without unlocking it, for
// the lock screen and the flows that would replace it.
func (b *Bridge) LockedAccount() (appsvc.DeviceAccount, error) {
	dir, err := appsvc.DataDir()
	if err != nil {
		return appsvc.DeviceAccount{}, err
	}
	return appsvc.LockedAccount(dir), nil
}

// PhraseCheck is what the forgot-passphrase restore shows before it does
// anything.
type PhraseCheck struct {
	// Match: none (nothing on the device), same, different or unknown.
	Match string `json:"match"`
	// KeepsData: restoring keeps this device's guilds and history.
	KeepsData bool `json:"keepsData"`
	// Device is the account on the device now.
	Device appsvc.DeviceAccount `json:"device"`
	// Incoming is the fingerprint of the phrase's account.
	Incoming string `json:"incoming"`
	// Confirm is what a confirmation to erase sends back: the incoming
	// account over the keystore the device holds now (app.ConfirmToken).
	Confirm string `json:"confirm"`
}

// CheckRecoveryPhrase validates a recovery phrase and says what restoring it
// on this device would do. It changes nothing.
func (b *Bridge) CheckRecoveryPhrase(phrase string) (PhraseCheck, error) {
	seed, err := identity.SeedFromMnemonic(phrase)
	if err != nil {
		return PhraseCheck{}, errBadPhrase
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return PhraseCheck{}, err
	}
	id, err := identity.FromSeed(seed)
	if err != nil {
		return PhraseCheck{}, errBadPhrase
	}
	return PhraseCheck{
		Match:     appsvc.MatchDeviceAccount(dir, seed),
		KeepsData: appsvc.RestoreKeepsData(dir, seed),
		Device:    appsvc.LockedAccount(dir),
		Incoming:  id.Fingerprint(),
		Confirm:   appsvc.ConfirmToken(dir, id.Fingerprint()),
	}, nil
}

var errBadPhrase = errors.New("that recovery phrase isn't valid — check each word and their order")

// ReplaceRefusal is the detail of a CodeReplaceRefused error.
type ReplaceRefusal struct {
	Match        string               `json:"match"`
	Device       appsvc.DeviceAccount `json:"device"`
	Incoming     string               `json:"incoming"`
	IncomingName string               `json:"incomingName,omitempty"`
	// Confirm is what the screen sends back as its yes to exactly this
	// replacement: the incoming account over the keystore shown.
	Confirm string `json:"confirm"`
}

// codeReplace turns the app's refusal into the coded error the UI confirms on.
func codeReplace(err error) error {
	var r *appsvc.ReplaceRefusedError
	if errors.As(err, &r) {
		return &CodedError{
			Code: CodeReplaceRefused,
			Msg:  "this would erase the account on this device; confirm to continue",
			Detail: ReplaceRefusal{
				Match: r.Match, Device: r.Device, Incoming: r.Incoming, IncomingName: r.IncomingName,
				Confirm: r.Confirm,
			},
		}
	}
	return err
}

// ChangePassphrase re-seals this device's keystore under a new passphrase.
// Needs an unlocked session AND the current passphrase: an unlocked window
// left on a desk must not be enough to lock its owner out.
func (b *Bridge) ChangePassphrase(current, next string) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	if strings.TrimSpace(next) == "" {
		return errors.New("choose a new passphrase")
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := appsvc.ChangePassphrase(dir, svc.PublicKey(), current, next); err != nil {
		if errors.Is(err, identity.ErrWrongPassphrase) {
			return wrongPassphrase()
		}
		if errors.Is(err, identity.ErrNotThisAccount) {
			return errors.New("the account stored on this device is no longer the one you're signed in as — another Concord window restored or linked a different one. Nothing was changed; sign out and check")
		}
		return err
	}
	return nil
}

func wrongPassphrase() error {
	return &CodedError{Code: CodeWrongPassphrase, Msg: "that passphrase didn't work"}
}

// RevealMnemonicWith returns the recovery phrase after re-checking the
// passphrase. The phrase IS the account — anyone who reads it can become you —
// so an unlocked window alone is not enough to show it.
func (b *Bridge) RevealMnemonicWith(passphrase string) (string, error) {
	svc, err := b.service()
	if err != nil {
		return "", err
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return "", err
	}
	if !appsvc.VerifyPassphrase(dir, passphrase) {
		return "", wrongPassphrase()
	}
	return svc.Mnemonic()
}

// StartOverWord is what the user types to erase this device from the lock
// screen (start over, or erasing data found without its keystore).
const StartOverWord = "ERASE"
