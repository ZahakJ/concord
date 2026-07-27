package bridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

// The update path is pre-authorized code execution, so each way it can be
// attacked gets its own case: a forged manifest, a signature from the wrong
// key, and — the easy one to forget — a release with the signature simply
// removed.
func TestVerifyReleaseSums(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("abc123  concord-linux-amd64-v1.2.3\n")
	good := ed25519.Sign(priv, sums)

	if err := verifyWithKey(pub, sums, good); err != nil {
		t.Fatalf("genuine release rejected: %v", err)
	}
	// Manifest altered after signing: a different binary hash smuggled in.
	if err := verifyWithKey(pub, []byte("dead00  concord-linux-amd64-v1.2.3\n"), good); err != errBadReleaseSignature {
		t.Fatalf("tampered manifest accepted: %v", err)
	}
	// Correctly signed, but by somebody else's key.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if err := verifyWithKey(pub, sums, ed25519.Sign(other, sums)); err != errBadReleaseSignature {
		t.Fatalf("foreign signature accepted: %v", err)
	}
	// Signature stripped. Must NOT degrade to "unsigned is fine" — that would
	// make the whole mechanism opt-out for an attacker.
	if err := verifyWithKey(pub, sums, nil); err != errUnsignedRelease {
		t.Fatalf("missing signature accepted: %v", err)
	}
	// A build with no key keeps working the old way.
	if err := verifyWithKey(nil, sums, nil); err != nil {
		t.Fatalf("unsigned build should stay permissive: %v", err)
	}
}

// The committed key file must parse to a usable key once filled in, and be
// inert (not half-parsed) while it's still just comments.
func TestReleasePubKeyFile(t *testing.T) {
	if k := releasePubKey(); k != nil && len(k) != ed25519.PublicKeySize {
		t.Fatalf("embedded key has wrong size: %d", len(k))
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	saved := releasePubKeyFile
	t.Cleanup(func() { releasePubKeyFile = saved })

	releasePubKeyFile = "# a comment\n\n" + hex.EncodeToString(pub) + "\n"
	got := releasePubKey()
	if got == nil || !strings.EqualFold(hex.EncodeToString(got), hex.EncodeToString(pub)) {
		t.Fatalf("key file with comments did not parse")
	}
	if !releaseSigned() {
		t.Fatal("releaseSigned() should be true once a key is present")
	}
	releasePubKeyFile = "# only comments\n"
	if releasePubKey() != nil || releaseSigned() {
		t.Fatal("comment-only key file should read as no key")
	}
}
