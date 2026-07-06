package mls

import (
	"bytes"
	"context"
	"testing"

	"github.com/zahak/concord/internal/identity"
)

// engineForNewMember builds an MLS engine whose credential is a fresh Concord
// Ed25519 identity's public key, mirroring real usage.
func engineForNewMember(t *testing.T) (Engine, []byte) {
	t.Helper()
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	cred := []byte(id.PublicKey())
	eng, err := New(cred, id.PrivateKey())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng, cred
}

func TestTwoPartyGroupRoundTrip(t *testing.T) {
	ctx := context.Background()
	alice, aliceCred := engineForNewMember(t)
	bob, _ := engineForNewMember(t)

	bobKP, err := bob.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("bob KeyPackage: %v", err)
	}

	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Alice invites Bob; in a 2-party group the inviter's state advances in
	// place, so only the welcome needs delivering.
	_, welcome, err := alice.Invite(ctx, gid, bobKP)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := bob.Join(ctx, welcome); err != nil {
		t.Fatalf("Join: %v", err)
	}

	plaintext := []byte("hello from alice")
	ct, err := alice.Encrypt(ctx, gid, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// The wire form must not leak the plaintext.
	if bytes.Contains(ct, plaintext) {
		t.Fatal("plaintext found in ciphertext")
	}

	msg, err := bob.Decrypt(ctx, gid, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(msg.Plaintext, plaintext) {
		t.Fatalf("decrypted %q, want %q", msg.Plaintext, plaintext)
	}
	// Sender is authenticated as Alice's account key.
	if !bytes.Equal(msg.SenderID, aliceCred) {
		t.Fatal("sender identity does not match Alice's credential")
	}

	// And the reverse direction works too.
	ct2, err := bob.Encrypt(ctx, gid, []byte("hi alice"))
	if err != nil {
		t.Fatalf("bob Encrypt: %v", err)
	}
	if _, err := alice.Decrypt(ctx, gid, ct2); err != nil {
		t.Fatalf("alice Decrypt: %v", err)
	}
}

func TestThreePartyGroupWithCommitDelivery(t *testing.T) {
	ctx := context.Background()
	alice, _ := engineForNewMember(t)
	bob, _ := engineForNewMember(t)
	carol, _ := engineForNewMember(t)

	// Alice creates and invites Bob.
	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	bobKP, _ := bob.KeyPackage(ctx)
	_, bobWelcome, err := alice.Invite(ctx, gid, bobKP)
	if err != nil {
		t.Fatalf("invite bob: %v", err)
	}
	if _, err := bob.Join(ctx, bobWelcome); err != nil {
		t.Fatalf("bob join: %v", err)
	}

	// Alice invites Carol. Now Bob is an existing member and MUST apply the
	// commit to advance to the new epoch, or he falls out of sync.
	carolKP, _ := carol.KeyPackage(ctx)
	commit, carolWelcome, err := alice.Invite(ctx, gid, carolKP)
	if err != nil {
		t.Fatalf("invite carol: %v", err)
	}
	if err := bob.ApplyCommit(ctx, gid, commit); err != nil {
		t.Fatalf("bob apply commit: %v", err)
	}
	if _, err := carol.Join(ctx, carolWelcome); err != nil {
		t.Fatalf("carol join: %v", err)
	}

	// All three should now share the epoch: Carol sends, Alice and Bob read.
	ct, err := carol.Encrypt(ctx, gid, []byte("carol here"))
	if err != nil {
		t.Fatalf("carol encrypt: %v", err)
	}
	for name, r := range map[string]Engine{"alice": alice, "bob": bob} {
		msg, err := r.Decrypt(ctx, gid, ct)
		if err != nil {
			t.Fatalf("%s decrypt: %v", name, err)
		}
		if string(msg.Plaintext) != "carol here" {
			t.Fatalf("%s decrypted %q", name, msg.Plaintext)
		}
	}

	// Membership reflects all three.
	members, err := alice.Members(ctx, gid)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("group has %d members, want 3", len(members))
	}
}

func TestRemovedMemberLosesAccess(t *testing.T) {
	ctx := context.Background()
	alice, _ := engineForNewMember(t)
	bob, _ := engineForNewMember(t)
	carol, carolCred := engineForNewMember(t)

	// Build a 3-member group: alice (owner) + bob + carol.
	gid, _ := alice.CreateGroup(ctx)
	bobKP, _ := bob.KeyPackage(ctx)
	_, bobWelcome, _ := alice.Invite(ctx, gid, bobKP)
	if _, err := bob.Join(ctx, bobWelcome); err != nil {
		t.Fatalf("bob join: %v", err)
	}
	carolKP, _ := carol.KeyPackage(ctx)
	addCommit, carolWelcome, _ := alice.Invite(ctx, gid, carolKP)
	if err := bob.ApplyCommit(ctx, gid, addCommit); err != nil {
		t.Fatalf("bob apply add: %v", err)
	}
	if _, err := carol.Join(ctx, carolWelcome); err != nil {
		t.Fatalf("carol join: %v", err)
	}

	// Alice removes Carol; Bob applies the removal commit.
	rmCommit, err := alice.Remove(ctx, gid, carolCred)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := bob.ApplyCommit(ctx, gid, rmCommit); err != nil {
		t.Fatalf("bob apply removal: %v", err)
	}

	// Alice sends at the new epoch: Bob (still a member) reads it, Carol cannot.
	ct, err := alice.Encrypt(ctx, gid, []byte("members only"))
	if err != nil {
		t.Fatalf("encrypt post-removal: %v", err)
	}
	if _, err := bob.Decrypt(ctx, gid, ct); err != nil {
		t.Fatalf("remaining member bob cannot decrypt: %v", err)
	}
	if _, err := carol.Decrypt(ctx, gid, ct); err == nil {
		t.Fatal("removed member carol could still decrypt new messages")
	}

	// And the roster reflects the removal.
	members, _ := alice.Members(ctx, gid)
	if len(members) != 2 {
		t.Fatalf("group has %d members after removal, want 2", len(members))
	}
}

func TestOutsiderCannotDecrypt(t *testing.T) {
	ctx := context.Background()
	alice, _ := engineForNewMember(t)
	bob, _ := engineForNewMember(t)
	eve, _ := engineForNewMember(t) // never added to the group

	gid, _ := alice.CreateGroup(ctx)
	bobKP, _ := bob.KeyPackage(ctx)
	_, welcome, _ := alice.Invite(ctx, gid, bobKP)
	if _, err := bob.Join(ctx, welcome); err != nil {
		t.Fatalf("bob join: %v", err)
	}

	ct, err := alice.Encrypt(ctx, gid, []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Eve has no group state for gid; decryption must fail, not leak.
	if _, err := eve.Decrypt(ctx, gid, ct); err == nil {
		t.Fatal("outsider was able to decrypt group ciphertext")
	}
}
