package app

import (
	"bytes"
	"testing"

	"github.com/zahak/concord/internal/identity"
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
	m, ok := loadDeviceMarker(dir, id.PublicKey())
	if !ok || m.Cert == nil {
		t.Fatal("marker should load under its own account")
	}
	// …but is ignored for a different account (a copied marker isn't trusted).
	other, _ := identity.Generate()
	if _, ok := loadDeviceMarker(dir, other.PublicKey()); ok {
		t.Fatal("marker must not load under a foreign account")
	}
}

func TestNoMarkerMeansSingleDevice(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.Generate()
	if _, ok := loadDeviceMarker(dir, id.PublicKey()); ok {
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
	m, ok := loadDeviceMarker(dir, id.PublicKey())
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
