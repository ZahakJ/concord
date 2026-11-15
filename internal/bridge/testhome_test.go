package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
)

// A test that builds a Bridge and sets no CONCORD_HOME must not be able to
// reach the default data directory, which on a developer's machine is their
// real profile. One existing test called ConfirmWipe on a bare Bridge and
// relied on the typed-word check to return first; with that check broken the
// call went on to erase the default directory. The data directory itself now
// refuses under `go test`, so no check under test stands alone in front of it.
//
// The config root is pointed at a temporary directory holding something that
// looks like a profile, so that a failure of the guard shows as those files
// vanishing rather than as damage to anything real.
func TestABridgeUnderTestCannotReachTheDefaultHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CONCORD_HOME", "")
	for _, v := range []string{"XDG_CONFIG_HOME", "HOME", "USERPROFILE", "APPDATA", "AppData"} {
		t.Setenv(v, root)
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if rel, rerr := filepath.Rel(root, cfg); rerr != nil || filepath.IsAbs(rel) || rel == ".." || len(rel) > 1 && rel[:2] == ".." {
		t.Fatalf("setup: the config root %s is not under the temporary directory", cfg)
	}
	profile := filepath.Join(cfg, "concord")
	files := []string{"keystore.json", "concord.db", "peers.json", filepath.Join("mls", "group")}
	for _, f := range files {
		p := filepath.Join(profile, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("kept"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	intact := func(after string) {
		t.Helper()
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(profile, f)); err != nil {
				t.Fatalf("after %s: %s is gone from the default data directory", after, f)
			}
		}
		if _, err := os.Stat(filepath.Join(profile, "keystore.json.lock")); err == nil {
			t.Fatalf("after %s: the keystore in the default data directory was locked for writing", after)
		}
	}

	// The wipe, with a live ticket and the right word: nothing but the data
	// directory's own refusal is in the way.
	armTicket(t, "abc123", "euclid", time.Now())
	if err := (&Bridge{}).ConfirmWipe("abc123", "euclid"); !errors.Is(err, appsvc.ErrNoHomeUnderTest) {
		t.Fatalf("ConfirmWipe on a bare Bridge with no CONCORD_HOME: %v, want ErrNoHomeUnderTest", err)
	}
	intact("ConfirmWipe")

	b := New(context.Background())
	t.Cleanup(b.Close)
	if err := b.Login("some passphrase"); !errors.Is(err, appsvc.ErrNoHomeUnderTest) {
		t.Fatalf("Login with no CONCORD_HOME: %v, want ErrNoHomeUnderTest", err)
	}
	intact("Login")
	if _, err := b.BeginStartOver(); !errors.Is(err, appsvc.ErrNoHomeUnderTest) {
		t.Fatalf("BeginStartOver with no CONCORD_HOME: %v, want ErrNoHomeUnderTest", err)
	}
	if _, err := b.HasIdentity(); !errors.Is(err, appsvc.ErrNoHomeUnderTest) {
		t.Fatalf("HasIdentity with no CONCORD_HOME: %v, want ErrNoHomeUnderTest", err)
	}
	if _, err := appsvc.KeystorePath(); !errors.Is(err, appsvc.ErrNoHomeUnderTest) {
		t.Fatalf("KeystorePath with no CONCORD_HOME: %v, want ErrNoHomeUnderTest", err)
	}
	intact("the rest")
}
