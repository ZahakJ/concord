package bridge

import (
	"bytes"
	"errors"
	"log"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
)

// Fingerprint unlock and the App Lock gate (DESIGN-BIOUNLOCK, revision 2).
//
// The secret U that opens the keystore's device slot lives on the phone,
// wrapped by a biometric-bound Android Keystore key. It reaches the core only
// through the in-process calls below (mobile.Node), never through Dispatch:
// none of these entry points is an RPC method, so the page, and anything that
// runs script in it, can neither read U nor open the gate.

// enrolWindow is how long after a passphrase unlock turning on fingerprint
// unlock may take that unlock as its proof of the passphrase.
const enrolWindow = 60 * time.Second

var (
	// ErrAppLocked is every RPC but SessionState while the App Lock gate is
	// closed.
	ErrAppLocked  = errors.New("bridge: Concord is locked; unlock with your fingerprint")
	errEnrolProof = errors.New("bridge: enter your passphrase to turn on fingerprint unlock")
)

// gateAllows reports whether method may run while the gate is closed:
// asking whether it is, signing out (which ends the session the gate
// guards), hanging up, and a notification's Mark as read (which runs but
// answers nothing). A call's signalling passes only while this node is in a
// call: a call in progress keeps going behind the gate, and nothing else
// may use those methods to reach a peer.
// None of these reads anything the gate protects.
func (b *Bridge) gateAllows(method string) bool {
	switch method {
	case "SessionState", "Logout", "LeaveVoice", "MarkRead":
		return true
	case "SignalCall", "RelaySignal", "CallIceServers":
		return b.inCall()
	}
	return false
}

// GateEventAllowed reports whether an event may reach the page while the
// gate is closed: the gate's own, and a call's signalling while this node is
// in a call.
func (b *Bridge) GateEventAllowed(name string) bool {
	switch name {
	case "applock":
		return true
	case "voice-signal", "voice-presence":
		return b.inCall()
	}
	return false
}

// inCall reports whether a page of this node holds a call (voicelife's
// owner table).
func (b *Bridge) inCall() bool {
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.rooms) > 0
}

// Gated reports whether the App Lock gate is closed.
func (b *Bridge) Gated() bool { return b.gated.Load() }

func (b *Bridge) setGate(on bool) {
	if on {
		// The passphrase-unlock window does not outlive the gate closing.
		b.enrolUntil = time.Time{}
	}
	if b.gated.Swap(on) != on && b.OnAppLock != nil {
		b.OnAppLock(on)
	}
}

// EngageAppLock closes the gate, if a session is open, fingerprint unlock is
// on and the user has not turned App Lock off. The phone calls it when the
// app goes to the background (after its grace period).
//
// It holds b.mu throughout, so it cannot close the gate on a session that a
// concurrent Logout has just ended: the gate is never closed with no session.
func (b *Bridge) EngageAppLock() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.appLockAppliesLocked() {
		return false
	}
	b.setGate(true)
	return true
}

// appLockAppliesLocked reports whether the gate may close now: a session is
// open, fingerprint unlock is on and App Lock has not been turned off. For a
// caller holding b.mu.
func (b *Bridge) appLockAppliesLocked() bool {
	if b.svc == nil || b.svc.AppLockOff() {
		return false
	}
	dir, err := appsvc.DataDir()
	return err == nil && appsvc.DeviceSlotID(dir) != nil
}

// gateIfBackgroundedLocked closes the gate on a session that opened, or was
// let through the gate, while the app is off screen (an unlock finishing as
// the user switched away): onStop has already been and will not come again.
// Otherwise it opens it. For a caller holding b.mu.
func (b *Bridge) gateIfBackgroundedLocked() {
	b.setGate(b.shellBackground && b.appLockAppliesLocked())
}

// UseDeviceSecret is the fingerprint's way in. With no session it unlocks
// (opening the keystore through the slot); with one, it checks U against
// the slot and opens the App Lock gate. It never creates an account.
func (b *Bridge) UseDeviceSecret(slotID, u []byte) error {
	defer zero(u)
	b.mu.Lock()
	defer b.mu.Unlock()
	// Either way this is not a passphrase unlock.
	b.enrolUntil = time.Time{}
	dataDir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	if b.svc != nil {
		if err := b.svc.VerifyDeviceSecret(slotID, u); err != nil {
			return err
		}
		b.gateIfBackgroundedLocked()
		return nil
	}
	if b.linking {
		return errLinkInProgress
	}
	if appsvc.DeviceSlotID(dataDir) == nil {
		return identity.ErrNoDeviceSlot
	}
	// startLocked closes the gate if the app went to the background meanwhile.
	return b.startLocked(appsvc.Config{DataDir: dataDir, DeviceSlotID: slotID, DeviceSecret: u})
}

// EnrolDeviceSecret turns fingerprint unlock on: it adds a slot sealed under
// U. Its proof of the passphrase is either the passphrase itself, or (with
// passphrase "") an unlock by passphrase within the last enrolWindow, which
// it uses up.
func (b *Bridge) EnrolDeviceSecret(slotID, u []byte, passphrase string) error {
	defer zero(u)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc == nil {
		return errors.New("bridge: unlock before turning on fingerprint unlock")
	}
	if b.gated.Load() {
		return ErrAppLocked
	}
	dataDir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	if passphrase != "" {
		if !appsvc.VerifyPassphrase(dataDir, passphrase) {
			return wrongPassphrase()
		}
	} else if b.enrolUntil.IsZero() || time.Now().After(b.enrolUntil) {
		return errEnrolProof
	}
	b.enrolUntil = time.Time{}
	if err := b.svc.AddDeviceSlot(slotID, u); err != nil {
		return err
	}
	b.wrapperID, b.wrapperKnown = append([]byte(nil), slotID...), true
	return nil
}

// DropDeviceSlot turns fingerprint unlock off (slotID nil: any slot).
func (b *Bridge) DropDeviceSlot(slotID []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc == nil {
		return errors.New("bridge: unlock before turning off fingerprint unlock")
	}
	if b.gated.Load() {
		return ErrAppLocked
	}
	if err := b.svc.RemoveDeviceSlot(slotID); err != nil {
		return err
	}
	if slotID == nil || bytes.Equal(b.wrapperID, slotID) {
		b.wrapperID = nil
	}
	return nil
}

// SetDeviceWrapper tells the core which enrolment the phone holds a wrapped
// U for (nil: none). A slot without its wrapper can never be opened, and is
// dropped at the next passphrase unlock (the keystore is not written while
// the phone is locked).
func (b *Bridge) SetDeviceWrapper(slotID []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wrapperID, b.wrapperKnown = append([]byte(nil), slotID...), true
	if len(slotID) == 0 {
		b.wrapperID = nil
	}
	if b.svc != nil {
		b.dropOrphanSlotLocked()
	}
}

// dropOrphanSlotLocked removes a slot the phone holds no wrapper for, for a
// caller holding b.mu with a session open.
func (b *Bridge) dropOrphanSlotLocked() {
	if !b.wrapperKnown || b.svc == nil {
		return
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return
	}
	slot := appsvc.DeviceSlotID(dir)
	if slot == nil || bytes.Equal(slot, b.wrapperID) {
		return
	}
	if err := b.svc.RemoveDeviceSlot(slot); err != nil {
		log.Printf("concord: could not remove a fingerprint slot this phone cannot open: %v", err)
	}
}

// AppLockView is App Lock's state for Settings.
type AppLockView struct {
	Enrolled bool `json:"enrolled"`
	On       bool `json:"on"`
}

// AppLock reports whether fingerprint unlock is on and whether App Lock is.
func (b *Bridge) AppLock() (AppLockView, error) {
	svc, err := b.service()
	if err != nil {
		return AppLockView{}, err
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return AppLockView{}, err
	}
	enrolled := appsvc.DeviceSlotID(dir) != nil
	return AppLockView{Enrolled: enrolled, On: enrolled && !svc.AppLockOff()}, nil
}

// SetAppLock turns App Lock on or off. It runs only with the gate open.
func (b *Bridge) SetAppLock(on bool) error {
	svc, err := b.service()
	if err != nil {
		return err
	}
	return svc.SetAppLockOff(!on)
}

// zero overwrites a secret the caller handed in, once the core is done with
// it. Everything derived from it is zeroed where it is derived (identity).
func zero(p []byte) {
	for i := range p {
		p[i] = 0
	}
}

// NotifyContext is what the phone's notifier needs to post a DM notification:
// our own fingerprint (so our own words are not announced to us) and the DM
// channels with their names. It is not an RPC method: the gate keeps the page
// from Identity and Guilds, while the native notifier, in process, must keep
// posting behind it. Nothing more than those names leaves the core here.
type NotifyContext struct {
	Self string            `json:"self"`
	DMs  map[string]string `json:"dms"`
}

// NotifyContext reports the notifier's context, or an error with no session.
func (b *Bridge) NotifyContext() (NotifyContext, error) {
	id, err := b.Identity()
	if err != nil {
		return NotifyContext{}, err
	}
	guilds, err := b.Guilds()
	if err != nil {
		return NotifyContext{}, err
	}
	out := NotifyContext{Self: id.Fingerprint, DMs: map[string]string{}}
	for _, g := range guilds {
		if g.Kind != "dm" {
			continue
		}
		for _, ch := range g.Channels {
			out.DMs[ch.ID] = g.Name
		}
	}
	return out, nil
}

// AppLockActive reports whether App Lock applies now (a session is open,
// fingerprint unlock is on, and App Lock has not been turned off), and
// whether that is known at all: with no session it is not.
func (b *Bridge) AppLockActive() (on, known bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.svc == nil {
		return false, false
	}
	return b.appLockAppliesLocked(), true
}
