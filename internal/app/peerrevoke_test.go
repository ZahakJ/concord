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

// A stranger can make us keep nothing. presence() gives any peer that says
// hello a fingerprint of its own key, so "placed as the account that signed"
// alone let anyone mint identities and fill our store; only an account we
// share a guild with is a source now.
func TestAStrangersHelloKeepsNoRevocations(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx) // no guild: Carol is a stranger to Alice
	carol := w.carol.host.PeerID()
	var recs []*identity.DeviceRevocation
	for i := 0; i < helloRevocationsMax; i++ {
		d, _ := identity.Generate()
		recs = append(recs, w.carol.id.RevokeDevice(d.PublicKey(), int64(1000+i)))
	}
	raw, _ := json.Marshal(helloFrame{Credential: w.carol.myCredential, AccountRevoked: recs})
	w.alice.ingestHello(carol, raw)
	if fp := w.alice.presence(carol).Fingerprint; fp != w.carol.Fingerprint() {
		t.Fatalf("Alice places Carol as %q; the test needs her placed as herself", fp)
	}
	if v, _ := w.alice.store.GetSetting(peerRevocationsKey); v != "" {
		t.Fatalf("a stranger's hello wrote %d bytes of revocations", len(v))
	}
	w.alice.deviceMu.RLock()
	n := len(w.alice.peerRevoked)
	w.alice.deviceMu.RUnlock()
	if n != 0 {
		t.Fatalf("a stranger's hello put %d revocations in memory", n)
	}
	if w.alice.revocationSource(w.carol.Fingerprint()) {
		t.Fatal("a stranger counts as a revocation source")
	}
}

func revService(t *testing.T) *Service {
	t.Helper()
	me, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	s := recordService(t, me)
	s.loadPeerRevoked()
	return s
}

func revocationsBy(t *testing.T, a *identity.Identity, n int, at int64) []*identity.DeviceRevocation {
	t.Helper()
	var out []*identity.DeviceRevocation
	for i := 0; i < n; i++ {
		d, err := identity.Generate()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, a.RevokeDevice(d.PublicKey(), at+int64(i)))
	}
	return out
}

func keptCount(t *testing.T, s *Service) (mem, disk int) {
	t.Helper()
	s.deviceMu.RLock()
	mem = len(s.peerRevoked)
	s.deviceMu.RUnlock()
	recs, err := s.readPeerRevocations()
	if err != nil {
		t.Fatal(err)
	}
	return mem, len(recs)
}

// The caps refuse instead of evicting: an account's 65th record, however new
// its signer says it is, does not push out an older one, and memory and the
// store hold the same set.
func TestPeerRevocationCapsRefuseAndNeverEvict(t *testing.T) {
	s := revService(t)
	a, _ := identity.Generate()
	fpr := a.Fingerprint()
	old := revocationsBy(t, a, peerRevocationsPerAccount, 100)
	for i := 0; i < len(old); i += helloRevocationsMax {
		if err := s.keepPeerRevocations(fpr, old[i:min(i+helloRevocationsMax, len(old))]); err != nil {
			t.Fatal(err)
		}
	}
	late := revocationsBy(t, a, 3, 1<<40) // "newer" by the signer's own clock
	if err := s.keepPeerRevocations(fpr, late); err != nil {
		t.Fatal(err)
	}
	if mem, disk := keptCount(t, s); mem != peerRevocationsPerAccount || disk != peerRevocationsPerAccount {
		t.Fatalf("kept %d in memory and %d stored, want %d in both", mem, disk, peerRevocationsPerAccount)
	}
	for _, r := range old {
		if !s.certRevoked(&identity.DeviceCert{AccountPub: r.AccountPub, DevicePub: r.DevicePub}) {
			t.Fatal("an older record was evicted")
		}
	}
	for _, r := range late {
		if s.certRevoked(&identity.DeviceCert{AccountPub: r.AccountPub, DevicePub: r.DevicePub}) {
			t.Fatal("a record past the cap was kept")
		}
	}
	// Records from the wrong account, or that do not verify, are not taken.
	b, _ := identity.Generate()
	bad := revocationsBy(t, b, 1, 1)
	bad[0].Sig = append([]byte(nil), bad[0].Sig...)
	bad[0].Sig[0] ^= 1
	_ = s.keepPeerRevocations(b.Fingerprint(), append(bad, revocationsBy(t, a, 1, 1)...))
	if mem, disk := keptCount(t, s); mem != peerRevocationsPerAccount || disk != peerRevocationsPerAccount {
		t.Fatalf("a forged or misattributed record was kept (%d, %d)", mem, disk)
	}
	// A reload gives back the same set.
	s.loadPeerRevoked()
	if mem, _ := keptCount(t, s); mem != peerRevocationsPerAccount {
		t.Fatalf("reload holds %d", mem)
	}
}

// The whole set has a cap too, across accounts.
func TestPeerRevocationsTotalCap(t *testing.T) {
	s := revService(t)
	for n := 0; n*peerRevocationsPerAccount <= peerRevocationsTotal; n++ {
		a, _ := identity.Generate()
		recs := revocationsBy(t, a, peerRevocationsPerAccount, 1)
		for i := 0; i < len(recs); i += helloRevocationsMax {
			_ = s.keepPeerRevocations(a.Fingerprint(), recs[i:i+helloRevocationsMax])
		}
	}
	if mem, disk := keptCount(t, s); mem != peerRevocationsTotal || disk != peerRevocationsTotal {
		t.Fatalf("kept %d in memory and %d stored, want %d", mem, disk, peerRevocationsTotal)
	}
}

// A stored set that cannot be read is not taken for an empty one: nothing is
// written over it, the new record still counts in memory, and the failure is
// reported.
func TestPeerRevocationsUnreadableSetIsNotOverwritten(t *testing.T) {
	s := revService(t)
	if err := s.store.SetSetting(peerRevocationsKey, "{not json"); err != nil {
		t.Fatal(err)
	}
	s.loadPeerRevoked()
	a, _ := identity.Generate()
	recs := revocationsBy(t, a, 1, 1)
	if err := s.keepPeerRevocations(a.Fingerprint(), recs); err == nil {
		t.Fatal("keeping a record over an unreadable set reported no error")
	}
	if v, _ := s.store.GetSetting(peerRevocationsKey); v != "{not json" {
		t.Fatalf("the unreadable set was overwritten with %q", v)
	}
	if !s.certRevoked(&identity.DeviceCert{AccountPub: recs[0].AccountPub, DevicePub: recs[0].DevicePub}) {
		t.Fatal("the new record does not count in memory")
	}
}

// Past one hello's worth, the newest half goes every time and the rest walk
// through the older records, so every unlink reaches a member eventually.
func TestShareWindowReachesEveryRecord(t *testing.T) {
	s := revService(t)
	a, _ := identity.Generate()
	recs := revocationsBy(t, a, 100, 1)
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		out := s.shareWindow(recs)
		if len(out) != helloRevocationsMax {
			t.Fatalf("a hello carried %d", len(out))
		}
		for j := 0; j < helloRevocationsMax/2; j++ {
			if out[j].RevokedAt != int64(100-j) {
				t.Fatalf("slot %d is not the %d-th newest", j, j)
			}
		}
		for _, r := range out {
			seen[peerRevKey(r.AccountPub, r.DevicePub)] = true
		}
	}
	if len(seen) != len(recs) {
		t.Fatalf("six hellos carried %d of %d records", len(seen), len(recs))
	}
	if got := s.shareWindow(recs[:5]); len(got) != 5 {
		t.Fatalf("a short list went as %d", len(got))
	}
}

func TestPeerRevocationValidRefusesTheAccountKey(t *testing.T) {
	a, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	self := a.RevokeDevice(a.PublicKey(), 5)
	if peerRevocationValid(self) {
		t.Fatal("an account's revocation of its own account key passed as a device unlink")
	}
}
