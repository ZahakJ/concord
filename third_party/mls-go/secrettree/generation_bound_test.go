package secrettree

import (
	"errors"
	"testing"
	"time"

	"github.com/thomas-vilte/mls-go/ciphersuite"
)

// A frame that names a generation far ahead of the leaf is refused before
// any key is derived: 0xFFFFFFFF used to mean four billion HKDF steps under
// the group's lock. The leaf is left as it was and still decrypts.
func TestRatchetRefusesAGenerationFarAhead(t *testing.T) {
	encSecret, _ := ciphersuite.NewSecretRandom(32)
	tree, _ := NewTree(encSecret, 4, ciphersuite.MLS128DHKEMP256)
	leaf, _ := tree.LeafForIndex(0)
	start := time.Now()
	for _, gen := range []uint32{0xFFFFFFFF, MaxGenerationJump + 1} {
		if _, err := leaf.ApplicationKey(gen); !errors.Is(err, ErrGenerationTooFar) {
			t.Fatalf("ApplicationKey(%d): %v, want ErrGenerationTooFar", gen, err)
		}
		if _, err := leaf.HandshakeNonce(gen); !errors.Is(err, ErrGenerationTooFar) {
			t.Fatalf("HandshakeNonce(%d): %v, want ErrGenerationTooFar", gen, err)
		}
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("refusing took %v", d)
	}
	if _, err := leaf.ApplicationKey(3); err != nil {
		t.Fatalf("the leaf no longer derives: %v", err)
	}
	if _, err := leaf.ApplicationKey(3 + 1000); err != nil {
		t.Fatalf("a jump inside the bound is refused: %v", err)
	}
}
