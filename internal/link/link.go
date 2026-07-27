// Package link holds the device-linking handshake primitives for Concord's
// multi-device support (Phase 4). When a user adds a second device to their
// account, the already-unlocked device (the "issuer") displays a short-lived
// QR/code carrying a random linking secret plus how to reach it; the new device
// (the "joiner") scans it and dials the issuer over the /concord/link/1.0.0
// stream. Both sides prove knowledge of the secret with an HMAC challenge before
// any account material moves.
//
// This package has no network dependencies (crypto + pure encoding only) so
// the handshake can be unit-tested without a network. The stream wiring and the
// actual account-seed transfer live in the net/app layers and are gated on the
// multi-device migration; these are the primitives they build on.
package link

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zahak/concord/internal/compactcode"
)

// Protocol is the libp2p stream protocol ID for device linking.
const Protocol = "/concord/link/1.0.0"

// SecretSize is the linking secret length (256-bit, single-use).
const SecretSize = 32

// offerTTL bounds how long a displayed code is valid — a linking window should
// be short so a leaked/photographed code can't be replayed later.
const offerTTL = 2 * time.Minute

// Offer is what the issuer encodes into the QR/code shown to the user. The
// secret authenticates the channel; PeerID + Addrs let the joiner dial the
// issuer directly (LAN first, relay fallback). CreatedAt bounds the validity
// window.
type Offer struct {
	Secret    []byte   `json:"s"`           // 32-byte linking secret
	PeerID    string   `json:"p"`           // issuer libp2p PeerID
	Addrs     []string `json:"a"`           // issuer dialable multiaddrs
	Bootstrap []string `json:"b,omitempty"` // rendezvous nodes (for relay dial + adoption)
	CreatedAt int64    `json:"t"`           // unix seconds
}

// NewOffer builds a fresh single-use offer for the issuer's address info.
func NewOffer(peerID string, addrs []string) (*Offer, error) {
	secret := make([]byte, SecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &Offer{Secret: secret, PeerID: peerID, Addrs: addrs, CreatedAt: time.Now().Unix()}, nil
}

// codePrefix marks the compact binary format ("Concord Link v1"). A legacy
// code is base64url(JSON) whose payload must start with '{' — encoded "ey…" —
// so the prefix can never collide with one.
const codePrefix = "CL1"

// Encode renders an offer as a compact prefixed base64url string for a QR
// code: fixed-size secret, binary peer ID and multiaddrs, and relay circuit
// addresses elided (re-derived from the carried bootstrap list on decode).
// Roughly a third the size of the old JSON encoding.
func (o *Offer) Encode() string {
	// Elide against the bootstrap list as encoded, not as given: an entry the
	// cap drops can't be an index the decoder resolves.
	boot := compactcode.DedupeCap(o.Bootstrap, compactcode.MaxAddrs)
	kept, circuits := compactcode.ElideCircuits(o.Addrs, boot)
	addrs := compactcode.DedupeCap(compactcode.RankAddrs(kept), compactcode.MaxAddrs)
	b := make([]byte, 0, 256)
	b = append(b, o.Secret...)
	b = compactcode.AppendPeerID(b, o.PeerID)
	b = binary.AppendUvarint(b, uint64(o.CreatedAt))
	b = compactcode.AppendAddrs(b, addrs)
	b = compactcode.AppendAddrs(b, boot)
	b = binary.AppendUvarint(b, circuits)
	return codePrefix + base64.RawURLEncoding.EncodeToString(b)
}

// DecodeOffer parses a scanned code, rejecting a malformed, mis-sized, or
// expired offer. Both the compact "CL1…" format and the legacy base64url JSON
// format decode (new clients read old codes; the reverse isn't supported —
// codes live for two minutes, so mixed-version linking just means "upgrade
// the older device first").
func DecodeOffer(code string) (*Offer, error) {
	if rest, ok := strings.CutPrefix(code, codePrefix); ok {
		return decodeOfferV1(rest)
	}
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return nil, errors.New("link: bad code encoding")
	}
	var o Offer
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, errors.New("link: bad code")
	}
	return validateOffer(&o)
}

func decodeOfferV1(s string) (*Offer, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("link: bad code encoding")
	}
	r := compactcode.NewReader(raw)
	o := &Offer{}
	o.Secret = r.Take(SecretSize)
	o.PeerID = r.PeerID()
	o.CreatedAt = int64(r.Uvarint())
	o.Addrs = r.Addrs()
	o.Bootstrap = r.Addrs()
	// The circuit mask was appended after the first CL1 codes shipped; those
	// end here and meant "a circuit for every rendezvous".
	circuits := compactcode.AllCircuits
	if r.More() {
		circuits = r.Uvarint()
	}
	if r.Err() != nil {
		return nil, errors.New("link: bad code")
	}
	o.Addrs = compactcode.RestoreCircuits(o.Addrs, o.Bootstrap, circuits)
	return validateOffer(o)
}

func validateOffer(o *Offer) (*Offer, error) {
	if len(o.Secret) != SecretSize || o.PeerID == "" {
		return nil, errors.New("link: incomplete code")
	}
	if o.Expired() {
		return nil, errors.New("link: code expired")
	}
	return o, nil
}

// Expired reports whether the offer's validity window has passed.
func (o *Offer) Expired() bool {
	return time.Since(time.Unix(o.CreatedAt, 0)) > offerTTL
}

// Proof is an HMAC over a role label + a per-session nonce, keyed by the linking
// secret. Each side sends its own (with a distinct role) and verifies the
// other's, so possession of the secret is proven mutually and neither proof can
// be replayed as the other party's.
func Proof(secret []byte, role string, nonce []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(role))
	m.Write([]byte{0})
	m.Write(nonce)
	return m.Sum(nil)
}

// Role labels, kept distinct so an issuer proof can never be replayed as a
// joiner proof or vice-versa.
const (
	RoleIssuer = "concord-link-issuer"
	RoleJoiner = "concord-link-joiner"
)

// VerifyProof constant-time-checks a proof for role over nonce.
func VerifyProof(secret []byte, role string, nonce, got []byte) bool {
	want := Proof(secret, role, nonce)
	return subtle.ConstantTimeCompare(want, got) == 1
}

// Nonce returns a fresh 32-byte challenge nonce.
func Nonce() ([]byte, error) {
	n := make([]byte, 32)
	_, err := rand.Read(n)
	return n, err
}
