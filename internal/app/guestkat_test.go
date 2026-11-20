package app

import (
	"bytes"
	"crypto/ecdh"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Known-answer vectors for the guest leg and the booking seal, shared with the
// guest page's JS test (which reads testdata/guest-kat.json). Go regenerates
// the file with CONCORD_WRITE_KAT=1 and otherwise asserts it reproduces every
// byte, so the two implementations cannot drift apart silently.
//
// Encodings: fields named *_hex are lowercase hex; S, linkId, gn, hn, gpk, hpk,
// epk, spk, the records' "c" and "sealed" are base64url without padding, as
// they appear in the link and on the wire. Private keys are given three ways:
// the raw scalar (hex), a JWK "d" (base64url), and PKCS#8 DER (hex) for
// WebCrypto's importKey.

type guestKATRecord struct {
	Dir      string `json:"dir"` // "g2h" | "h2g"
	Counter  uint64 `json:"counter"`
	NonceHex string `json:"nonce_hex"`
	// Plaintext before padding, and the padded bytes' length.
	Plaintext string `json:"plaintext"`
	PaddedLen int    `json:"padded_len"`
	C         string `json:"c"`    // base64url(ciphertext ‖ tag)
	Line      string `json:"line"` // the record exactly as framed, without '\n'
}

type guestKAT struct {
	Comment    string `json:"comment"`
	S          string `json:"S"`
	LinkID     string `json:"linkId"`
	KHex       string `json:"K_hex"`
	HostPeerID string `json:"hostPeerId"`
	Gn         string `json:"gn"`
	Hn         string `json:"hn"`

	GskHex   string `json:"gsk_hex"`
	GskD     string `json:"gsk_jwk_d"`
	GskPKCS8 string `json:"gsk_pkcs8_hex"`
	Gpk      string `json:"gpk"`
	HskHex   string `json:"hsk_hex"`
	HskD     string `json:"hsk_jwk_d"`
	HskPKCS8 string `json:"hsk_pkcs8_hex"`
	Hpk      string `json:"hpk"`

	ZHex   string `json:"Z_hex"`
	G2HHex string `json:"k_g2h_hex"`
	H2GHex string `json:"k_h2g_hex"`

	Records []guestKATRecord `json:"records"`

	Booking struct {
		EskHex    string `json:"esk_hex"`
		EskD      string `json:"esk_jwk_d"`
		EskPKCS8  string `json:"esk_pkcs8_hex"`
		Epk       string `json:"epk"`
		SskHex    string `json:"ssk_hex"`
		Spk       string `json:"spk"`
		ZHex      string `json:"Z_hex"`
		KeyHex    string `json:"key_hex"`
		NonceHex  string `json:"nonce_hex"`
		Plaintext string `json:"plaintext"`
		Sealed    string `json:"sealed"` // base64url(nonce ‖ ciphertext ‖ tag)
	} `json:"booking"`
}

const guestKATPath = "testdata/guest-kat.json"

func katScalar(label string) *ecdh.PrivateKey {
	sum := sha256.Sum256([]byte(label))
	k, err := ecdh.P256().NewPrivateKey(sum[:])
	if err != nil {
		panic(err)
	}
	return k
}

func katPKCS8(k *ecdh.PrivateKey) string {
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(der)
}

func seq(start byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = start + byte(i)
	}
	return b
}

// buildGuestKAT computes every vector from fixed inputs with the production
// functions.
func buildGuestKAT(t *testing.T) guestKAT {
	t.Helper()
	var v guestKAT
	v.Comment = "Concord guest leg v2 and booking seal v1 known answers. See docs/dev/guest-e2e.md and internal/app/guestkat_test.go for encodings."
	secret := seq(0, 32)
	gn, hn := seq(0x10, 16), seq(0x20, 16)
	gsk, hsk := katScalar("concord kat guest ephemeral"), katScalar("concord kat host ephemeral")
	gpk, hpk := gsk.PublicKey().Bytes(), hsk.PublicKey().Bytes()
	v.S = b64url.EncodeToString(secret)
	v.LinkID = guestLinkID(secret)
	k := guestLinkKey(secret)
	v.KHex = hex.EncodeToString(k)
	v.HostPeerID = "12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN"
	v.Gn, v.Hn = b64url.EncodeToString(gn), b64url.EncodeToString(hn)
	v.GskHex, v.GskD, v.GskPKCS8 = hex.EncodeToString(gsk.Bytes()), b64url.EncodeToString(gsk.Bytes()), katPKCS8(gsk)
	v.HskHex, v.HskD, v.HskPKCS8 = hex.EncodeToString(hsk.Bytes()), b64url.EncodeToString(hsk.Bytes()), katPKCS8(hsk)
	v.Gpk, v.Hpk = b64url.EncodeToString(gpk), b64url.EncodeToString(hpk)
	z, err := gsk.ECDH(hsk.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	z2, _ := hsk.ECDH(gsk.PublicKey())
	if !bytes.Equal(z, z2) {
		t.Fatal("ECDH disagrees with itself")
	}
	v.ZHex = hex.EncodeToString(z)
	g2h, h2g := guestSessionKeys(k, z, v.LinkID, v.HostPeerID, gn, hn, gpk, hpk)
	v.G2HHex, v.H2GHex = hex.EncodeToString(g2h), hex.EncodeToString(h2g)

	add := func(dir string, c *guestCipher, pt string) {
		ctr := c.ctr
		line := c.seal([]byte(pt))
		var f struct{ C string }
		_ = json.Unmarshal(line, &f)
		v.Records = append(v.Records, guestKATRecord{
			Dir: dir, Counter: ctr, NonceHex: hex.EncodeToString(guestNonce(ctr)),
			Plaintext: pt, PaddedLen: len(padGuestPlaintext([]byte(pt))), C: f.C, Line: string(line),
		})
	}
	tx := newGuestCipher(g2h)
	add("g2h", tx, `{"type":"join","name":"Ada","gid":"Q2FzZUZpbGVHaWQwMTIz"}`)
	add("g2h", tx, `{"type":"msg","content":"hello"}`)
	rx := newGuestCipher(h2g)
	add("h2g", rx, `{"type":"welcome","meeting":"Standup","name":"Ada","channel":"ch1"}`)
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'x'
	}
	add("h2g", rx, `{"type":"msg","content":"`+string(long)+`"}`)

	esk, ssk := katScalar("concord kat booking visitor"), katScalar("concord kat booking host")
	epk, spk := esk.PublicKey().Bytes(), ssk.PublicKey().Bytes()
	bz, _ := ssk.ECDH(esk.PublicKey())
	key := bookingSealKey(bz, epk, spk)
	nonce := seq(0x30, 12)
	pt := `{"meetingURL":"https://gw.example/guest#h=12D3KooW&k=AAAA","ics":"BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"}`
	b := &v.Booking
	b.EskHex, b.EskD, b.EskPKCS8 = hex.EncodeToString(esk.Bytes()), b64url.EncodeToString(esk.Bytes()), katPKCS8(esk)
	b.Epk = b64url.EncodeToString(epk)
	b.SskHex = hex.EncodeToString(ssk.Bytes())
	b.Spk = b64url.EncodeToString(spk)
	b.ZHex, b.KeyHex, b.NonceHex = hex.EncodeToString(bz), hex.EncodeToString(key), hex.EncodeToString(nonce)
	b.Plaintext = pt
	b.Sealed = b64url.EncodeToString(bookingSeal(key, nonce, []byte(pt)))
	return v
}

func TestGuestKnownAnswers(t *testing.T) {
	got := buildGuestKAT(t)
	blob, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	blob = append(blob, '\n')
	if os.Getenv("CONCORD_WRITE_KAT") == "1" {
		if err := os.MkdirAll(filepath.Dir(guestKATPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(guestKATPath, blob, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(guestKATPath)
	if err != nil {
		t.Fatalf("read %s: %v", guestKATPath, err)
	}
	if !bytes.Equal(want, blob) {
		t.Fatalf("%s does not match what the code derives; the guest page is pinned to the file, so a change here breaks it", guestKATPath)
	}

	// And the vectors open the way a receiver opens them: the counter is the
	// nonce, padding is spaces, and the peer's key (not the sender's) works.
	var v guestKAT
	if err := json.Unmarshal(want, &v); err != nil {
		t.Fatal(err)
	}
	g2h, _ := hex.DecodeString(v.G2HHex)
	h2g, _ := hex.DecodeString(v.H2GHex)
	rx := map[string]*guestCipher{"g2h": newGuestCipher(g2h), "h2g": newGuestCipher(h2g)}
	for _, r := range v.Records {
		ct, _ := b64url.DecodeString(r.C)
		if len(ct) != r.PaddedLen+16 || r.PaddedLen%guestPadBlock != 0 {
			t.Errorf("record %s/%d is %d bytes, padded %d", r.Dir, r.Counter, len(ct), r.PaddedLen)
		}
		pt, err := rx[r.Dir].open(r.C)
		if err != nil {
			t.Fatalf("record %s/%d does not open: %v", r.Dir, r.Counter, err)
		}
		if string(bytes.TrimRight(pt, " ")) != r.Plaintext {
			t.Errorf("record %s/%d opened to %q", r.Dir, r.Counter, pt)
		}
	}
}

// TestGuestCipherStrictCounters covers the receiver's side of a replayed,
// skipped or reflected record without any network: each fails to open.
func TestGuestCipherStrictCounters(t *testing.T) {
	g2h, h2g := guestSessionKeys(seq(1, 32), seq(2, 32), "id", "peer", seq(3, 16), seq(4, 16), seq(5, 65), seq(6, 65))
	tx, rx := newGuestCipher(g2h), newGuestCipher(g2h)
	body := func(line []byte) string {
		var f struct{ C string }
		_ = json.Unmarshal(line, &f)
		return f.C
	}
	r0 := body(tx.seal([]byte("zero")))
	if _, err := rx.open(r0); err != nil {
		t.Fatalf("in-order record refused: %v", err)
	}
	if _, err := rx.open(r0); err == nil {
		t.Fatal("a replayed record opened")
	}
	_ = tx.seal([]byte("one, dropped"))
	if _, err := rx.open(body(tx.seal([]byte("two")))); err == nil {
		t.Fatal("a record after a skipped counter opened")
	}
	// Reflection: a guest record sent back to the guest is under the other
	// direction's key.
	back := newGuestCipher(h2g)
	if _, err := back.open(body(newGuestCipher(g2h).seal([]byte("mirror")))); err == nil {
		t.Fatal("a reflected record opened under the other direction's key")
	}
}
