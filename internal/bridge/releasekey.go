package bridge

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
)

// Distinct so the UI can tell "this release wasn't signed by us" apart from an
// ordinary network failure — one is a red flag, the other is a bad afternoon.
var (
	errUnsignedRelease     = errors.New("release is not signed — refusing to install")
	errBadReleaseSignature = errors.New("release signature does not verify — refusing to install")
)

// Release signing.
//
// Until now the updater's trust anchor was "GitHub's TLS certificate, plus
// GitHub's account security": it fetched the binary and SHA256SUMS from the
// same release, so the checksum proved the download hadn't been corrupted in
// transit but said nothing about who produced it. Anyone able to write to the
// release could replace both halves and every client would install it happily.
//
// So releases are signed with an offline ed25519 key whose public half is
// compiled into this binary. SHA256SUMS already lists every asset's hash, which
// makes it the natural manifest — sign that one file and every asset is covered
// by it. Verification order matters: check the signature over SHA256SUMS first,
// then check the downloaded file against the now-trusted hash. Never the other
// way round.
//
// The point isn't only GitHub. Once a signature is what's trusted, WHERE the
// bytes came from stops mattering — which is the prerequisite for ever fetching
// an update from a peer instead of a server.

// The public key ships in the repository (it's public by definition) and is
// embedded at build time, so it can't be swapped by whoever serves the update.
// Generate a keypair with `make release-keygen`; keep the private half offline.
//
//go:embed release-pubkey.txt
var releasePubKeyFile string

// releasePubKey returns the embedded verification key, or nil when this build
// wasn't given one.
//
// A build with no key keeps the old checksum-only behavior rather than refusing
// to update at all. That is not a hole an attacker can walk through: the key is
// baked into the user's own binary, so it can't be stripped remotely — a build
// that HAS a key always enforces it. It only means builds made before signing
// existed carry on working.
func releasePubKey() ed25519.PublicKey {
	for _, line := range strings.Split(releasePubKeyFile, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		b, err := hex.DecodeString(line)
		if err != nil || len(b) != ed25519.PublicKeySize {
			return nil
		}
		return ed25519.PublicKey(b)
	}
	return nil
}

// verifyReleaseSums checks a detached signature over the SHA256SUMS bytes.
// Returns nil when this build has no key configured (see releasePubKey).
func verifyReleaseSums(sums, sig []byte) error {
	return verifyWithKey(releasePubKey(), sums, sig)
}

// verifyWithKey is the checkable core: key in, verdict out, no embedding and no
// network, so the enforcing path can be tested on a build whose own key is empty.
func verifyWithKey(key ed25519.PublicKey, sums, sig []byte) error {
	if key == nil {
		return nil // unsigned build: fall back to checksum-only
	}
	if len(sig) != ed25519.SignatureSize {
		return errUnsignedRelease
	}
	if !ed25519.Verify(key, sums, sig) {
		return errBadReleaseSignature
	}
	return nil
}

// releaseSigned reports whether this build enforces signatures — used to tell
// the user which guarantee they're actually getting.
func releaseSigned() bool { return releasePubKey() != nil }
