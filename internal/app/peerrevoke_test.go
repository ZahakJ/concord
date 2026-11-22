package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
)

// docs/dev/owner-rejoin.md, open item 1: a device its account unlinked was
// still the account to every OTHER member, so a moderator's heal door let it
// back in and the device mapping placed it as the account (which is what the
// owner chain is served on). The account's devices now hand their signed
// revocations to the members they greet; the member keeps one only from the
// account that signed it, and then refuses the device at its own door.
func TestAMembersDoorRefusesADeviceTheAccountUnlinked(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	g := w.guildOf(t, w.bob, w.phone, w.alice, w.carol) // Bob owns it; Alice's door is the one under test
	// Alice is a moderator: Manage members makes her device a door.
	rid, err := w.bob.UpsertRole(g.ID, "", "Doorkeeper", "", PermManageMembers, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.bob.AssignRole(g.ID, w.alice.Fingerprint(), rid, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 60*time.Second, func() bool { return w.alice.hasPerm(g.ID, PermManageMembers) }, "Alice's role to reach her")

	kp, err := w.phone.mls.KeyPackage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cred := append([]byte(nil), w.phone.myCredential...)
	pid := w.phone.host.PeerID()
	alice, bob, carol := w.alice.host.PeerID(), w.bob.host.PeerID(), w.carol.host.PeerID()
	waitFor(t, 30*time.Second, func() bool { return w.alice.presence(pid).Fingerprint == w.bob.id.Fingerprint() }, "Alice to place Bob's phone as Bob")
	_ = w.phone.Close()

	if err := w.bob.UnlinkDevice(w.phoneKey); err != nil {
		t.Fatalf("UnlinkDevice: %v", err)
	}
	recs := w.bob.revocations()
	if len(recs) != 1 {
		t.Fatalf("Bob holds %d revocations, want 1", len(recs))
	}

	// Carried by somebody else, Bob's genuine revocation is not taken: a peer
	// delivers only its own account's word.
	w.alice.adoptPeerRevocations(carol, recs)
	if w.alice.leafRevoked(cred) {
		t.Fatal("Alice took Bob's revocation from Carol")
	}
	// Nor can an account withdraw a device of another account by naming its
	// key: Carol's signature over Bob's phone key revokes nothing of Bob's.
	forged := w.carol.id.RevokeDevice(recs[0].DevicePub, time.Now().Unix())
	w.alice.adoptPeerRevocations(carol, []*identity.DeviceRevocation{forged})
	if w.alice.leafRevoked(cred) {
		t.Fatal("Carol's revocation of Bob's phone key counted against Bob's phone")
	}

	// Bob's desktop answers Alice's hello; the answer carries the revocation.
	w.bob.answered.release(alice)
	req, _ := json.Marshal(helloFrame{Credential: w.alice.myCredential})
	raw, err := w.bob.handleHello(ctx, alice, req)
	if err != nil {
		t.Fatal(err)
	}
	var f helloFrame
	_ = json.Unmarshal(raw, &f)
	if len(f.AccountRevoked) != 1 {
		t.Fatalf("Bob's hello answer carried %d revocations, want 1", len(f.AccountRevoked))
	}
	w.alice.ingestHello(bob, raw)
	if !w.alice.leafRevoked(cred) {
		t.Fatal("Alice did not keep the revocation Bob's own desktop handed her")
	}
	if fp := w.alice.presence(pid).Fingerprint; fp == w.bob.id.Fingerprint() {
		t.Fatal("Alice still places the unlinked phone as Bob, so she would serve it Bob's chain")
	}

	// The door. The binding check passes the phone (its certificate verifies and
	// names its key); the revocation is what refuses it.
	ireq, _ := json.Marshal(inviteRequest{GuildID: g.ID, KeyPackage: kp, Credential: cred, Heal: true})
	out, err := w.alice.handleInviteRequest(ctx, pid, ireq)
	if err != nil {
		t.Fatal(err)
	}
	var resp inviteResponse
	_ = json.Unmarshal(out, &resp)
	if len(resp.Welcome) > 0 || resp.Error != "this device was unlinked" {
		t.Fatalf("Alice's door: welcome %d bytes, error %q; want the unlinked refusal", len(resp.Welcome), resp.Error)
	}

	// It survives a restart: kept in the store, loaded at start.
	w.alice.deviceMu.Lock()
	w.alice.peerRevoked = nil
	w.alice.deviceMu.Unlock()
	w.alice.loadPeerRevoked()
	if !w.alice.leafRevoked(cred) {
		t.Fatal("the revocation did not survive a reload")
	}
	// And a credential Alice learns again later (a roster, a message) does not
	// map the phone back to Bob.
	w.alice.learnDeviceCert(cred)
	if fp := w.alice.presence(pid).Fingerprint; fp == w.bob.id.Fingerprint() {
		t.Fatal("a later sighting of the certificate re-mapped the unlinked phone to Bob")
	}
}

// The per-account cap keeps an account's newest records and leaves others'.
func TestPeerRevocationsCapPerAccount(t *testing.T) {
	a, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	var recs []*identity.DeviceRevocation
	for i := 0; i < 10; i++ {
		d, _ := identity.Generate()
		recs = append(recs, a.RevokeDevice(d.PublicKey(), int64(100+i)))
	}
	d, _ := identity.Generate()
	recs = append(recs, b.RevokeDevice(d.PublicKey(), 1))
	out := capPerAccount(recs, 3)
	if len(out) != 4 {
		t.Fatalf("kept %d, want 3 of A and 1 of B", len(out))
	}
	for _, r := range out[:3] {
		if r.RevokedAt < 107 {
			t.Fatalf("kept an older record of A (%d)", r.RevokedAt)
		}
	}
	self := a.RevokeDevice(a.PublicKey(), 5)
	if peerRevocationValid(self) {
		t.Fatal("an account's revocation of its own account key passed as a device unlink")
	}
}
