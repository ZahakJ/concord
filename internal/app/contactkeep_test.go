package app

import (
	"context"
	"testing"

	"github.com/zahak/concord/internal/identity"
)

// testFingerprint is a fingerprint of a real key, so nothing downstream can
// reject it as malformed.
func testFingerprint(t *testing.T) string {
	t.Helper()
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	return id.Fingerprint()
}

// TestPruneKeepsPeersWeStillTalkTo is the regression from the contacts-prune
// commits. The launch-time prune keeps only rows that pass knownContact, which
// is a test of RELATIONSHIP (verified / shares a guild / we messaged them
// first). Someone you have met, are still connected to and still re-dial every
// launch — but who removed you from the one server you shared — failed it, so
// their row was deleted while the connection was up: they dropped out of the
// contacts list mid-session, and verifying them afterwards failed with a raw
// store error, because verification is an UPDATE of the row that had just gone.
func TestPruneKeepsPeersWeStillTalkTo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startService(t, ctx)

	// Somebody we met and still re-dial on every launch: they are in peers.json,
	// which is only ever written for a peer we shared a guild with at the time.
	friendID, friendFpr := testPeerID(t), testFingerprint(t)
	if err := svc.store.RecordContact(friendID, friendFpr); err != nil {
		t.Fatalf("RecordContact: %v", err)
	}
	svc.peers.Remember(friendID, []string{"/ip4/198.51.100.7/tcp/4001"})

	// And a genuine stranger row of the kind the prune exists to clear.
	strangerID, strangerFpr := testPeerID(t), testFingerprint(t)
	if err := svc.store.RecordContact(strangerID, strangerFpr); err != nil {
		t.Fatalf("RecordContact: %v", err)
	}

	svc.pruneContacts()

	contacts, err := svc.Contacts()
	if err != nil {
		t.Fatalf("Contacts: %v", err)
	}
	got := map[string]bool{}
	for _, c := range contacts {
		got[c.Fingerprint] = true
	}
	if !got[friendFpr] {
		t.Error("the prune deleted a peer we are still re-dialling every launch")
	}
	if got[strangerFpr] {
		t.Error("the prune kept a stranger row it exists to clear")
	}
}

// TestVerifyFingerprintWithoutAContactRow: verifying is the user saying "I
// compared safety numbers with this person out of band". That must not depend on
// a contacts row happening to exist — and since recording became gated, for a
// guild member we have never had a direct connection with, it does not. The old
// code was a bare UPDATE, so it failed, and the failure reached the UI as the
// raw string "store: unknown fingerprint".
func TestVerifyFingerprintWithoutAContactRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startService(t, ctx)

	fpr := testFingerprint(t)
	if err := svc.VerifyFingerprint(fpr); err != nil {
		t.Fatalf("verifying someone with no contact row: %v", err)
	}
	if !svc.VerifiedFingerprints()[fpr] {
		t.Fatal("the verification did not stick")
	}
	// And it survives the prune, which never touches verified rows.
	svc.pruneContacts()
	if !svc.VerifiedFingerprints()[fpr] {
		t.Fatal("the prune undid a verification the user did by hand")
	}
}
