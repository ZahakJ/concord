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
// The account key (priv/pub) is the mnemonic-backed root: it *is* the account,
// stable across every device the user links, and what fingerprints/safety
// numbers are computed from. The device key is per-install (random, never
// leaves the device): it is what will drive this device's libp2p PeerID and MLS
// leaf once multi-device linking is enabled, so a phone and desktop under one
// account don't collide. Until that migration is switched on, the device key is
// carried but unused — the network/MLS layers still derive from the account
// seed, so existing single-device behavior is unchanged.
//
// The private material never leaves the process except through the encrypted
// keystore (see keystore.go).
type Identity struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey

	// deviceSeed is a 32-byte per-device secret. nil on a legacy identity loaded
	// from a v1 keystore until it is upgraded (LoadOrCreate generates + persists
	// one on first unlock).
	deviceSeed []byte
}

// Generate creates a brand-new random identity, including a fresh device seed.
func Generate() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	dseed := make([]byte, SeedSize)
	if _, err := rand.Read(dseed); err != nil {
		return nil, fmt.Errorf("generate device seed: %w", err)
	}
	return &Identity{priv: priv, pub: pub, deviceSeed: dseed}, nil
}

// DeviceSeed returns this device's 32-byte seed (secret), or nil on a
// not-yet-upgraded legacy identity.
func (id *Identity) DeviceSeed() []byte {
	if id.deviceSeed == nil {
		return nil
	}
	out := make([]byte, len(id.deviceSeed))
	copy(out, id.deviceSeed)
	return out
}

// ensureDeviceSeed generates a device seed if one isn't present, returning
// whether it created a new one (so callers know to persist the upgrade).
func (id *Identity) ensureDeviceSeed() (bool, error) {
	if id.deviceSeed != nil {
		return false, nil
	}
	dseed := make([]byte, SeedSize)
	if _, err := rand.Read(dseed); err != nil {
		return false, fmt.Errorf("generate device seed: %w", err)
	}
	id.deviceSeed = dseed
	return true, nil
}

// DeviceKey returns the Ed25519 private key derived from the device seed, or nil
// if the device seed hasn't been set. Used (once multi-device is enabled) for
// this device's libp2p host key and MLS signing key.
func (id *Identity) DeviceKey() ed25519.PrivateKey {
	if id.deviceSeed == nil {
		return nil
	}
	return ed25519.NewKeyFromSeed(id.deviceSeed)
}

// DevicePublicKey returns the device key's public half, or nil if unset.
func (id *Identity) DevicePublicKey() ed25519.PublicKey {
	if id.deviceSeed == nil {
		return nil
	}
	pk, _ := ed25519.NewKeyFromSeed(id.deviceSeed).Public().(ed25519.PublicKey)
	return pk
}

// FromSeeds reconstructs an identity from an account seed and a device seed —
// how a linked device rebuilds its identity after the linking handshake hands it
// the account seed (it keeps its own device seed). deviceSeed may be nil to
// leave the device key unset.
func FromSeeds(accountSeed, deviceSeed []byte) (*Identity, error) {
	id, err := FromSeed(accountSeed)
	if err != nil {
		return nil, err
	}
	if deviceSeed != nil {
		if len(deviceSeed) != SeedSize {
			return nil, fmt.Errorf("identity: device seed must be %d bytes, got %d", SeedSize, len(deviceSeed))
		}
		id.deviceSeed = append([]byte(nil), deviceSeed...)
	}
	return id, nil
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
