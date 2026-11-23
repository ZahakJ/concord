package bridge

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"weak"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

// Erasing this device.
//
// ResetIdentity already existed and already did the right thing, but it was
// only reachable from the login screen, behind "Forgot passphrase?" — which is
// to say: the only way to delete your data was to be unable to open it. Someone
// who wants to leave has no reason to think of that as the exit, and a store
// listing has to be able to point at a route that a logged-in person can find.
//
// The bare ResetIdentity RPC still refuses while unlocked, unchanged. This is a
// separate, two-call path, and the two calls exist because of what a single one
// would be: a method on a loopback HTTP API that irreversibly destroys an
// account, callable in one line by anything that can reach the port. So:
//
//  1. BeginWipe mints a single-use nonce with a short life and returns, with
//     it, the exact phrase the user must type and a count of what is about to
//     go. Nothing is destroyed.
//  2. ConfirmWipe takes that nonce back plus the typed phrase. Both must match;
//     the nonce is spent on the attempt, right or wrong, so a wrong guess costs
//     a fresh trip through the dialog rather than becoming a retry loop.
//
// The phrase is the account's own display name. A fixed word ("delete") is
// something a script can hardcode; a name is something the person doing it has
// to actually know and read off their own screen. When the profile has no name
// to offer, it falls back to "delete" and says so.
//
// What this does NOT do, and the dialog says so: it does not remove anything
// from anybody else. Messages already delivered are on their devices, sealed to
// groups this device is about to stop being part of, and no amount of local
// deletion reaches them. That is what a peer-to-peer system means, and pretending
// otherwise in a deletion flow would be the dishonest part.

// wipeTicketTTL bounds how long a confirmation stays valid. Long enough to read
// a dialog and type a name, short enough that a ticket cannot sit around.
const wipeTicketTTL = 5 * time.Minute

// wipeConfirmFallback is the phrase used when the profile carries no name.
const wipeConfirmFallback = "delete"

var (
	wipeMu     sync.Mutex
	wipeTicket string    // "" when none is outstanding
	wipePhrase string    // what must be typed for this ticket
	wipeIssued time.Time // when it was minted
	// What the confirmation was opened for: the session that showed the
	// dialog and the keystore on the device then. ConfirmWipe erases only for
	// that session, still running, over that same keystore; a ticket armed
	// without them (zero values) confirms nothing. The session is held
	// weakly: a dialog left open does not keep a signed-out session, and the
	// identity it holds in memory, alive; once it is gone the ticket matches
	// no session.
	wipeSvc      weak.Pointer[appsvc.Service]
	wipeKeystore []byte
)

// WipeView is what the confirmation dialog is built from. Every number in it is
// read off this device, so the dialog can say what is actually being destroyed
// instead of a generic warning.
type WipeView struct {
	// Ticket is handed straight back to ConfirmWipe. It is not a secret worth
	// protecting — it is a proof that this call came after a dialog.
	Ticket string `json:"ticket"`
	// Phrase is what the user must type, shown verbatim in the dialog.
	Phrase string `json:"phrase"`
	// Guilds and Devices are what goes and what does not: guilds leave this
	// device, linked devices keep their own copies and simply lose this one.
	// Devices counts OTHER devices — LinkedDevices always includes the one you
	// are holding, and telling a single-device user they have one device to fall
	// back on is the exact wrong thing to say in this dialog.
	Guilds  int `json:"guilds"`
	Devices int `json:"devices"`
}

// otherDevices counts the linked devices that are not this one and have not
// been revoked. A revoked device is not a way back — it has already been told
// to erase itself.
func otherDevices(all []appsvc.LinkedDeviceView) int {
	n := 0
	for _, d := range all {
		if !d.ThisOne && d.Revoked == 0 {
			n++
		}
	}
	return n
}

// BeginWipe opens a device-erase confirmation. It destroys nothing.
func (b *Bridge) BeginWipe() (WipeView, error) {
	svc, err := b.service()
	if err != nil {
		return WipeView{}, err
	}
	ksPath, err := appsvc.KeystorePath()
	if err != nil {
		return WipeView{}, err
	}
	keystore, err := identity.ReadKeystore(ksPath)
	if err != nil {
		return WipeView{}, err
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return WipeView{}, err
	}
	phrase := strings.TrimSpace(svc.SelfProfile().Name)
	// A name nobody can type — empty, invisible, or carrying format
	// characters — made the dialog unconfirmable: "Type  to confirm".
	if !appsvc.Typeable(phrase) {
		phrase = wipeConfirmFallback
	}
	ticket := hex.EncodeToString(raw)

	wipeMu.Lock()
	wipeTicket, wipePhrase, wipeIssued = ticket, phrase, time.Now()
	wipeSvc, wipeKeystore = weak.Make(svc), keystore
	wipeMu.Unlock()

	return WipeView{
		Ticket:  ticket,
		Phrase:  phrase,
		Guilds:  len(svc.Guilds()),
		Devices: otherDevices(svc.LinkedDevices()),
	}, nil
}

// takeWipeTicket spends the outstanding ticket and reports the phrase it was
// minted with. It is spent whether or not it matches, so a mismatched
// confirmation cannot be retried against the same ticket.
func takeWipeTicket(ticket string) (phrase string, ok bool) {
	phrase, _, _, ok = takeWipe(ticket)
	return phrase, ok
}

// takeWipe is takeWipeTicket with what the ticket was opened for.
func takeWipe(ticket string) (phrase string, svc *appsvc.Service, keystore []byte, ok bool) {
	wipeMu.Lock()
	defer wipeMu.Unlock()
	if wipeTicket == "" || ticket == "" || ticket != wipeTicket {
		return "", nil, nil, false
	}
	phrase, svc, keystore, ok = wipePhrase, wipeSvc.Value(), wipeKeystore, time.Since(wipeIssued) <= wipeTicketTTL
	wipeTicket, wipePhrase, wipeSvc, wipeKeystore = "", "", weak.Pointer[appsvc.Service]{}, nil
	return phrase, svc, keystore, ok
}

// ConfirmWipe locks the app and erases the identity, the encrypted database and
// the MLS group state from this device. Irreversible without the 24-word
// recovery phrase, and even that recovers the account, not this device's copy of
// what was in it.
func (b *Bridge) ConfirmWipe(ticket, typed string) error {
	phrase, svc, keystore, ok := takeWipe(ticket)
	if !ok {
		return errors.New("that confirmation expired — close this and start again")
	}
	if !strings.EqualFold(strings.TrimSpace(typed), phrase) {
		return errors.New("that didn't match — type it exactly as shown")
	}
	// The yes was for one session and the account it had open. Signed out
	// since, or unlocked again (as this account or another): it is for
	// nothing, and nothing is erased.
	stale := &CodedError{Code: CodeConfirmStale, Msg: "This confirmation was opened in a session that has ended. Nothing was deleted. Open it again to erase this device."}

	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	ksPath, err := appsvc.KeystorePath()
	if err != nil {
		return err
	}

	// The erase holds b.mu throughout: an unlock from another window in
	// between would otherwise open the keystore that is being deleted, or
	// find it gone and create a fresh account under whatever it was typed.
	b.mu.Lock()
	if svc == nil || b.svc != svc {
		b.mu.Unlock()
		return stale
	}
	// Only over the keystore that session opened, checked under the keystore
	// lock with the erase: one another process has since written (a restore,
	// a link) or this session re-sealed (a new passphrase) was never shown to
	// this dialog.
	//
	// The session is closed in that same hold, once the check has passed and
	// before any file goes. The store is open, the node is connected and the
	// reconcile beat is still running; deleting the database out from under
	// all of that is how a wipe turns into a crash report instead of a login
	// screen. A wipe refused at the check, or one that cannot take the lock,
	// deletes nothing and leaves the session as it was.
	closed := false
	err = appsvc.ResetIdentityIf(dir, func() bool {
		now, rerr := identity.ReadKeystore(ksPath)
		return rerr == nil && bytes.Equal(now, keystore)
	}, func() {
		_ = b.svc.Close()
		b.svc = nil
		b.setGate(false)
		closed = true
	})
	if !closed && errors.Is(err, identity.ErrKeystoreChanged) {
		if pub, rerr := identity.ReadAccountPub(ksPath); rerr != nil || !bytes.Equal(pub, svc.PublicKey()) {
			// Not this session's account any more (another window restored or
			// linked another, or erased it): the session is over.
			_ = b.svc.Close()
			b.svc = nil
			b.setGate(false)
			b.lockReason = LockSignedOut
			b.mu.Unlock()
			if b.OnPresence != nil {
				b.OnPresence()
			}
			return &CodedError{Code: CodeConfirmStale, Msg: "Another Concord window or process put another account on this device, or removed this one, since this confirmation opened. Nothing was deleted, and you have been signed out. Unlock and look again before erasing."}
		}
	}
	if !closed {
		b.mu.Unlock()
		switch {
		case errors.Is(err, identity.ErrKeystoreChanged):
			return &CodedError{Code: CodeConfirmStale, Msg: "The keystore on this device changed since this confirmation opened: its passphrase was changed, or another Concord window or process wrote it. Nothing was deleted, and you are still signed in. Open it again to erase this device."}
		case errors.Is(err, identity.ErrKeystoreBusy):
			return &CodedError{Code: CodeKeystoreBusy, Msg: "Another Concord window or process is using the keystore. Nothing was deleted, and you are still signed in. Try again."}
		}
		return err
	}
	if err != nil && !errors.Is(err, identity.ErrEraseDeferred) && !errors.Is(err, appsvc.ErrDataNotErased) {
		// The session is closed and the keystore is still here: not wiped.
		b.lockReason = LockSignedOut
		b.mu.Unlock()
		if b.OnPresence != nil {
			b.OnPresence()
		}
		return err
	}
	b.lockReason = LockWiped
	b.mu.Unlock()
	// The UI is now talking to a locked bridge over a session that no longer
	// exists; poke the presence feed so the shell goes back to the lock screen,
	// the same way an unlink does (see the OnUnlinked hook in bridge.go).
	if b.OnPresence != nil {
		b.OnPresence()
	}
	return erasePendingError(err)
}

// erasePendingError reports a wipe whose keystore removal had to be deferred
// (identity.ErrEraseDeferred) as what it is: done, with one file still to go.
// It is not a failure, and must not read as one.
func erasePendingError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, appsvc.ErrDataNotErased) {
		// Not done, and said exactly: what is still here is named, and it
		// stays until the user erases again. Nothing removes it on its own,
		// and nothing new is set up beside it in the meantime.
		left := "some of its data"
		if _, list, ok := strings.Cut(err.Error(), "still here: "); ok {
			left = list
		}
		return errors.New("The erase did not finish. Still on this device: " + left + ". Nothing new will be set up beside it. Close anything else using Concord's files, then erase again (Forgot passphrase → start over).")
	}
	return &CodedError{
		Code: CodeErasePending,
		Msg:  "Everything on this device was erased. One file is still held by another program; it can no longer be opened, and Concord deletes it as soon as it can — at the latest the next time this device is unlocked or set up.",
	}
}
