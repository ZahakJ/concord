// Package identity is layer 1 of Concord: it owns a peer's cryptographic
// identity. An identity is a single Ed25519 keypair that doubles as the user's
// account — there is no server-side signup. Everything else in Concord
// (the libp2p PeerID, group membership, message signatures) is derived from
// this keypair.
//
// This package is deliberately pure: it depends only on the standard library
// and golang.org/x/crypto, never on libp2p or the network layer, so that the
// derivation rules can be unit-tested in isolation.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
)

// SeedSize is the length in bytes of the Ed25519 seed we persist. The full
// private key is deterministically regenerated from it via ed25519.NewKeyFromSeed.
const SeedSize = ed25519.SeedSize // 32

// Identity is a peer's long-term cryptographic identity.
//
// The private key never leaves the process except through the encrypted
// keystore (see keystore.go). The public key is safe to share and is what
// other peers use to address and verify this peer.
type Identity struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

// Generate creates a brand-new random identity.
func Generate() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	return &Identity{priv: priv, pub: pub}, nil
}

// FromSeed reconstructs an identity from a 32-byte seed. This is how the
// keystore rehydrates an identity after decrypting it at rest.
func FromSeed(seed []byte) (*Identity, error) {
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("identity: seed must be %d bytes, got %d", SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("identity: unexpected public key type")
	}
	return &Identity{priv: priv, pub: pub}, nil
}

// Seed returns the 32-byte seed that uniquely determines this identity's
// private key. Callers must treat it as secret material.
func (id *Identity) Seed() []byte {
	// ed25519.PrivateKey is seed(32) || public(32); Seed() copies the first half.
	return id.priv.Seed()
}

// PublicKey returns a copy of the raw 32-byte Ed25519 public key. This is the
// stable, shareable component of the identity that the network layer turns
// into a libp2p PeerID.
func (id *Identity) PublicKey() ed25519.PublicKey {
	out := make(ed25519.PublicKey, len(id.pub))
	copy(out, id.pub)
	return out
}

// PrivateKey returns the underlying Ed25519 private key. Intended for the
// network layer (to construct a libp2p host) and signing; never serialize it
// directly — persist Seed() through the encrypted keystore instead.
func (id *Identity) PrivateKey() ed25519.PrivateKey {
	return id.priv
}

// Sign produces an Ed25519 signature over msg using this identity.
func (id *Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(id.priv, msg)
}

// Verify checks an Ed25519 signature produced by the peer owning pub.
func Verify(pub ed25519.PublicKey, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}

// fingerprintEncoding renders fingerprint bytes without padding, in uppercase,
// which keeps the safety number compact and easy to read aloud.
var fingerprintEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Fingerprint returns a stable, human-verifiable short identifier for this
// identity: a base32 rendering of the first 20 bytes of SHA-256(publicKey),
// grouped into blocks of four. Two peers can read these aloud (a "safety
// number") to confirm they hold each other's real key and defeat MITM during
// out-of-band contact exchange.
func (id *Identity) Fingerprint() string {
	return FingerprintOf(id.pub)
}

// FingerprintOf computes the fingerprint for an arbitrary public key, so the
// UI can display a contact's safety number for verification.
func FingerprintOf(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	raw := fingerprintEncoding.EncodeToString(sum[:20]) // 20 bytes -> 32 base32 chars
	return group(raw, 4)
}

// group inserts a space every n runes, producing "AB12 CD34 ..." style output.
func group(s string, n int) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && i%n == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
