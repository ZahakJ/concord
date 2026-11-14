package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Under `go test` the default data directory is never handed out: it is a real
// profile on whoever's machine runs the tests. CONCORD_HOME is the only way.
func TestDataDirRefusesTheDefaultHomeUnderTest(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "HOME", "USERPROFILE", "APPDATA", "AppData"} {
		t.Setenv(v, root)
	}
	t.Setenv("CONCORD_HOME", "")
	if dir, err := DataDir(); !errors.Is(err, ErrNoHomeUnderTest) {
		t.Fatalf("DataDir with no CONCORD_HOME under test = %q, %v; want ErrNoHomeUnderTest", dir, err)
	}
	if _, err := KeystorePath(); !errors.Is(err, ErrNoHomeUnderTest) {
		t.Fatalf("KeystorePath with no CONCORD_HOME under test: %v; want ErrNoHomeUnderTest", err)
	}
	// And it did not create the directory on the way to refusing.
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == appDir {
			t.Errorf("the default data directory was created at %s", p)
		}
		return nil
	})

	home := filepath.Join(root, "chosen")
	t.Setenv("CONCORD_HOME", home)
	if dir, err := DataDir(); err != nil || dir != home {
		t.Fatalf("DataDir with CONCORD_HOME = %q, %v; want %q", dir, err, home)
	}
}
