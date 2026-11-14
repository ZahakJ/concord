package identity

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// An unlock that finds no keystore and creates an account says so in the
// log, so an operator of a service that unlocks itself can see it happen.
func TestCreatingANewAccountIsLogged(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, created, err := LoadOrCreate(path, "pw")
	if err != nil || !created {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "created a new account") || !strings.Contains(logged.String(), strings.Fields(id.Fingerprint())[0]) {
		t.Fatalf("creating an account left no log line: %q", logged.String())
	}
	if strings.Contains(logged.String(), id.Fingerprint()) {
		t.Fatalf("the log names the whole account: %q", logged.String())
	}
	logged.Reset()
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logged.String(), "created a new account") {
		t.Fatal("an ordinary unlock was logged as creating an account")
	}
}

// The create guard runs only inside the create, under the lock, after the
// check that there is still no keystore: a keystore another process puts
// there first (a restore over the device's data) is never met by the guard,
// so the guard can never delete that account's data.
func TestCreateGuardNeverRunsOverAKeystoreThatAppeared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	called := false
	orig := CreateGuard
	CreateGuard = func(string) error { called = true; return nil }
	t.Cleanup(func() { CreateGuard = orig })
	other, _ := Generate()
	setHooks(t, resealHooks{beforeCreate: func() {
		if err := CreateKeystore(path, "theirs", other); err != nil { // the other process
			t.Fatal(err)
		}
	}})
	if _, _, err := LoadOrCreate(path, "mine"); err == nil {
		t.Fatal("the losing create opened a keystore under another passphrase")
	}
	if called {
		t.Fatal("the create guard ran although a keystore had appeared")
	}
	if got, err := LoadKeystore(path, "theirs"); err != nil || got.Fingerprint() != other.Fingerprint() {
		t.Fatalf("the other process's keystore: %v", err)
	}
}

// A keystore that is a symlink to something that cannot be read (a drive not
// mounted) is not "no keystore": the unlock says what it is, and creates
// nothing.
func TestDanglingKeystoreLinkIsNamedNotTakenForAbsence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	target := filepath.Join(dir, "unmounted", "keystore.json")
	if err := os.Symlink(target, path); err != nil {
		t.Skip("no symlinks here")
	}
	_, created, err := LoadOrCreate(path, "pw")
	if err == nil || created {
		t.Fatalf("unlock through a dangling keystore link: created %v, %v", created, err)
	}
	if !strings.Contains(err.Error(), "symbolic link") || !strings.Contains(err.Error(), target) {
		t.Fatalf("the error does not say the keystore is a link to %s: %v", target, err)
	}
	// The screens ask the same question the unlock does: a keystore is there,
	// just unreachable, so they offer the unlock (which says why) rather than
	// a create form.
	if !HasKeystore(path) {
		t.Fatal("HasKeystore took a dangling keystore link for no keystore")
	}
}
