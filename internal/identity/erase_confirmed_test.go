package identity

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// An erase the user confirmed for one keystore cannot be carried out without
// the keystore lock: without it, its check and its erase are not one step,
// and a holder of the lock that finishes writing another keystore in between
// would have that one erased. So it erases nothing, leaves no erase pending,
// and says the keystore is busy. Once the lock is free, the same erase goes
// through.
func TestAConfirmedEraseThatCannotTakeTheLockErasesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "ada-pass"); err != nil {
		t.Fatal(err)
	}
	ada := mustRead(t, path)
	isAda := func() bool {
		now, err := ReadKeystore(path)
		return err == nil && bytes.Equal(now, ada)
	}
	thens := 0
	then := func() { thens++ }

	release := holdKeystoreInProcess(t)
	err := RemoveKeystoreIf(path, isAda, then)
	if !errors.Is(err, ErrKeystoreBusy) {
		t.Fatalf("confirmed erase without the lock: %v, want ErrKeystoreBusy", err)
	}
	if got, rerr := ReadKeystore(path); rerr != nil || !bytes.Equal(got, ada) {
		t.Fatalf("confirmed erase without the lock removed the keystore (%v)", rerr)
	}
	if _, serr := os.Lstat(path + ".erased"); !os.IsNotExist(serr) {
		t.Fatalf("confirmed erase without the lock left an erase pending: %v", serr)
	}
	if thens != 0 {
		t.Fatal("confirmed erase without the lock erased the rest of the device")
	}

	release()
	if err := RemoveKeystoreIf(path, isAda, then); err != nil {
		t.Fatalf("the same erase once the lock is free: %v", err)
	}
	if _, serr := os.Lstat(path); !os.IsNotExist(serr) {
		t.Fatalf("the keystore is still here after the erase: %v", serr)
	}
	if thens != 1 {
		t.Fatalf("the rest of the device was erased %d times, want once", thens)
	}
}

// The case that makes it necessary: the lock holder is a restore of Bob, and
// it renames his keystore in just after the check approved Ada's. Bob's
// keystore must survive an erase that was confirmed for Ada.
func TestAConfirmedEraseLeavesAKeystoreWrittenAfterItsCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "ada-pass"); err != nil {
		t.Fatal(err)
	}
	ada := mustRead(t, path)
	other := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(other, "bob-pass"); err != nil {
		t.Fatal(err)
	}
	bob := mustRead(t, other)

	release := holdKeystoreInProcess(t)
	renamed := false
	bobLands := func() {
		if renamed {
			return
		}
		renamed = true
		tmp := path + ".bob"
		if err := os.WriteFile(tmp, bob, 0o600); err != nil {
			t.Error(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Error(err)
		}
	}
	still := func() bool {
		now, err := ReadKeystore(path)
		ok := err == nil && bytes.Equal(now, ada)
		bobLands()
		return ok
	}
	err := RemoveKeystoreIf(path, still, nil)
	bobLands() // if the erase never looked, the holder finishes all the same
	release()
	if got, rerr := ReadKeystore(path); rerr != nil || !bytes.Equal(got, bob) {
		t.Fatalf("an erase confirmed for Ada's keystore (err %v) removed Bob's, written after its check (%v)", err, rerr)
	}
	if _, serr := os.Lstat(path + ".erased"); !os.IsNotExist(serr) {
		t.Fatalf("an erase is pending over Bob's keystore: %v", serr)
	}
}

// A confirmed erase needs its check: with none, it erases nothing.
func TestAConfirmedEraseWithoutACheckErasesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if _, _, err := LoadOrCreate(path, "pw"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveKeystoreIf(path, nil, nil); err == nil {
		t.Fatal("an erase with no check went through")
	}
	if !HasKeystore(path) {
		t.Fatal("an erase with no check removed the keystore")
	}
}
