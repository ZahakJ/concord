package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A damaged or crafted keystore — a flipped bit in a stored Argon2 setting,
// a zero, a value no build writes, a number JSON cannot fit — must fail to
// open with an ordinary error, quickly: not panic, not hang, not ask for
// terabytes. Values that parse but are out of range are reported as the
// wrong-passphrase-or-corrupted error the recovery flow keys on. The same
// holds for passphrase-sealed envelopes, which can arrive from elsewhere.
func TestOutOfRangeKDFSettingsFailToOpenWithoutHarm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	if err := CreateKeystore(path, "pw", id); err != nil {
		t.Fatal(err)
	}
	good := mustRead(t, path)
	sealed, err := SealWithPassphrase("pw", []byte("archive"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		field, value string
		corrupt      bool // parses, and must be ErrWrongPassphrase
	}{
		{"argon_time", "0", true},
		{"argon_threads", "0", true},
		{"argon_memory", "0", true},
		{"argon_time", "17", true},
		{"argon_time", "4294967295", true},
		{"argon_memory", "4294967295", true},
		{"argon_memory", "1048577", true},
		{"argon_threads", "255", true},
		{"argon_memory", "7", true},
		{"salt", `"AAAA"`, true},
		{"salt", "null", true},
		{"argon_time", "-1", false},
		{"argon_time", "4294967296", false},
		{"argon_threads", "256", false},
		{"argon_memory", "1e30", false},
		{"argon_time", `"3"`, false},
	} {
		for kind, base := range map[string][]byte{"keystore": good, "envelope": sealed} {
			var env map[string]json.RawMessage
			if err := json.Unmarshal(base, &env); err != nil {
				t.Fatal(err)
			}
			env[tc.field] = json.RawMessage(tc.value)
			blob, _ := json.Marshal(env)

			done := make(chan error, 1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						done <- errors.New("panic")
						t.Errorf("%s %s=%s: panicked: %v", kind, tc.field, tc.value, r)
					}
				}()
				if kind == "keystore" {
					if err := os.WriteFile(path, blob, 0o600); err != nil {
						done <- err
						return
					}
					_, err := LoadKeystore(path, "pw")
					done <- err
					return
				}
				_, err := OpenWithPassphrase("pw", blob)
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Errorf("%s %s=%s: opened", kind, tc.field, tc.value)
				} else if tc.corrupt && !errors.Is(err, ErrWrongPassphrase) {
					t.Errorf("%s %s=%s: %v, want the wrong-passphrase-or-corrupted error", kind, tc.field, tc.value, err)
				}
			case <-time.After(20 * time.Second):
				t.Fatalf("%s %s=%s: still deriving after 20 s", kind, tc.field, tc.value)
			}
		}
	}
}

// An empty, truncated or overwritten keystore is reported as the
// wrong-passphrase-or-corrupted error the unlock screen sends to recovery,
// and an unlock never creates an account over it.
func TestDamagedKeystoreIsReportedForRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	if err := CreateKeystore(path, "pw", id); err != nil {
		t.Fatal(err)
	}
	good := mustRead(t, path)
	for name, blob := range map[string][]byte{
		"empty":       {},
		"truncated":   good[:len(good)/2],
		"overwritten": []byte("\x00\x00\x00\x00"),
		"no nonce":    []byte(`{"version":2,"kdf":"argon2id"}`),
	} {
		if err := os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadKeystore(path, "pw"); !errors.Is(err, ErrWrongPassphrase) {
			t.Errorf("%s: %v, want the wrong-passphrase-or-corrupted error", name, err)
		}
		if _, created, err := LoadOrCreate(path, "pw"); created || !errors.Is(err, ErrWrongPassphrase) {
			t.Errorf("%s: unlock created %v, %v", name, created, err)
		}
		if !bytes.Equal(mustRead(t, path), blob) {
			t.Errorf("%s: the unlock wrote over the damaged keystore", name)
		}
	}
}
