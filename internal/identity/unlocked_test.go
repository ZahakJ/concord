package identity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A keystore an interim build wrote with a "clear_tag" field, in any format,
// opens as any other: the field is ignored, nothing is reported as altered,
// and nothing is re-sealed on its account.
func TestKeystoreWithAClearTagFieldOpensQuietly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _, err := LoadOrCreate(path, "pw")
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]json.RawMessage
	_ = json.Unmarshal(mustRead(t, path), &env)
	env["clear_tag"] = json.RawMessage(`"bm90IGEgdGFnIHRoaXMgYnVpbGQga25vd3M="`)
	out, _ := json.Marshal(env)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	decided := false
	setHooks(t, resealHooks{beforeReseal: func() { decided = true }})
	got, _, err := LoadOrCreate(path, "pw")
	if err != nil || got.Fingerprint() != id.Fingerprint() {
		t.Fatalf("a keystore with a clear_tag field: %v", err)
	}
	if TakeLabelAltered(path) {
		t.Fatal("an unknown field raised the someone-had-access alarm")
	}
	if decided {
		t.Fatal("an unknown field made the unlock re-seal the keystore")
	}
}

// A clear account key that names another account than the one sealed is the
// alteration an unlock can actually detect: the user is told once and the
// key is corrected.
func TestForgedClearAccountKeyIsReportedOnceAndCorrected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _, err := LoadOrCreate(path, "pw")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := Generate()
	var env map[string]any
	_ = json.Unmarshal(mustRead(t, path), &env)
	env["account_pub"] = []byte(other.PublicKey())
	out, _ := json.Marshal(env)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	unlockWithReseal(t, path, "pw", nil, resealHooks{})
	if !TakeLabelAltered(path) || TakeLabelAltered(path) {
		t.Fatal("the forged account key was not reported exactly once")
	}
	if !namesAccount(mustRead(t, path), id.PublicKey()) {
		t.Fatal("the forged account key was not corrected")
	}
}
