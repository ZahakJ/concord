package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func slotFixture(t *testing.T) (path string, id *Identity, slotID, u []byte) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "keystore.json")
	id, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateKeystore(path, "correct horse", id); err != nil {
		t.Fatal(err)
	}
	slotID, u = make([]byte, DeviceSlotIDSize), make([]byte, DeviceSecretSize)
	_, _ = rand.Read(slotID)
	_, _ = rand.Read(u)
	if err := AddDeviceSlot(path, id, slotID, u); err != nil {
		t.Fatal(err)
	}
	return path, id, slotID, u
}

func versionOf(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env keystoreFile
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	return env.Version
}

func TestDeviceSlotOpensToTheSameKeys(t *testing.T) {
	path, id, slotID, u := slotFixture(t)
	if v := versionOf(t, path); v != keystoreVersionSlot {
		t.Fatalf("version %d with a slot, want %d", v, keystoreVersionSlot)
	}
	got, err := OpenWithDeviceSlot(path, slotID, u)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Seed(), id.Seed()) || !bytes.Equal(got.DeviceSeed(), id.DeviceSeed()) {
		t.Fatal("the slot opened to other seeds")
	}
	if !bytes.Equal(DeviceSlotID(path), slotID) {
		t.Fatal("DeviceSlotID does not name the slot")
	}
	// The passphrase slot is untouched.
	if _, err := LoadKeystore(path, "correct horse"); err != nil {
		t.Fatalf("passphrase after enrolment: %v", err)
	}
	// Nothing secret in the clear.
	b, _ := os.ReadFile(path)
	for _, s := range [][]byte{u, id.Seed(), id.DeviceSeed()} {
		if bytes.Contains(b, s) || strings.Contains(string(b), string(mustB64(s))) {
			t.Fatal("a secret is in the file in the clear")
		}
	}
}

func mustB64(b []byte) []byte {
	j, _ := json.Marshal(b)
	return bytes.Trim(j, `"`)
}

func TestDeviceSlotRefusesAWrongSecretWithoutSendingToRecovery(t *testing.T) {
	path, _, slotID, u := slotFixture(t)
	bad := append([]byte(nil), u...)
	bad[0] ^= 1
	_, err := OpenWithDeviceSlot(path, slotID, bad)
	if !errors.Is(err, ErrDeviceSlotMismatch) || errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong U: %v", err)
	}
	other := append([]byte(nil), slotID...)
	other[0] ^= 1
	if _, err := OpenWithDeviceSlot(path, other, u); !errors.Is(err, ErrNoDeviceSlot) {
		t.Fatalf("another enrolment's id: %v", err)
	}
}

// The slot key binds the account and device keys: a slot whose recorded keys
// were edited does not open, and a slot moved into another keystore does not
// open to that keystore's account.
func TestDeviceSlotIsBoundToItsKeys(t *testing.T) {
	path, _, slotID, u := slotFixture(t)
	b, _ := os.ReadFile(path)
	var env keystoreFile
	_ = json.Unmarshal(b, &env)
	other, _ := Generate()
	env.DeviceSlot.DevicePub = other.DevicePublicKey()
	nb, _ := json.Marshal(env)
	_ = os.WriteFile(path, nb, 0o600)
	if _, err := OpenWithDeviceSlot(path, slotID, u); !errors.Is(err, ErrDeviceSlotMismatch) {
		t.Fatalf("edited device key: %v", err)
	}

	path2, _, _, _ := slotFixture(t)
	b2, _ := os.ReadFile(path2)
	var env2 keystoreFile
	_ = json.Unmarshal(b2, &env2)
	_ = json.Unmarshal(b, &env) // the original slot
	env2.DeviceSlot = env.DeviceSlot
	nb2, _ := json.Marshal(env2)
	_ = os.WriteFile(path2, nb2, 0o600)
	if _, err := OpenWithDeviceSlot(path2, slotID, u); !errors.Is(err, ErrDeviceSlotMismatch) {
		t.Fatalf("slot moved into another keystore: %v", err)
	}
}

// A passphrase change copies the slot byte for byte; a write for other keys
// (a restore of another account) drops it.
func TestDeviceSlotSurvivesAPassphraseChangeOnly(t *testing.T) {
	path, _, slotID, u := slotFixture(t)
	before := slotOf(mustRead(t, path))
	if err := ChangePassphrase(path, "correct horse", "battery staple"); err != nil {
		t.Fatal(err)
	}
	after := slotOf(mustRead(t, path))
	if after == nil || !bytes.Equal(after.Box, before.Box) || !bytes.Equal(after.Nonce, before.Nonce) {
		t.Fatal("the slot was not copied unchanged across a passphrase change")
	}
	if _, err := OpenWithDeviceSlot(path, slotID, u); err != nil {
		t.Fatalf("slot after passphrase change: %v", err)
	}

	other, _ := Generate()
	if err := ReplaceKeystore(path, "new account", other, mustRead(t, path)); err != nil {
		t.Fatal(err)
	}
	if slotOf(mustRead(t, path)) != nil || versionOf(t, path) != keystoreVersion {
		t.Fatal("a keystore for other keys kept the slot")
	}
}

func TestRemovingTheSlotWritesV2Again(t *testing.T) {
	path, id, slotID, u := slotFixture(t)
	wrong := append([]byte(nil), slotID...)
	wrong[0] ^= 1
	if err := RemoveDeviceSlot(path, id, wrong); err != nil {
		t.Fatal(err)
	}
	if DeviceSlotID(path) == nil {
		t.Fatal("removing another enrolment's id removed this one")
	}
	if err := RemoveDeviceSlot(path, id, slotID); err != nil {
		t.Fatal(err)
	}
	if DeviceSlotID(path) != nil || versionOf(t, path) != keystoreVersion {
		t.Fatal("the slot outlived its removal, or the file stayed v3")
	}
	if _, err := OpenWithDeviceSlot(path, slotID, u); !errors.Is(err, ErrNoDeviceSlot) {
		t.Fatalf("open after removal: %v", err)
	}
	if _, err := LoadKeystore(path, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveDeviceSlot(path, nil, nil); err == nil {
		t.Fatal("a locked device wrote the keystore")
	}
}

func TestAddDeviceSlotRefusesAnotherAccount(t *testing.T) {
	path, _, slotID, u := slotFixture(t)
	other, _ := Generate()
	if err := AddDeviceSlot(path, other, slotID, u); !errors.Is(err, ErrNotThisAccount) {
		t.Fatalf("enrolling another account's keys: %v", err)
	}
	if err := AddDeviceSlot(path, other, slotID, u[:8]); err == nil {
		t.Fatal("a short U was taken")
	}
}

// A build that knows only v2 refuses a v3 file as a newer format, with the
// words the unlock screen turns into "saved by a newer version".
func TestV3IsANewerFormatToAV2Reader(t *testing.T) {
	path, _, _, _ := slotFixture(t)
	b := mustRead(t, path)
	var env keystoreFile
	_ = json.Unmarshal(b, &env)
	env.Version = maxKeystoreVersion + 1
	nb, _ := json.Marshal(env)
	_, err := OpenKeystoreBytes(nb, "correct horse")
	if err == nil || !strings.Contains(err.Error(), "unsupported keystore version") || errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("newer version: %v", err)
	}
}
