// Package app is layer 6 of Concord: it orchestrates the lower layers
// (identity, net, domain, media, store) and is the single surface the UI binds
// to. This file owns the on-disk layout so every entry point — the Wails GUI
// and the headless CLI alike — agrees on where Concord keeps its data.
package app

import (
	"os"
	"path/filepath"
)

// appDir is the subdirectory under the OS config root where Concord stores all
// per-user state.
const appDir = "concord"

// DataDir returns Concord's per-user data directory, creating it if needed.
// It follows OS conventions via os.UserConfigDir (e.g. ~/.config/concord on
// Linux). CONCORD_HOME overrides it, which is what lets us run several
// isolated instances on one machine for local multi-peer testing.
func DataDir() (string, error) {
	if override := os.Getenv("CONCORD_HOME"); override != "" {
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", err
		}
		return override, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, appDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// KeystorePath returns the path to the encrypted identity keystore in the
// default data directory.
func KeystorePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return keystorePathIn(dir), nil
}

// keystorePathIn returns the keystore path within an explicit data directory.
func keystorePathIn(dataDir string) string {
	return filepath.Join(dataDir, "keystore.json")
}

// dbPathIn returns the encrypted database path within an explicit data directory.
func dbPathIn(dataDir string) string {
	return filepath.Join(dataDir, "concord.db")
}

// HasIdentity reports whether an identity keystore already exists in dataDir,
// so the UI can show "unlock" vs "create a passphrase".
func HasIdentity(dataDir string) bool {
	_, err := os.Stat(keystorePathIn(dataDir))
	return err == nil
}

// ResetIdentity wipes the identity and all data tied to it (keystore, database,
// MLS group state) so a fresh identity can be created — for a forgotten
// passphrase or a corrupted keystore. Irreversible.
func ResetIdentity(dataDir string) error {
	for _, p := range []string{
		keystorePathIn(dataDir),
		keystorePathIn(dataDir) + ".tmp",
		dbPathIn(dataDir),
		dbPathIn(dataDir) + "-wal",
		dbPathIn(dataDir) + "-shm",
	} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// MLS state directory.
	if dir, err := mlsDirIn(dataDir); err == nil {
		_ = os.RemoveAll(dir)
	}
	return nil
}

// mlsDirIn returns the directory holding persistent MLS group state, creating
// it if needed.
func mlsDirIn(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "mls")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
