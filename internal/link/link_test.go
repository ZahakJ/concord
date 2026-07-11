package link

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A realistic issuer: valid peer ID, LAN + QUIC addrs, one rendezvous node
// (whose derived /p2p-circuit addr should be elided from the wire).
const testPeerID = "12D3KooWCzonwxmETSLwgDY9JAe9WczUHssnYTrSpCqkp6UXZg1q"

func realisticOffer(t *testing.T) *Offer {
	t.Helper()
	boot := "/dns/concord-rdv.example.dev/tcp/4001/p2p/" + testPeerID
	o, err := NewOffer(testPeerID, []string{
		"/ip4/192.168.1.23/tcp/4001",
		"/ip4/192.168.1.23/udp/4001/quic-v1",
		boot + "/p2p-circuit",
	})
	if err != nil {
		t.Fatal(err)
	}
	o.Bootstrap = []string{boot}
	return o
}

// TestOfferCompactFormat pins the new format's properties: it round-trips
// (including the re-derived circuit addr), it's a fraction of the legacy JSON
// size, and legacy codes still decode.
func TestOfferCompactFormat(t *testing.T) {
	o := realisticOffer(t)

	code := o.Encode()
	if !strings.HasPrefix(code, codePrefix) {
		t.Fatalf("expected %s prefix, got %q", codePrefix, code[:8])
	}
	if len(code) >= 350 {
		t.Fatalf("compact offer code should be <350 chars, got %d", len(code))
	}

	got, err := DecodeOffer(code)
	if err != nil {
		t.Fatalf("DecodeOffer: %v", err)
	}
	if string(got.Secret) != string(o.Secret) || got.PeerID != o.PeerID || got.CreatedAt != o.CreatedAt {
		t.Fatalf("offer did not round-trip: %+v", got)
	}
	for _, want := range o.Addrs { // incl. the elided-then-restored circuit
		found := false
		for _, a := range got.Addrs {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("addr %q lost in round-trip (got %v)", want, got.Addrs)
		}
	}
	if len(got.Bootstrap) != 1 || got.Bootstrap[0] != o.Bootstrap[0] {
		t.Fatalf("bootstrap did not round-trip: %v", got.Bootstrap)
	}

	// Legacy JSON+base64url codes (pre-CL1 clients) must still decode.
	legacyJSON, _ := json.Marshal(o)
	legacy := base64.RawURLEncoding.EncodeToString(legacyJSON)
	if _, err := DecodeOffer(legacy); err != nil {
		t.Fatalf("legacy code must decode: %v", err)
	}
	if len(code) >= len(legacy)/2 {
		t.Fatalf("compact code (%d chars) should be under half the legacy size (%d chars)",
			len(code), len(legacy))
	}
	t.Logf("offer code: legacy %d chars → compact %d chars", len(legacy), len(code))
}

func TestOfferRoundTrip(t *testing.T) {
	o, err := NewOffer("12D3KooWabc", []string{"/ip4/10.0.0.1/tcp/4001", "/dns/relay/tcp/4001/p2p-circuit"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeOffer(o.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if got.PeerID != o.PeerID || len(got.Addrs) != 2 || len(got.Secret) != SecretSize {
		t.Fatalf("offer did not round-trip: %+v", got)
	}
}

func TestExpiredOfferRejected(t *testing.T) {
	o, _ := NewOffer("p", nil)
	o.CreatedAt = time.Now().Add(-5 * time.Minute).Unix()
	if _, err := DecodeOffer(o.Encode()); err == nil {
		t.Fatal("an expired offer must be rejected")
	}
}

func TestMutualProof(t *testing.T) {
	o, _ := NewOffer("p", nil)
	joinerNonce, _ := Nonce()
	issuerNonce, _ := Nonce()

	// Each side proves knowledge of the secret for its own role.
	joinerProof := Proof(o.Secret, RoleJoiner, joinerNonce)
	issuerProof := Proof(o.Secret, RoleIssuer, issuerNonce)

	if !VerifyProof(o.Secret, RoleJoiner, joinerNonce, joinerProof) {
		t.Fatal("valid joiner proof should verify")
	}
	if !VerifyProof(o.Secret, RoleIssuer, issuerNonce, issuerProof) {
		t.Fatal("valid issuer proof should verify")
	}
	// A proof for one role must not verify as the other (no cross-role replay).
	if VerifyProof(o.Secret, RoleIssuer, joinerNonce, joinerProof) {
		t.Fatal("joiner proof must not verify as an issuer proof")
	}
	// A wrong secret fails.
	wrong := make([]byte, SecretSize)
	wrong[0] = 1
	if VerifyProof(wrong, RoleJoiner, joinerNonce, joinerProof) {
		t.Fatal("proof under the wrong secret must fail")
	}
}
