// Command releasekey generates the release signing keypair and signs a release
// manifest with it.
//
// The private half never enters the repository, the build, or CI — it lives on
// the release machine and is used at publish time only. The public half is
// committed (internal/bridge/release-pubkey.txt) and compiled into every
// binary, which is what makes it un-swappable by whoever serves the download.
//
//	releasekey gen                  # make a keypair, print the public half
//	releasekey sign dist/SHA256SUMS # write dist/SHA256SUMS.sig
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "gen":
		err = gen()
	case "sign":
		if len(os.Args) != 3 {
			usage()
		}
		err = sign(os.Args[2])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasekey:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: releasekey gen | releasekey sign <file>")
	os.Exit(2)
}

// keyPath is where the private key lives: outside the repo, user-only.
func keyPath() (string, error) {
	if p := os.Getenv("CONCORD_RELEASE_KEY"); p != "" {
		return p, nil
	}
	home, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "concord", "release.key"), nil
}

func gen() error {
	path, err := keyPath()
	if err != nil {
		return err
	}
	// Never silently replace a signing key: every release ever made with the old
	// one becomes unverifiable, and there's no way to undo that.
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists — refusing to overwrite an existing signing key", path)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf(`Private key written to %s (mode 0600).

  BACK IT UP, OFFLINE. Lose it and you can never sign another update that
  existing installs will accept. Leak it and someone else can.

Public key — put this line in internal/bridge/release-pubkey.txt and commit it:

%s
`, path, hex.EncodeToString(pub))
	return nil
}

func sign(file string) error {
	path, err := keyPath()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read signing key (%s): %w", path, err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.PrivateKeySize {
		return fmt.Errorf("signing key at %s is not a hex ed25519 private key", path)
	}
	msg, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(ed25519.PrivateKey(seed), msg)
	out := file + ".sig"
	if err := os.WriteFile(out, sig, 0o644); err != nil {
		return err
	}
	fmt.Printf("signed %s -> %s\n", file, out)
	return nil
}
