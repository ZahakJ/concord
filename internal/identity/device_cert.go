package identity

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"errors"
)

// DeviceCert is an account attesting one of its devices: the account (root) key
// signs the device's public key, so any peer holding the account key (every
// group member does — it's the fingerprint/credential root) can verify that a
// device leaf genuinely belongs to that account. This is the primitive
// multi-device linking is built on: a phone and desktop under one account each
// carry a DeviceCert signed by the shared account key, and MLS leaves are
// admitted only when their cert verifies.
//
// The account key itself never has to be online for verification — the cert
// travels with the leaf. Only *issuing* a cert (linking a new device) needs the
// account key, which is why linking happens on an already-unlocked device.
type DeviceCert struct {
	AccountPub []byte `json:"account"` // Ed25519 account (root) pubkey
	DevicePub  []byte `json:"device"`  // Ed25519 device pubkey
	DeviceName string `json:"name"`    // user-facing label ("Pixel 7", "Desktop")
	IssuedAt   int64  `json:"issued"`  // unix seconds
	Sig        []byte `json:"sig"`     // account-key signature over signingBytes()
}

// certDomain namespaces the signature so a DeviceCert signature can never be
// confused with any other account-key signature (invites, governance, etc.).
var certDomain = []byte("concord-device-cert-v1")

// signingBytes is the canonical message the account key signs: a
// length-prefixed, order-fixed encoding so two peers always reconstruct the
// exact same bytes regardless of JSON field ordering.
func (c *DeviceCert) signingBytes() []byte {
	var b []byte
	put := func(p []byte) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(p)))
		b = append(b, n[:]...)
		b = append(b, p...)
	}
	put(certDomain)
	put(c.AccountPub)
	put(c.DevicePub)
	put([]byte(c.DeviceName))
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(c.IssuedAt))
	b = append(b, ts[:]...)
	return b
}

// IssueDeviceCert signs a certificate binding this identity's device key to its
// account key. The device seed must be set (see LoadOrCreate's upgrade path).
func (id *Identity) IssueDeviceCert(deviceName string, issuedAt int64) (*DeviceCert, error) {
	dpub := id.DevicePublicKey()
	if dpub == nil {
		return nil, errors.New("identity: device seed not set")
	}
	c := &DeviceCert{
		AccountPub: id.PublicKey(),
		DevicePub:  dpub,
		DeviceName: deviceName,
		IssuedAt:   issuedAt,
	}
	c.Sig = ed25519.Sign(id.priv, c.signingBytes())
	return c, nil
}

// IssueDeviceCertFor signs a cert for ANOTHER device's public key using this
// identity's account key — used by the linking device to admit a new device it
// is bringing into the account.
func (id *Identity) IssueDeviceCertFor(devicePub ed25519.PublicKey, deviceName string, issuedAt int64) *DeviceCert {
	c := &DeviceCert{
		AccountPub: id.PublicKey(),
		DevicePub:  append([]byte(nil), devicePub...),
		DeviceName: deviceName,
		IssuedAt:   issuedAt,
	}
	c.Sig = ed25519.Sign(id.priv, c.signingBytes())
	return c
}

// Verify checks the account-key signature over the cert. A valid cert proves the
// account (AccountPub) authorized the device (DevicePub).
func (c *DeviceCert) Verify() bool {
	if len(c.AccountPub) != ed25519.PublicKeySize || len(c.DevicePub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(c.AccountPub, c.signingBytes(), c.Sig)
}

// AccountFingerprint is the safety-number fingerprint of the certifying account
// — the stable identity a device leaf maps back to for roster/verification.
func (c *DeviceCert) AccountFingerprint() string {
	return FingerprintOf(c.AccountPub)
}

// Marshal/ParseDeviceCert serialize a cert for embedding in an MLS credential or
// the link handshake.
func (c *DeviceCert) Marshal() []byte {
	b, _ := json.Marshal(c)
	return b
}

// ParseDeviceCert decodes a cert. A legacy 32-byte raw credential (no cert) is
// signalled by ok=false so callers can fall back to treating the bytes as a
// bare account pubkey (account == device), preserving pre-multi-device compat.
func ParseDeviceCert(b []byte) (*DeviceCert, bool) {
	if len(b) == ed25519.PublicKeySize {
		return nil, false // legacy bare-account credential
	}
	var c DeviceCert
	if json.Unmarshal(b, &c) != nil || len(c.AccountPub) == 0 {
		return nil, false
	}
	return &c, true
}

// DeviceRevocation is an account withdrawing one of its own devices: the same
// account key that signed a DeviceCert signs, later, "this device is no longer
// mine". It is the counterpart to IssueDeviceCertFor and carries exactly the
// same authority — nobody but the account can produce one, and anybody holding
// the account key can check one.
//
// What it is NOT is a capability revocation. The bytes here are an assertion,
// and an assertion only binds a client that chooses to honor it. The real,
// cryptographic half of unlinking is removing that device's leaf from the MLS
// groups, which is what actually stops it decrypting anything sent afterwards.
// This record is what makes an HONEST client on the revoked device notice and
// erase itself, and what stops every other client from continuing to render that
// device as its owner. See the threat model in app/unlink.go.
type DeviceRevocation struct {
	AccountPub []byte `json:"account"` // Ed25519 account (root) pubkey
	DevicePub  []byte `json:"device"`  // the device being withdrawn
	RevokedAt  int64  `json:"revoked"` // unix seconds
	Sig        []byte `json:"sig"`     // account-key signature over signingBytes()
}

// revokeDomain namespaces the signature. Distinct from certDomain so a
// certificate signature can never be replayed as a revocation, or the reverse —
// which, given that one of the two erases a device, is not a subtle difference.
var revokeDomain = []byte("concord-device-revoke-v1")

func (r *DeviceRevocation) signingBytes() []byte {
	var b []byte
	put := func(p []byte) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(p)))
		b = append(b, n[:]...)
		b = append(b, p...)
	}
	put(revokeDomain)
	put(r.AccountPub)
	put(r.DevicePub)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(r.RevokedAt))
	b = append(b, ts[:]...)
	return b
}

// RevokeDevice signs a revocation for one of this account's device keys.
func (id *Identity) RevokeDevice(devicePub ed25519.PublicKey, revokedAt int64) *DeviceRevocation {
	r := &DeviceRevocation{
		AccountPub: id.PublicKey(),
		DevicePub:  append([]byte(nil), devicePub...),
		RevokedAt:  revokedAt,
	}
	r.Sig = ed25519.Sign(id.priv, r.signingBytes())
	return r
}

// Verify checks the account-key signature over the revocation.
func (r *DeviceRevocation) Verify() bool {
	if r == nil || len(r.AccountPub) != ed25519.PublicKeySize || len(r.DevicePub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(r.AccountPub, r.signingBytes(), r.Sig)
}
