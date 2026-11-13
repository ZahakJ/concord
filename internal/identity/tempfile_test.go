package identity

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func skipWithoutLinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic and hard links need privileges on Windows")
	}
}

// A symlink planted where the old code put its temp file must not receive
// the keystore, and the keystore must not become a link.
func TestKeystoreWriteNeverFollowsAPlantedTempName(t *testing.T) {
	skipWithoutLinks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(decoy, []byte("decoy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(decoy, path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(path, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(decoy); !bytes.Equal(got, []byte("decoy")) {
		t.Fatal("the keystore was written through a planted symlink")
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the keystore became a link (%v)", err)
	}
	assertOpensWithOnly(t, path, "new", "old")
}

// A hard link from the old temp name to the keystore (a backup tool's copy)
// must not let the write rewrite the live keystore in place; the change
// lands as a new file renamed over the old one.
func TestKeystoreWriteNeverRewritesAHardLinkedKeystoreInPlace(t *testing.T) {
	skipWithoutLinks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(path, "old", "new"); err != nil {
		t.Fatal(err)
	}
	live, _ := os.Lstat(path)
	other, _ := os.Lstat(path + ".tmp")
	if os.SameFile(live, other) {
		t.Fatal("the live keystore was rewritten in place through a hard link")
	}
	assertOpensWithOnly(t, path, "new", "old")
}

// A keystore that is itself a symlink is not written through: replacing it
// would leave the keystore it names, under the old passphrase, in place.
func TestKeystoreWriteRefusesASymlinkedKeystore(t *testing.T) {
	skipWithoutLinks(t)
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if _, _, err := LoadOrCreate(real, "old"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "keystore.json")
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(path, "old", "new"); !errors.Is(err, errKeystoreSymlink) {
		t.Fatalf("change through a symlinked keystore: %v", err)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced")
	}
	if _, err := LoadKeystore(real, "old"); err != nil {
		t.Fatalf("the linked keystore changed: %v", err)
	}
}

// A temp file a crashed write left is removed by the next write.
func TestKeystoreWriteRemovesStaleTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	if _, _, err := LoadOrCreate(path, "old"); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".keystore.json.12345.tmp")
	if err := os.WriteFile(stale, []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(path, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("a stale temp file survived a write (stat %v)", err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".keystore.json.*.tmp"))
	if len(left) != 0 {
		t.Fatalf("a write left its temp file: %v", left)
	}
}
