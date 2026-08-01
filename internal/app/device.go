package app

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/identity"
)

// Device-linking mode (Phase 4). By default a Concord install is the account's
// original device: its libp2p PeerID and MLS leaf derive from the account key,
// exactly as every pre-multi-device client does. A device that was LINKED into
// an existing account (a phone paired to a desktop) instead uses its own
// per-device key so the two don't collide at the network layer, and presents a
// device certificate (account key signing the device key) as its MLS credential
// so peers still map the leaf back to the shared account.
//
// The distinction is recorded in a small on-disk marker written by the linking
// flow. Its presence is what flips Start into linked-device mode; absence keeps
// the single-device behavior byte-for-byte unchanged.

// deviceMarkerName is the marker file under the data dir.
const deviceMarkerName = "device.json"

// deviceMarker records that this install is a linked device and carries the
// account-signed certificate for its device key.
type deviceMarker struct {
	Cert *identity.DeviceCert `json:"cert"`
}

func deviceMarkerPath(dataDir string) string {
	return filepath.Join(dataDir, deviceMarkerName)
}

// loadDeviceMarker returns the linked-device marker if this install has one and
// it verifies against the given account key. A missing/invalid/foreign marker
// yields (nil,false) → default single-device mode.
func loadDeviceMarker(dataDir string, accountPub ed25519.PublicKey) (*deviceMarker, bool) {
	b, err := os.ReadFile(deviceMarkerPath(dataDir))
	if err != nil {
		return nil, false
	}
	var m deviceMarker
	if json.Unmarshal(b, &m) != nil || m.Cert == nil {
		return nil, false
	}
	// The cert must verify AND certify THIS install's account — a marker copied
	// from another account is ignored rather than trusted.
	if !m.Cert.Verify() || !ed25519Equal(m.Cert.AccountPub, accountPub) {
		return nil, false
	}
	return &m, true
}

// saveDeviceMarker persists a linked-device marker (written by the link flow
// once the account key has signed this device's cert).
func saveDeviceMarker(dataDir string, cert *identity.DeviceCert) error {
	b, err := json.MarshalIndent(deviceMarker{Cert: cert}, "", "  ")
	if err != nil {
		return err
	}
	tmp := deviceMarkerPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, deviceMarkerPath(dataDir))
}

// ownDevicesKey is the SUPERSEDED store key: a bare list of the certificates
// this account had issued. It is read once, to migrate into the richer registry
// in devices.go (which also holds certs we merely LEARNED, plus names and
// last-seen), and never written again.
//
// Why keeping certs mattered at all: we sign one when we link a phone and then
// used to forget it, leaving the leaf the phone plants in each shared group as
// the only record. A device that linked but joined nothing (every guild handover
// failed), or whose leaf was later removed, was unplaceable FOREVER — its PeerID
// resolved to its own device key, so your own phone read as a stranger and no
// amount of relearning could fix it, because there was nothing left to relearn
// from. Why keeping only the ISSUED ones was not enough: the phone that is
// actually in the wild was linked by a build that kept nothing.
const ownDevicesKey = "account.devices"

// rememberOwnDevice records a cert we just issued for one of our devices.
// Idempotent: re-linking a device we already certified (RedeemLink reuses the
// device seed) replaces nothing and adds nothing.
func (s *Service) rememberOwnDevice(cert *identity.DeviceCert) {
	if cert == nil {
		return
	}
	s.learnDeviceCert(cert.Marshal())
	s.noteOwnDevice(cert, cert.DeviceName, "", false)
}

// mailboxKeyOf returns the key a member's mailbox tag is derived from — the key
// their libp2p PeerID is built on, since the rendezvous node computes the tag
// from the connected peer's PeerID. For a device-cert credential that's the
// device pubkey (the linked device's PeerID); for a legacy bare credential it's
// the account key (which is also that device's PeerID). This keeps sender-side
// deposit tags and node-side registration tags in agreement across both modes.
func mailboxKeyOf(cred []byte) []byte {
	if cert, ok := identity.ParseDeviceCert(cred); ok && cert.Verify() {
		return cert.DevicePub
	}
	return cred
}

// credentialBoundToPeer verifies that a claimed member credential really belongs
// to the peer that presented it (its authenticated libp2p PeerID), defeating
// credential spoofing in the invite handshake:
//   - a legacy bare credential must equal the dialing account key (the PeerID);
//   - a device-cert credential must verify AND name the dialing device key as its
//     DevicePub — so a device can only ever claim its own account-signed cert.
func credentialBoundToPeer(cred []byte, p peer.ID) bool {
	pub, err := p.ExtractPublicKey()
	if err != nil {
		return false
	}
	raw, err := pub.Raw()
	if err != nil {
		return false
	}
	if cert, ok := identity.ParseDeviceCert(cred); ok {
		return cert.Verify() && ed25519Equal(cert.DevicePub, raw)
	}
	return ed25519Equal(cred, raw)
}

func ed25519Equal(a, b ed25519.PublicKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mlsIdentity returns the MLS credential + signing key for this install: a
// linked device signs with its own device key and presents its device cert; the
// original device keeps the legacy account-key credential + HKDF-derived signing
// key, so existing groups see no change.
func mlsIdentity(id *identity.Identity, marker *deviceMarker) (credential []byte, signingKey ed25519.PrivateKey) {
	if marker != nil && id.DeviceKey() != nil {
		return marker.Cert.Marshal(), id.DeviceKey()
	}
	return []byte(id.PublicKey()), deriveMLSSigningKey(id)
}
