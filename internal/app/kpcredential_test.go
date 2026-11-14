package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
)

// A join request carries two credentials: the one the committer binds to the
// connection it arrived on, and the one inside the key package, which is the
// one the leaf keeps and everything after reads (roster, governance, display
// names). MLS checks the key package's signature against its own leaf key and
// nothing more, so the second credential is whatever the sender wrote. These
// tests have a requester present its own, honest credential with a key package
// that names somebody else, on every admission path, and require the committer
// to refuse without touching the group.

// forgedKeyPackage is a well-formed key package whose leaf names cred, signed
// by a fresh key that nobody in any group holds.
func forgedKeyPackage(t *testing.T, ctx context.Context, cred []byte) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := mls.New(cred, priv)
	if err != nil {
		t.Fatalf("forge engine: %v", err)
	}
	defer eng.Close()
	kp, err := eng.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("forge key package: %v", err)
	}
	return kp
}

// rosterOf is a guild's leaf credentials (hex, sorted) and epoch as one
// member's own MLS state holds them.
func rosterOf(t *testing.T, ctx context.Context, s *Service, g domain.Guild) ([]string, uint64) {
	t.Helper()
	creds, err := s.mls.Members(ctx, g.GroupID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	out := make([]string, 0, len(creds))
	for _, c := range creds {
		out = append(out, hex.EncodeToString(c))
	}
	slices.Sort(out)
	epoch, err := s.mls.Epoch(ctx, g.GroupID)
	if err != nil {
		t.Fatalf("epoch: %v", err)
	}
	return out, epoch
}

// credentialOf copies a member's leaf credential out of the committer's roster,
// the way anyone in the guild could.
func credentialOf(t *testing.T, ctx context.Context, committer, member *Service, g domain.Guild) []byte {
	t.Helper()
	creds, err := committer.mls.Members(ctx, g.GroupID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	for _, c := range creds {
		if bytes.Equal(c, member.myCredential) {
			return append([]byte(nil), c...)
		}
	}
	t.Fatalf("the member's credential is not on the committer's roster")
	return nil
}

// requireForgedRefused sends req from requester to committer over the real
// invite stream and requires a refusal, no welcome, and the committer's roster
// and epoch exactly as they were.
func requireForgedRefused(t *testing.T, ctx context.Context, committer, requester *Service, g domain.Guild, req inviteRequest) {
	t.Helper()
	membersBefore, epochBefore := rosterOf(t, ctx, committer, g)
	body, _ := json.Marshal(req)
	rctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	raw, err := requester.host.RequestInvite(rctx, committer.host.AddrInfo(), body)
	if err != nil {
		t.Fatalf("invite stream: %v", err)
	}
	var resp inviteResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("invite response: %v", err)
	}
	if resp.Error == "" || len(resp.Welcome) != 0 {
		t.Errorf("the committer served a key package naming a credential the requester did not prove (error %q, welcome %d bytes)", resp.Error, len(resp.Welcome))
	} else {
		t.Logf("refused: %q", resp.Error)
	}
	membersAfter, epochAfter := rosterOf(t, ctx, committer, g)
	if epochAfter != epochBefore {
		t.Errorf("the committer's epoch moved from %d to %d: a commit was made", epochBefore, epochAfter)
	}
	if !slices.Equal(membersAfter, membersBefore) {
		t.Errorf("the committer's roster changed:\nbefore %v\nafter  %v", membersBefore, membersAfter)
	}
}

// kpWorld is Alice (who owns things), Carol (a member whose credential gets
// borrowed) and Bob (the requester), on one rendezvous.
func kpWorld(t *testing.T, ctx context.Context) (alice, carol, bob *Service) {
	t.Helper()
	boot := testRendezvous(t, ctx)
	alice = startServiceOn(t, ctx, t.TempDir(), boot)
	carol = startServiceOn(t, ctx, t.TempDir(), boot)
	bob = startServiceOn(t, ctx, t.TempDir(), boot)
	for svc, name := range map[*Service]string{alice: "Alice", carol: "Carol", bob: "Bob"} {
		if err := svc.SetDisplayName(name); err != nil {
			t.Fatal(err)
		}
	}
	return alice, carol, bob
}

func inviteCodeOf(t *testing.T, s *Service, guildID string) string {
	t.Helper()
	code, err := s.InviteCode(guildID)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// Invite join: Bob holds the code and asks in under his own credential, with a
// key package naming Alice, the owner, or naming Carol while leaving his own
// credential out of the request altogether.
func TestInviteJoinRefusesKeyPackageNamingSomeoneElse(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alice, carol, bob := kpWorld(t, ctx)
	g, err := alice.CreateGuild("shared")
	if err != nil {
		t.Fatal(err)
	}
	code := inviteCodeOf(t, alice, g.ID)
	if _, err := carol.JoinViaInvite(code); err != nil {
		t.Fatalf("Carol joins: %v", err)
	}
	waitMemberCount(t, 60*time.Second, g.ID, 2, alice, carol)

	asOwner := forgedKeyPackage(t, ctx, credentialOf(t, ctx, alice, alice, g))
	requireForgedRefused(t, ctx, alice, bob, g, inviteRequest{
		GuildID: g.ID, KeyPackage: asOwner, Credential: bob.myCredential, Profile: bob.selfWireProfile(),
	})
	asCarol := forgedKeyPackage(t, ctx, credentialOf(t, ctx, alice, carol, g))
	requireForgedRefused(t, ctx, alice, bob, g, inviteRequest{
		GuildID: g.ID, KeyPackage: asCarol, Profile: bob.selfWireProfile(),
	})

	if _, err := bob.JoinViaInvite(code); err != nil {
		t.Fatalf("an honest join right after was refused: %v", err)
	}
	waitMemberCount(t, 60*time.Second, g.ID, 3, alice, carol, bob)
}

// Heal re-add: Bob is seated and asks to be re-added, under his own credential,
// with a key package naming Carol.
func TestHealRefusesKeyPackageNamingSomeoneElse(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alice, carol, bob := kpWorld(t, ctx)
	g, err := alice.CreateGuild("shared")
	if err != nil {
		t.Fatal(err)
	}
	code := inviteCodeOf(t, alice, g.ID)
	seated := []*Service{alice}
	for _, j := range []*Service{carol, bob} {
		if _, err := j.JoinViaInvite(code); err != nil {
			t.Fatalf("join: %v", err)
		}
		seated = append(seated, j)
		waitMemberCount(t, 60*time.Second, g.ID, len(seated), seated...)
	}

	asCarol := forgedKeyPackage(t, ctx, credentialOf(t, ctx, alice, carol, g))
	requireForgedRefused(t, ctx, alice, bob, g, inviteRequest{
		GuildID: g.ID, KeyPackage: asCarol, Credential: bob.myCredential, Profile: bob.selfWireProfile(), Heal: true,
	})

	if ok, why := bob.healAsk(g.ID, alice.host.PeerID()); !ok {
		t.Fatalf("an honest heal right after was refused: %q", why)
	}
	waitMemberCount(t, 60*time.Second, g.ID, 3, alice, carol, bob)
}

// Linked device admission: Bob's phone, presenting its own device cert, redeems
// the guild's code with a key package naming Alice, the owner.
func TestLinkedDeviceRefusesKeyPackageNamingSomeoneElse(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	g := w.guildOf(t, w.alice, w.bob, w.carol)
	if bytes.Equal(w.phone.myCredential, w.bob.myCredential) {
		t.Fatal("setup: the phone has no device credential of its own")
	}

	asOwner := forgedKeyPackage(t, ctx, credentialOf(t, ctx, w.alice, w.alice, g))
	requireForgedRefused(t, ctx, w.alice, w.phone, g, inviteRequest{
		GuildID: g.ID, KeyPackage: asOwner, Credential: w.phone.myCredential, Profile: w.phone.selfWireProfile(),
	})

	if _, err := w.phone.JoinViaInvite(inviteCodeOf(t, w.alice, g.ID)); err != nil {
		t.Fatalf("the phone's honest admission right after was refused: %v", err)
	}
	waitMemberCount(t, 60*time.Second, g.ID, 4, w.alice, w.bob, w.carol, w.phone)
}

// DM start: Alice opens a DM by code, Bob redeems it under his own credential
// with a key package naming Carol, who is in a guild with both of them.
func TestDMStartRefusesKeyPackageNamingSomeoneElse(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alice, carol, bob := kpWorld(t, ctx)
	shared, err := alice.CreateGuild("shared")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := carol.JoinViaInvite(inviteCodeOf(t, alice, shared.ID)); err != nil {
		t.Fatalf("Carol joins: %v", err)
	}
	waitMemberCount(t, 60*time.Second, shared.ID, 2, alice, carol)
	carolCred := credentialOf(t, ctx, alice, carol, shared)

	dmCode, err := alice.NewDMInvite()
	if err != nil {
		t.Fatal(err)
	}
	ic, err := decodeInviteCode(dmCode)
	if err != nil {
		t.Fatal(err)
	}
	alice.mu.RLock()
	dm := alice.guilds[ic.GuildID].Clone()
	alice.mu.RUnlock()
	if dm.Kind != "dm" {
		t.Fatalf("setup: NewDMInvite made a %q", dm.Kind)
	}

	requireForgedRefused(t, ctx, alice, bob, dm, inviteRequest{
		GuildID: dm.ID, KeyPackage: forgedKeyPackage(t, ctx, carolCred), Credential: bob.myCredential, Profile: bob.selfWireProfile(),
	})

	if _, err := bob.JoinViaInvite(dmCode); err != nil {
		t.Fatalf("an honest DM join right after was refused: %v", err)
	}
	waitMemberCount(t, 60*time.Second, dm.ID, 2, alice, bob)
}
