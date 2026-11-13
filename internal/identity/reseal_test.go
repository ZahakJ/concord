package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stripAccountPub rewrites the keystore at path the way builds before the
// clear account key wrote it, so the next unlock decides to re-seal.
func stripAccountPub(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	delete(env, "account_pub")
	out, _ := json.Marshal(env)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// setHooks installs h for the duration of the test.
func setHooks(t *testing.T, h resealHooks) {
	t.Helper()
	testHooks = h
	t.Cleanup(func() { testHooks = resealHooks{} })
}

// unlockWithReseal runs LoadOrCreate with the seams in h (gap is called at
// the moment the unlock has decrypted the file and decided to re-seal, before
// the re-seal looks at the file) and returns once that re-seal has finished.
func unlockWithReseal(t *testing.T, path, pass string, gap func(), h resealHooks) {
	t.Helper()
	done := make(chan struct{}, 1)
	var decided atomic.Bool
	h.beforeReseal = func() {
		decided.Store(true)
		if gap != nil {
			gap()
		}
	}
	h.resealDone = func() { done <- struct{}{} }
	setHooks(t, h)
	if _, _, err := LoadOrCreate(path, pass); err != nil {
		t.Fatal(err)
	}
	if !decided.Load() {
		t.Fatal("the unlock did not decide to re-seal")
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the re-seal never finished")
	}
}

func assertOpensWithOnly(t *testing.T, path, want, notWant string) {
	t.Helper()
	if _, err := LoadKeystore(path, want); err != nil {
		t.Fatalf("passphrase %q no longer opens the keystore: %v", want, err)
	}
	if _, err := LoadKeystore(path, notWant); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("passphrase %q still opens the keystore (err %v): a re-seal wrote it back", notWant, err)
	}
}

// The interleaving that review found: an unlock decrypts an older keystore
// under the old passphrase, a passphrase change lands, and only then does the
// re-seal look at the file. Checked against a second read of the path, the
// re-seal saw the NEW file as "unchanged" and sealed the old passphrase back
// over it.
func TestResealNeverRevertsAPassphraseChangeAfterTheUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	stripAccountPub(t, path)
	unlockWithReseal(t, path, "old", func() {
		if err := ChangePassphrase(path, "old", "new"); err != nil {
			t.Fatal(err)
		}
	}, resealHooks{})
	assertOpensWithOnly(t, path, "new", "old")
}

// The same gap on the other path that re-seals at unlock: a keystore with no
// device seed yet, upgraded synchronously.
func TestDeviceSeedUpgradeNeverRevertsAPassphraseChangeAfterTheUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	id, _ := Generate()
	sealRaw(t, path, "old", id) // v1: account seed only
	unlockWithReseal(t, path, "old", func() {
		if err := ChangePassphrase(path, "old", "new"); err != nil {
			t.Fatal(err)
		}
	}, resealHooks{})
	assertOpensWithOnly(t, path, "new", "old")
}

// A passphrase change and a background re-seal, started together with no
// ordering between them, many times: whichever wins, the passphrase in force
// afterwards is the new one. Run it under -race.
func TestResealAndPassphraseChangeInAnyOrder(t *testing.T) {
	n := 8
	if testing.Short() {
		n = 3
	}
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pass-0"); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		cur, next := fmt.Sprintf("pass-%d", i), fmt.Sprintf("pass-%d", i+1)
		stripAccountPub(t, path)
		done := make(chan struct{}, 1)
		var decided atomic.Bool
		setHooks(t, resealHooks{
			beforeReseal: func() { decided.Store(true) },
			resealDone:   func() { done <- struct{}{} },
		})
		var wg sync.WaitGroup
		var changeErr, unlockErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.IntN(80)) * time.Millisecond)
			changeErr = ChangePassphrase(path, cur, next)
		}()
		go func() {
			defer wg.Done()
			_, _, unlockErr = LoadOrCreate(path, cur)
		}()
		wg.Wait()
		if changeErr != nil {
			t.Fatalf("iteration %d: change: %v", i, changeErr)
		}
		// The unlock may have lost to the change outright (wrong passphrase by
		// the time it read the file); anything else is a failure.
		if unlockErr != nil && !errors.Is(unlockErr, ErrWrongPassphrase) {
			t.Fatalf("iteration %d: unlock: %v", i, unlockErr)
		}
		if decided.Load() {
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Fatalf("iteration %d: the re-seal never finished", i)
			}
		}
		assertOpensWithOnly(t, path, next, cur)
	}
}

// An erase that lands while the re-seal is deriving its key (it has already
// found the file unchanged) must not be undone by the re-seal's rename. The
// unlock that spawns the re-seal is followed at once by the check for a
// revoked device, whose erase is exactly this.
func TestResealDoesNotRecreateAnErasedKeystore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	stripAccountPub(t, path)
	erased := make(chan error, 1)
	unlockWithReseal(t, path, "pw", nil, resealHooks{
		resealChecked: func() {
			go func() { erased <- RemoveKeystore(path) }()
			// Give an erase that does not wait for the re-seal the chance
			// to finish first; one that waits stays blocked here.
			select {
			case err := <-erased:
				erased <- err
			case <-time.After(300 * time.Millisecond):
			}
		},
	})
	if err := <-erased; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the keystore came back after it was erased (stat: %v)", err)
	}
	// And an unlock after the erase starts from nothing rather than finding a
	// re-seal's leftovers.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("a re-seal temp file survived the erase (stat: %v)", err)
	}
}
