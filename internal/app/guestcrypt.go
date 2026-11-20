package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// The guest leg's own encryption. See docs/dev/guest-e2e.md.
//
// A browser guest has no Concord keys, and its WebSocket ends at the
// rendezvous, which pipes it to this node. Everything inside that pipe after
// the opening exchange is sealed under keys that mix two things: K, derived
// from a secret S that lives only in the link's URL fragment (browsers never
// send a fragment to the server, and the guest page never puts S on the
// socket), and Z, an ephemeral P-256 Diffie-Hellman secret fresh for every
// visit. K proves the guest holds the link; Z means a link that leaks later
// does not open visits recorded earlier. The operator relaying the pipe sees a
// link id, two random nonces, two ephemeral public keys, and ciphertext in
// 256-byte buckets.
//
// The guest page (cmd/rendezvous/guestpage/index.html) implements the same
// derivations with WebCrypto, and testdata/guest-kat.json pins both to the
// same bytes; any change here must change there in the same commit.

const (
	guestSecretBytes = 32
	guestNonceBytes  = 16 // gn and hn, each
	// Plaintext is right-padded with spaces (JSON ignores trailing whitespace)
	// to a multiple of this, so a chat line's length does not show.
	guestPadBlock = 256

	guestInfoLinkID = "concord guest v2 link id"
	guestInfoKey    = "concord guest v2 link key"
	guestInfoG2H    = "concord guest v2 g2h"
	guestInfoH2G    = "concord guest v2 h2g"

	bookingInfoSeal = "concord booking v1 seal"
)

// b64url (attach.go) is base64.RawURLEncoding, the encoding of every byte
// string on the guest wire.

func guestHKDF(secret, salt []byte, info string, n int) []byte {
	out, err := hkdf.Key(sha256.New, secret, salt, info, n)
	if err != nil {
		panic(err) // only on n beyond 255*32, which no caller asks for
	}
	return out
}

// guestRandom reads n bytes from the CSPRNG. It fails rather than fall back to
// anything weaker: every caller is minting a secret, a nonce or a key, and a
// predictable one would be worse than refusing the visit.
func guestRandom(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("app: no randomness: %w", err)
	}
	return b, nil
}

// guestLinkID is the half of the link the host is told in clear, so it can
// find the link. It reveals nothing about S.
func guestLinkID(secret []byte) string {
	return b64url.EncodeToString(guestHKDF(secret, nil, guestInfoLinkID, 16))
}

// guestLinkKey is K, the link's long-term key. It never leaves either end.
func guestLinkKey(secret []byte) []byte {
	return guestHKDF(secret, nil, guestInfoKey, 32)
}

// guestEphemeral is one side's P-256 key for one visit, never stored.
func guestEphemeral() (*ecdh.PrivateKey, error) {
	return ecdh.P256().GenerateKey(rand.Reader)
}

// guestPublicKey parses a raw uncompressed P-256 point off the wire.
func guestPublicKey(raw []byte) (*ecdh.PublicKey, error) {
	if len(raw) != 65 || raw[0] != 4 {
		return nil, fmt.Errorf("app: not an uncompressed P-256 point")
	}
	return ecdh.P256().NewPublicKey(raw)
}

// guestSessionKeys derives one key per direction for one visit:
//
//	ikm  = K ‖ Z
//	salt = gn ‖ hn
//	ctx  = 0x00 "v2" 0x00 linkId 0x00 hostPeerId 0x00 b64url(gpk) 0x00 b64url(hpk)
//	k_g2h = HKDF(ikm, salt, "concord guest v2 g2h" ‖ ctx, 32)
//	k_h2g = HKDF(ikm, salt, "concord guest v2 h2g" ‖ ctx, 32)
//
// gn, hn and both ephemeral keys are fresh per visit, so no two visits share a
// key and a recorded visit cannot be replayed into a new one. The context binds
// the protocol version, the link and the host, so keys for one can never open
// records meant for another.
func guestSessionKeys(k, z []byte, linkID, hostPeer string, gn, hn, gpk, hpk []byte) (g2h, h2g []byte) {
	ikm := append(append([]byte{}, k...), z...)
	salt := append(append([]byte{}, gn...), hn...)
	ctx := "\x00v2\x00" + linkID + "\x00" + hostPeer + "\x00" + b64url.EncodeToString(gpk) + "\x00" + b64url.EncodeToString(hpk)
	return guestHKDF(ikm, salt, guestInfoG2H+ctx, 32), guestHKDF(ikm, salt, guestInfoH2G+ctx, 32)
}

// guestCipher seals or opens one direction of a visit. The nonce is a counter
// both ends keep: the stream is ordered, so the receiver demands exactly the
// next value and a dropped, replayed or reordered record fails to open.
type guestCipher struct {
	aead cipher.AEAD
	ctr  uint64
}

func newGuestCipher(key []byte) *guestCipher {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // key is always 32 bytes
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &guestCipher{aead: aead}
}

// guestNonce is 4 zero bytes then the 64-bit big-endian counter.
func guestNonce(ctr uint64) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n[4:], ctr)
	return n
}

// seal returns the record line (without its '\n') carrying plaintext.
func (c *guestCipher) seal(plaintext []byte) []byte {
	ct := c.aead.Seal(nil, guestNonce(c.ctr), padGuestPlaintext(plaintext), nil)
	c.ctr++
	return []byte(`{"type":"e","c":"` + b64url.EncodeToString(ct) + `"}`)
}

// open takes a record's base64 body and returns the padded plaintext. A record
// that fails to open does not advance the counter, but the session is over
// anyway: both ends close on the first bad record.
func (c *guestCipher) open(body string) ([]byte, error) {
	ct, err := b64url.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("app: guest record is not base64")
	}
	pt, err := c.aead.Open(nil, guestNonce(c.ctr), ct, nil)
	if err != nil {
		return nil, fmt.Errorf("app: guest record failed to open")
	}
	c.ctr++
	return pt, nil
}

// padGuestPlaintext right-pads with spaces to the next multiple of
// guestPadBlock; a plaintext already on a multiple is left as it is, and an
// empty one becomes one block.
func padGuestPlaintext(p []byte) []byte {
	n := (len(p) + guestPadBlock - 1) / guestPadBlock * guestPadBlock
	if n == 0 {
		n = guestPadBlock
	}
	out := make([]byte, n)
	copy(out, p)
	for i := len(p); i < n; i++ {
		out[i] = ' '
	}
	return out
}

// bookingSealKey is the key for the one sealed booking answer:
// HKDF(ECDH(esk, spk), salt = epk ‖ spk, "concord booking v1 seal", 32).
func bookingSealKey(z, epk, spk []byte) []byte {
	salt := append(append([]byte{}, epk...), spk...)
	return guestHKDF(z, salt, bookingInfoSeal, 32)
}

// bookingSeal returns nonce(12) ‖ AES-256-GCM(key, nonce, plaintext).
func bookingSeal(key, nonce, plaintext []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead.Seal(append([]byte{}, nonce...), nonce, plaintext, nil)
}

// bookingOpen is bookingSeal's inverse (the page's half, used by tests).
func bookingOpen(key, sealed []byte) ([]byte, error) {
	if len(sealed) < 12+16 {
		return nil, fmt.Errorf("app: sealed booking too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, sealed[:12], sealed[12:], nil)
}
