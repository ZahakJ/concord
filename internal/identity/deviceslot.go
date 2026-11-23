package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/secretbox"
)

// A device slot is a second way into the keystore, for a phone's fingerprint
// unlock (docs: DESIGN-BIOUNLOCK, revision 2). It seals the same two seeds
// the passphrase does, under a key derived from U: 32 random bytes the phone
// keeps only encrypted by an Android Keystore key that a strong biometric
// must release. The passphrase slot is never touched by any of this.
//
// The slot is immutable. It is written once when fingerprint unlock is turned
// on, copied byte for byte by every later write of the keystore for as long
// as the account and device keys are the ones it names, and dropped by any
// write where they are not (a restore, a re-link). Nothing holds the slot key
// after the open or the enrolment that derived it.

const (
	// DeviceSecretSize is the size of U.
	DeviceSecretSize = 32
	// DeviceSlotIDSize is the size of the enrolment id. It names the
	// enrolment (and the phone's Keystore alias), so a wrapped U from one
	// enrolment is never tried against another's slot.
	DeviceSlotIDSize = 16

	slotRecordVersion = 1
	slotInfo          = "concord/keystore/device-slot/v1"
)

var (
	// ErrNoDeviceSlot: the keystore has no device slot (or none with the
	// given id).
	ErrNoDeviceSlot = errors.New("identity: this keystore has no fingerprint unlock")
	// ErrDeviceSlotMismatch: U did not open the slot, or opened it to keys
	// other than the ones it names. It is deliberately not ErrWrongPassphrase:
	// it must never send anyone to the recovery phrase, since the passphrase
	// slot is untouched.
	ErrDeviceSlotMismatch = errors.New("identity: fingerprint unlock does not open this keystore")
)

type deviceSlot struct {
	ID         []byte `json:"id"`
	AccountPub []byte `json:"account_pub"`
	DevicePub  []byte `json:"device_pub"`
	Nonce      []byte `json:"nonce"`
	Box        []byte `json:"box"`
}

// opensTo reports whether the slot names id's account and device keys.
func (s *deviceSlot) opensTo(id *Identity) bool {
	if s == nil || id == nil || id.DeviceSeed() == nil {
		return false
	}
	return bytes.Equal(s.AccountPub, id.PublicKey()) && bytes.Equal(s.DevicePub, id.DevicePublicKey())
}

func (s *deviceSlot) wellFormed() bool {
	return s != nil && len(s.ID) == DeviceSlotIDSize &&
		len(s.AccountPub) == ed25519.PublicKeySize && len(s.DevicePub) == ed25519.PublicKeySize &&
		len(s.Nonce) == nonceSize && len(s.Box) > secretbox.Overhead
}

// slotOf is the device slot in a keystore snapshot, or nil.
func slotOf(blob []byte) *deviceSlot {
	if len(blob) == 0 {
		return nil
	}
	var env keystoreFile
	if json.Unmarshal(blob, &env) != nil || !env.DeviceSlot.wellFormed() {
		return nil
	}
	return env.DeviceSlot
}

// slotKey is K_dev = HKDF-SHA256(U, salt=id, info=slotInfo||account_pub||device_pub).
func slotKey(u, slotID, accountPub, devicePub []byte) [keySize]byte {
	info := make([]byte, 0, len(slotInfo)+len(accountPub)+len(devicePub))
	info = append(append(append(info, slotInfo...), accountPub...), devicePub...)
	var k [keySize]byte
	_, _ = io.ReadFull(hkdf.New(sha256.New, u, slotID, info), k[:])
	return k
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// slotRecord frames the two seeds: version, account seed, device seed.
func slotRecord(id *Identity) []byte {
	r := make([]byte, 0, 1+2*SeedSize)
	r = append(r, slotRecordVersion)
	r = append(r, id.Seed()...)
	return append(r, id.DeviceSeed()...)
}

// DeviceSlotID is the id of the keystore's device slot, or nil. It reads no
// secret: a locked phone uses it to tell whether its wrapped U and the slot
// belong together.
func DeviceSlotID(path string) []byte {
	blob, err := ReadKeystore(path)
	if err != nil {
		return nil
	}
	if s := slotOf(blob); s != nil {
		return append([]byte(nil), s.ID...)
	}
	return nil
}

// AddDeviceSlot seals id's seeds under U into the keystore at path, as slot
// slotID, replacing any slot there. The caller must have proven the
// passphrase (this writes without it, since the passphrase slot is copied
// as it is) and must hold id unlocked from this same file. U and slotID come
// from the phone.
func AddDeviceSlot(path string, id *Identity, slotID, u []byte) error {
	if len(u) != DeviceSecretSize || len(slotID) != DeviceSlotIDSize {
		return errors.New("identity: fingerprint unlock secret has the wrong size")
	}
	if id == nil || id.DeviceSeed() == nil {
		return errors.New("identity: this device has no device key yet")
	}
	unlock, err := lockKeystore(path)
	if err != nil {
		return err
	}
	defer unlock()
	blob, err := ReadKeystore(path)
	if err != nil {
		return err
	}
	var env keystoreFile
	if err := json.Unmarshal(blob, &env); err != nil {
		return ErrDamagedKeystore
	}
	if env.Version < 1 || env.Version > maxKeystoreVersion {
		return fmt.Errorf("identity: unsupported keystore version %d", env.Version)
	}
	if !bytes.Equal(env.AccountPub, id.PublicKey()) {
		return ErrNotThisAccount
	}
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("identity: read nonce: %w", err)
	}
	acct, dev := id.PublicKey(), id.DevicePublicKey()
	k := slotKey(u, slotID, acct, dev)
	rec := slotRecord(id)
	box := secretbox.Seal(nil, rec, &nonce, &k)
	zero(rec)
	zero(k[:])
	env.DeviceSlot = &deviceSlot{
		ID:         append([]byte(nil), slotID...),
		AccountPub: append([]byte(nil), acct...),
		DevicePub:  append([]byte(nil), dev...),
		Nonce:      nonce[:],
		Box:        box,
	}
	env.Version = keystoreVersionSlot
	return writeEnvelopeLocked(path, env, id.PublicKey())
}

// OpenWithDeviceSlot opens the keystore at path with U, for the slot named
// slotID. A U or slot that does not fit is ErrDeviceSlotMismatch; a
// keystore without that slot is ErrNoDeviceSlot.
func OpenWithDeviceSlot(path string, slotID, u []byte) (*Identity, error) {
	blob, err := ReadKeystore(path)
	if err != nil {
		return nil, err
	}
	var env keystoreFile
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, ErrDamagedKeystore
	}
	if env.Version < 1 || env.Version > maxKeystoreVersion {
		return nil, fmt.Errorf("identity: unsupported keystore version %d", env.Version)
	}
	s := env.DeviceSlot
	if !s.wellFormed() || !bytes.Equal(s.ID, slotID) {
		return nil, ErrNoDeviceSlot
	}
	if len(u) != DeviceSecretSize {
		return nil, ErrDeviceSlotMismatch
	}
	var nonce [nonceSize]byte
	copy(nonce[:], s.Nonce)
	k := slotKey(u, s.ID, s.AccountPub, s.DevicePub)
	rec, ok := secretbox.Open(nil, s.Box, &nonce, &k)
	zero(k[:])
	if !ok || len(rec) != 1+2*SeedSize || rec[0] != slotRecordVersion {
		zero(rec)
		return nil, ErrDeviceSlotMismatch
	}
	id, err := FromSeeds(rec[1:1+SeedSize], rec[1+SeedSize:])
	zero(rec)
	if err != nil {
		return nil, ErrDeviceSlotMismatch
	}
	// The keys the seeds make must be the ones the slot names, and the
	// account the file says it holds.
	if !s.opensTo(id) || (len(env.AccountPub) > 0 && !bytes.Equal(env.AccountPub, id.PublicKey())) {
		return nil, ErrDeviceSlotMismatch
	}
	noteVerified(path, blob, id.PublicKey())
	return id, nil
}

// RemoveDeviceSlot drops the keystore's device slot, if it is slotID (or any
// slot, when slotID is nil), and writes the file as v2 again so an earlier
// version of Concord opens it. No slot: nothing is written. id is the
// unlocked identity the file must hold: the keystore is never written while
// the phone is locked.
func RemoveDeviceSlot(path string, id *Identity, slotID []byte) error {
	if id == nil {
		return errors.New("identity: unlock before turning fingerprint unlock off")
	}
	unlock, err := lockKeystore(path)
	if err != nil {
		return err
	}
	defer unlock()
	blob, err := ReadKeystore(path)
	if err != nil {
		return err
	}
	var env keystoreFile
	if err := json.Unmarshal(blob, &env); err != nil {
		return ErrDamagedKeystore
	}
	if env.DeviceSlot == nil || (slotID != nil && !bytes.Equal(env.DeviceSlot.ID, slotID)) {
		return nil
	}
	if env.Version > maxKeystoreVersion {
		return fmt.Errorf("identity: unsupported keystore version %d", env.Version)
	}
	if !bytes.Equal(env.AccountPub, id.PublicKey()) {
		return ErrNotThisAccount
	}
	env.DeviceSlot = nil
	env.Version = keystoreVersion
	return writeEnvelopeLocked(path, env, id.PublicKey())
}
