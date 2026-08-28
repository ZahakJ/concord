package app

import (
	"bytes"
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
)

func TestDeviceMarkerRoundTripAndScoping(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.Generate()
	cert, err := id.IssueDeviceCert("Phone", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveDeviceMarker(dir, cert); err != nil {
		t.Fatal(err)
	}

	// Loads under the correct account…
	m, ok := loadDeviceMarker(dir, id.PublicKey(), id.DevicePublicKey())
	if !ok || m.Cert == nil {
		t.Fatal("marker should load under its own account")
	}
	// …but is ignored for a different account (a copied marker isn't trusted).
	other, _ := identity.Generate()
	if _, ok := loadDeviceMarker(dir, other.PublicKey(), id.DevicePublicKey()); ok {
		t.Fatal("marker must not load under a foreign account")
	}
}

func TestNoMarkerMeansSingleDevice(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.Generate()
	if _, ok := loadDeviceMarker(dir, id.PublicKey(), id.DevicePublicKey()); ok {
		t.Fatal("no marker file should mean single-device mode")
	}
	// mlsIdentity with no marker returns the legacy account credential + the
	// account-derived signing key — byte-for-byte the pre-multi-device behavior.
	cred, signing := mlsIdentity(id, nil)
	if !bytes.Equal(cred, id.PublicKey()) {
		t.Fatal("single-device credential must be the bare account key")
	}
	if !bytes.Equal(signing, deriveMLSSigningKey(id)) {
		t.Fatal("single-device signing key must be the account-derived key")
	}
}

func TestLinkedMlsIdentityUsesDeviceKeyAndCert(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.Generate()
	cert, _ := id.IssueDeviceCert("Phone", 1700000000)
	_ = saveDeviceMarker(dir, cert)
	m, ok := loadDeviceMarker(dir, id.PublicKey(), id.DevicePublicKey())
	if !ok {
		t.Fatal("marker should load")
	}

	cred, signing := mlsIdentity(id, m)
	// The credential is the device cert (not the bare account key)…
	parsed, isCert := identity.ParseDeviceCert(cred)
	if !isCert || !parsed.Verify() {
		t.Fatal("linked credential should be a verifiable device cert")
	}
	// …and it still resolves back to the account (this is what the whole
	// normalization layer relies on).
	if accountFingerprintOf(cred) != id.Fingerprint() {
		t.Fatal("device-cert credential must map to the account fingerprint")
	}
	// The signing key is the device key, so the MLS leaf's signature key matches
	// the cert's DevicePub.
	if !bytes.Equal(signing, id.DeviceKey()) {
		t.Fatal("linked signing key should be the device key")
	}
	if !bytes.Equal(parsed.DevicePub, id.DevicePublicKey()) {
		t.Fatal("cert DevicePub should match the device key's public half")
	}
}

// A device that was LINKED into an account keeps an account-signed certificate
// for its own device key in device.json. Starting over — "forgot passphrase →
// use my recovery phrase", or the reset button — has to forget that certificate
// along with everything else: the reset generates a brand-new device key on the
// next unlock, so a surviving marker makes the install present a certificate for
// a key it no longer holds. It believes it is linked, peers check the credential
// against the connection and refuse it, and nothing on either side says why.
func TestResetIdentityForgetsTheDeviceMarker(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.Generate()
	cert, err := id.IssueDeviceCert("Phone", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveDeviceMarker(dir, cert); err != nil {
		t.Fatal(err)
	}
	if err := ResetIdentity(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDeviceMarker(dir, id.PublicKey(), id.DevicePublicKey()); ok {
		t.Fatal("the linked-device marker survived a reset: this install will claim to be a device it can no longer prove it is")
	}
}

// Defence in depth for the same failure: even if a marker reaches the data dir
// some other way, it only describes THIS install while it certifies the device
// key this install actually holds. Verifying the signature and the account is
// not enough — both stay true across a reset, and only the device key moves.
func TestDeviceMarkerMustCertifyThisDeviceKey(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.Generate()
	cert, err := id.IssueDeviceCert("Phone", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveDeviceMarker(dir, cert); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDeviceMarker(dir, id.PublicKey(), id.DevicePublicKey()); !ok {
		t.Fatal("a marker certifying this device's own key must load")
	}
	// Same account, same valid signature — a different device key.
	stranger, _ := identity.Generate()
	if _, ok := loadDeviceMarker(dir, id.PublicKey(), stranger.DevicePublicKey()); ok {
		t.Fatal("a marker certifying some other device's key must not load")
	}
}
