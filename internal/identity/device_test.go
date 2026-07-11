package identity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDeviceSeedGeneratedAndDistinct(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if id.DeviceSeed() == nil {
		t.Fatal("Generate should produce a device seed")
	}
	if bytes.Equal(id.Seed(), id.DeviceSeed()) {
		t.Fatal("device seed must differ from account seed")
	}
	// Device key is stable and distinct from the account key.
	if !bytes.Equal(id.DeviceKey().Seed(), id.DeviceSeed()) {
		t.Fatal("DeviceKey should derive from the device seed")
	}
	if bytes.Equal(id.DevicePublicKey(), id.PublicKey()) {
		t.Fatal("device pubkey must differ from account pubkey")
	}
}

func TestDeviceCertSignatureVerifies(t *testing.T) {
	id, _ := Generate()
	cert, err := id.IssueDeviceCert("Test Device", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.Verify() {
		t.Fatal("account key should verify its own device cert")
	}
	if cert.AccountFingerprint() != id.Fingerprint() {
		t.Fatal("cert account fingerprint should match the identity")
	}
	// Tampering with the device pubkey invalidates the signature.
	cert.DevicePub[0] ^= 0xff
	if cert.Verify() {
		t.Fatal("tampered cert must not verify")
	}
	// Round-trips through marshal/parse.
	fresh, _ := id.IssueDeviceCert("Test Device", 1700000000)
	parsed, ok := ParseDeviceCert(fresh.Marshal())
	if !ok || !parsed.Verify() {
		t.Fatal("marshalled cert should parse and verify")
	}
	// A legacy 32-byte raw credential is reported as not-a-cert.
	if _, ok := ParseDeviceCert(id.PublicKey()); ok {
		t.Fatal("bare account pubkey must be reported as legacy (not a cert)")
	}
}

// TestLinkingDeviceIssuesCertForNewDevice covers the linking flow's core: an
// existing (unlocked) device signs a cert for a brand-new device's key.
func TestLinkingDeviceIssuesCertForNewDevice(t *testing.T) {
	existing, _ := Generate()  // desktop, already has the account
	newDevice, _ := Generate() // phone, only its device key matters here
	cert := existing.IssueDeviceCertFor(newDevice.DevicePublicKey(), "Phone", 1700000000)
	if !cert.Verify() {
		t.Fatal("linking device's cert should verify under the account key")
	}
	if cert.AccountFingerprint() != existing.Fingerprint() {
		t.Fatal("cert should certify the existing account")
	}
}

func TestDeviceSeedSurvivesSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	if err := SaveKeystore(path, "pw", id); err != nil {
		t.Fatal(err)
	}
	got, err := LoadKeystore(path, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.DeviceSeed(), id.DeviceSeed()) {
		t.Fatal("device seed did not round-trip through the keystore")
	}
}

// TestLegacyKeystoreUpgradesToV2 simulates a v1 keystore (account seed only)
// and checks LoadOrCreate adds a device seed and persists it as v2.
func TestLegacyKeystoreUpgradesToV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	// Write a v1-style file: seal only the account seed, drop the device fields.
	if err := SaveKeystore(path, "pw", id); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	env["version"] = 1
	delete(env, "device_nonce")
	delete(env, "device_ciphertext")
	raw, _ = json.Marshal(env)
	_ = os.WriteFile(path, raw, 0o600)

	// Loading a v1 file works and yields no device seed…
	loaded, err := LoadKeystore(path, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DeviceSeed() != nil {
		t.Fatal("v1 file should have no device seed")
	}
	// …but LoadOrCreate upgrades it in place.
	up, _, err := LoadOrCreate(path, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if up.DeviceSeed() == nil {
		t.Fatal("LoadOrCreate should have generated a device seed")
	}
	// The account key is unchanged by the upgrade.
	if !bytes.Equal(up.PublicKey(), id.PublicKey()) {
		t.Fatal("upgrade must not change the account key")
	}
	// And it's now persisted as v2 with the device seed present.
	raw, _ = os.ReadFile(path)
	env = nil
	_ = json.Unmarshal(raw, &env)
	if env["version"].(float64) != 2 {
		t.Fatalf("expected version 2 after upgrade, got %v", env["version"])
	}
	reloaded, _ := LoadKeystore(path, "pw")
	if !bytes.Equal(reloaded.DeviceSeed(), up.DeviceSeed()) {
		t.Fatal("upgraded device seed did not persist")
	}
}
