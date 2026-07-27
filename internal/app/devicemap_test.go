package app

import (
	"testing"

	"github.com/zahak/concord/internal/identity"
)

// A linked device is only recognised as a member if its device key can be
// resolved back to its account. That map used to be built solely from live
// traffic — serving someone's join, or decrypting a message they sent — and
// held in memory, so a restart erased it. A phone that had been quiet since
// then became a stranger: history sync answered its catch-up with an empty
// body, the caller read that as success, and it retried every twenty seconds
// for as long as you cared to wait. The roster already states the mapping in
// every leaf, so it is recoverable; this test is here because nothing noticed
// for ten days that it wasn't being recovered.
func TestLearnDeviceCertResolvesADeviceToItsAccount(t *testing.T) {
	acct, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cert, err := acct.IssueDeviceCert("Phone", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	cred := cert.Marshal()

	s := &Service{}
	// Before learning, the device key is nobody: this is the state a restart
	// leaves behind, and the state in which every membership check says no.
	if got := s.lookupDevice(cert.DevicePub); got != "" {
		t.Fatalf("unlearned device should resolve to nothing, got %q", got)
	}

	s.learnDeviceCert(cred)

	want := identity.FingerprintOf(acct.PublicKey())
	if got := s.lookupDevice(cert.DevicePub); got != want {
		t.Fatalf("device resolved to %q, want the account %q — a member check\nagainst this value is what decides whether the device can sync at all", got, want)
	}
}

// A forged cert must not be able to claim an account. learnDeviceCert verifies
// the account's signature before recording anything, so a cert issued by
// somebody else buys nothing.
func TestLearnDeviceCertRejectsAnUnsignedClaim(t *testing.T) {
	acct, _ := identity.Generate()
	attacker, _ := identity.Generate()
	cert, err := attacker.IssueDeviceCert("Impostor", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	// Point the cert at a victim account it was never signed by.
	cert.AccountPub = acct.PublicKey()
	cred := cert.Marshal()

	s := &Service{}
	s.learnDeviceCert(cred)
	if got := s.lookupDevice(cert.DevicePub); got != "" {
		t.Fatalf("a cert not signed by the account it names was accepted: %q", got)
	}
}
