package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
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
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return WipeView{}, err
	}
	phrase := strings.TrimSpace(svc.SelfProfile().Name)
	if phrase == "" {
		phrase = wipeConfirmFallback
	}
	ticket := hex.EncodeToString(raw)

	wipeMu.Lock()
	wipeTicket, wipePhrase, wipeIssued = ticket, phrase, time.Now()
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
	wipeMu.Lock()
	defer wipeMu.Unlock()
	if wipeTicket == "" || ticket == "" || ticket != wipeTicket {
		return "", false
	}
	phrase, ok = wipePhrase, time.Since(wipeIssued) <= wipeTicketTTL
	wipeTicket, wipePhrase = "", ""
	return phrase, ok
}

// ConfirmWipe locks the app and erases the identity, the encrypted database and
// the MLS group state from this device. Irreversible without the 24-word
// recovery phrase, and even that recovers the account, not this device's copy of
// what was in it.
func (b *Bridge) ConfirmWipe(ticket, typed string) error {
	phrase, ok := takeWipeTicket(ticket)
	if !ok {
		return errors.New("that confirmation expired — close this and start again")
	}
	if !strings.EqualFold(strings.TrimSpace(typed), phrase) {
		return errors.New("that didn't match — type it exactly as shown")
	}

	dir, err := appsvc.DataDir()
	if err != nil {
		return err
	}

	// Close the session BEFORE touching the files. The store is open, the node
	// is connected and the reconcile beat is still running; deleting the
	// database out from under all of that is how a wipe turns into a crash
	// report instead of a login screen.
	b.mu.Lock()
	if b.svc != nil {
		_ = b.svc.Close()
		b.svc = nil
	}
	b.mu.Unlock()

	if err := appsvc.ResetIdentity(dir); err != nil {
		return err
	}
	// The UI is now talking to a locked bridge over a session that no longer
	// exists; poke the presence feed so the shell goes back to the lock screen,
	// the same way an unlink does (see the OnUnlinked hook in bridge.go).
	if b.OnPresence != nil {
		b.OnPresence()
	}
	return nil
}
