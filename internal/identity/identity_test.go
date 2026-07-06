package identity

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateProducesUsableKeypair(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	msg := []byte("concord")
	sig := id.Sign(msg)
	if !Verify(id.PublicKey(), msg, sig) {
		t.Fatal("signature did not verify with its own public key")
	}
	// Tampered message must not verify.
	if Verify(id.PublicKey(), []byte("concordx"), sig) {
		t.Fatal("signature verified against a different message")
	}
}

func TestSeedRoundTripIsDeterministic(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	seed := id.Seed()
	if len(seed) != SeedSize {
		t.Fatalf("seed size = %d, want %d", len(seed), SeedSize)
	}

	restored, err := FromSeed(seed)
	if err != nil {
		t.Fatalf("FromSeed: %v", err)
	}
	if !bytes.Equal(id.PublicKey(), restored.PublicKey()) {
		t.Fatal("public key changed after seed round-trip")
	}
	if id.Fingerprint() != restored.Fingerprint() {
		t.Fatal("fingerprint changed after seed round-trip")
	}
}

func TestFromSeedRejectsWrongLength(t *testing.T) {
	if _, err := FromSeed(make([]byte, 8)); err == nil {
		t.Fatal("expected error for short seed")
	}
}

func TestSeedIsACopy(t *testing.T) {
	id, _ := Generate()
	seed := id.Seed()
	for i := range seed {
		seed[i] = 0
	}
	// Zeroing the returned slice must not corrupt the live identity.
	if bytes.Equal(id.Seed(), make([]byte, SeedSize)) {
		t.Fatal("mutating returned seed corrupted the identity")
	}
}

func TestFingerprintStableAndDistinct(t *testing.T) {
	a, _ := Generate()
	b, _ := Generate()

	if a.Fingerprint() != a.Fingerprint() {
		t.Fatal("fingerprint is not stable across calls")
	}
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("distinct identities produced the same fingerprint")
	}
	// Grouped, readable format: blocks separated by spaces.
	if !strings.Contains(a.Fingerprint(), " ") {
		t.Fatalf("fingerprint not grouped: %q", a.Fingerprint())
	}
	if FingerprintOf(a.PublicKey()) != a.Fingerprint() {
		t.Fatal("FingerprintOf(pub) disagrees with Identity.Fingerprint")
	}
}
