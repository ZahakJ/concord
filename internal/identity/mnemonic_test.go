package identity

import (
	"bytes"
	"testing"
)

func TestMnemonicRoundTrip(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	phrase, err := id.Mnemonic()
	if err != nil {
		t.Fatalf("Mnemonic: %v", err)
	}
	// 32 bytes entropy -> 24 words.
	if got := len(bytes.Fields([]byte(phrase))); got != 24 {
		t.Fatalf("phrase has %d words, want 24", got)
	}

	seed, err := SeedFromMnemonic(phrase)
	if err != nil {
		t.Fatalf("SeedFromMnemonic: %v", err)
	}
	if !bytes.Equal(seed, id.Seed()) {
		t.Fatal("decoded seed does not match original")
	}

	// A restored identity must have the SAME fingerprint (the whole point).
	restored, err := FromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Fingerprint() != id.Fingerprint() {
		t.Fatal("restored identity has a different fingerprint")
	}

	// Normalization: extra spaces / case tolerated.
	if _, err := SeedFromMnemonic("  " + phrase + "  "); err != nil {
		t.Fatalf("whitespace-padded phrase rejected: %v", err)
	}
	// A tampered phrase fails the checksum.
	if _, err := SeedFromMnemonic("abandon abandon abandon"); err == nil {
		t.Fatal("invalid phrase accepted")
	}
}
