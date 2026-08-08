package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/link"
)

// buildIssuerResponse mirrors what handleLinkRequest produces, without needing a
// running Service — so the joiner-side verification can be unit-tested.
func buildIssuerResponse(t *testing.T, issuer *identity.Identity, secret, joinerDevicePub []byte) *linkResponse {
	t.Helper()
	nonce, _ := link.Nonce()
	return &linkResponse{
		IssuerNonce: nonce,
		IssuerProof: link.Proof(secret, link.RoleIssuer, nonce),
		AccountSeed: issuer.Seed(),
		Cert:        issuer.IssueDeviceCertFor(joinerDevicePub, "Phone", time.Now().Unix()),
	}
}

func TestVerifyLinkResponseAcceptsValid(t *testing.T) {
	issuer, _ := identity.Generate()
	joiner, _ := identity.Generate()
	secret := make([]byte, link.SecretSize)
	secret[0] = 7

	resp := buildIssuerResponse(t, issuer, secret, joiner.DevicePublicKey())
	seed, err := verifyLinkResponse(secret, joiner.DevicePublicKey(), resp)
	if err != nil {
		t.Fatalf("valid response should verify: %v", err)
	}
	// The returned seed is the issuer's account seed — the joiner will rebuild
	// its identity from it plus its own device seed.
	if !bytes.Equal(seed, issuer.Seed()) {
		t.Fatal("returned seed should be the issuer account seed")
	}
	rebuilt, _ := identity.FromSeeds(seed, joiner.DeviceSeed())
	if rebuilt.Fingerprint() != issuer.Fingerprint() {
		t.Fatal("linked device should share the issuer's account fingerprint")
	}
}

func TestVerifyLinkResponseRejectsTampering(t *testing.T) {
	issuer, _ := identity.Generate()
	joiner, _ := identity.Generate()
	secret := make([]byte, link.SecretSize)

	// Wrong secret → issuer proof fails (MITM couldn't have known it).
	resp := buildIssuerResponse(t, issuer, secret, joiner.DevicePublicKey())
	wrong := make([]byte, link.SecretSize)
	wrong[0] = 1
	if _, err := verifyLinkResponse(wrong, joiner.DevicePublicKey(), resp); err == nil {
		t.Fatal("a wrong secret must fail the issuer proof")
	}

	// Cert for a different device key → rejected.
	other, _ := identity.Generate()
	resp2 := buildIssuerResponse(t, issuer, secret, other.DevicePublicKey())
	if _, err := verifyLinkResponse(secret, joiner.DevicePublicKey(), resp2); err == nil {
		t.Fatal("a cert for another device must be rejected")
	}

	// Account seed swapped so it no longer matches the cert's account → rejected.
	resp3 := buildIssuerResponse(t, issuer, secret, joiner.DevicePublicKey())
	resp3.AccountSeed = other.Seed()
	if _, err := verifyLinkResponse(secret, joiner.DevicePublicKey(), resp3); err == nil {
		t.Fatal("an account seed that doesn't match the cert must be rejected")
	}
}
