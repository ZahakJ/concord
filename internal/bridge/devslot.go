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

// gateAllows reports whether method may run while the gate is closed.
func gateAllows(method string) bool { return method == "SessionState" }

// Gated reports whether the App Lock gate is closed.
func (b *Bridge) Gated() bool { return b.gated.Load() }

func (b *Bridge) setGate(on bool) {
	if b.gated.Swap(on) != on && b.OnAppLock != nil {
		b.OnAppLock(on)
	}
}

// EngageAppLock closes the gate, if a session is open, fingerprint unlock is
// on and the user has not turned App Lock off. The phone calls it when the
// app goes to the background (after its grace period).
func (b *Bridge) EngageAppLock() bool {
	b.mu.Lock()
	svc := b.svc
	b.mu.Unlock()
	if svc == nil || svc.AppLockOff() {
		return false
	}
	dir, err := appsvc.DataDir()
	if err != nil || appsvc.DeviceSlotID(dir) == nil {
		return false
	}
	b.setGate(true)
	return true
}

// UseDeviceSecret is the fingerprint's way in. With no session it unlocks
// (opening the keystore through the slot); with one, it checks U against
// the slot and opens the App Lock gate. It never creates an account.
func (b *Bridge) UseDeviceSecret(slotID, u []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	dataDir, err := appsvc.DataDir()
	if err != nil {
		return err
	}
	if b.svc != nil {
		if err := b.svc.VerifyDeviceSecret(slotID, u); err != nil {
			return err
		}
		b.setGate(false)
		return nil
	}
	if b.linking {
		return errLinkInProgress
	}
	if appsvc.DeviceSlotID(dataDir) == nil {
		return identity.ErrNoDeviceSlot
	}
	if err := b.startLocked(appsvc.Config{DataDir: dataDir, DeviceSlotID: slotID, DeviceSecret: u}); err != nil {
		return err
	}
	b.setGate(false)
	return nil
}

// EnrolDeviceSecret turns fingerprint unlock on: it adds a slot sealed under
// U. Its proof of the passphrase is either the passphrase itself, or (with
// passphrase "") an unlock by passphrase within the last enrolWindow, which
// it uses up.
func (b *Bridge) EnrolDeviceSecret(slotID, u []byte, passphrase string) error {
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
