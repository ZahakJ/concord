package mls

import (
	"bytes"
	"context"
	"testing"
)

// batchOf builds n fresh engines and their key packages, ready to be admitted.
func batchOf(t *testing.T, ctx context.Context, n int) ([]Engine, [][]byte) {
	t.Helper()
	engines := make([]Engine, 0, n)
	kps := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		eng, _ := engineForNewMember(t)
		kp, err := eng.KeyPackage(ctx)
		if err != nil {
			t.Fatalf("KeyPackage %d: %v", i, err)
		}
		engines = append(engines, eng)
		kps = append(kps, kp)
	}
	return engines, kps
}

// TestAddMembersIsOneEpoch is the property the whole admission batch rests on:
// N joiners cost ONE commit and ONE epoch, and every one of them can read what
// the group says afterwards.
func TestAddMembersIsOneEpoch(t *testing.T) {
	ctx := context.Background()
	alice, _ := engineForNewMember(t)
	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	before, err := alice.Epoch(ctx, gid)
	if err != nil {
		t.Fatalf("Epoch: %v", err)
	}

	joiners, kps := batchOf(t, ctx, 6)
	commit, welcome, accepted, err := alice.AddMembers(ctx, gid, kps)
	if err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	if len(accepted) != len(kps) {
		t.Fatalf("accepted %d of %d key packages", len(accepted), len(kps))
	}
	if len(commit) == 0 {
		t.Fatal("no commit")
	}

	after, err := alice.Epoch(ctx, gid)
	if err != nil {
		t.Fatalf("Epoch after: %v", err)
	}
	if after != before+1 {
		t.Fatalf("epoch went %d -> %d, want exactly one step for %d joiners", before, after, len(kps))
	}
	if members, err := alice.Members(ctx, gid); err != nil || len(members) != len(kps)+1 {
		t.Fatalf("members %d (err %v), want %d", len(members), err, len(kps)+1)
	}

	// One welcome, every joiner finds its own secret in it.
	for i, j := range joiners {
		if _, err := j.Join(ctx, welcome); err != nil {
			t.Fatalf("joiner %d Join: %v", i, err)
		}
	}
	plaintext := []byte("everyone hears this")
	ct, err := alice.Encrypt(ctx, gid, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	for i, j := range joiners {
		msg, err := j.Decrypt(ctx, gid, ct)
		if err != nil {
			t.Fatalf("joiner %d Decrypt: %v", i, err)
		}
		if !bytes.Equal(msg.Plaintext, plaintext) {
			t.Fatalf("joiner %d read %q", i, msg.Plaintext)
		}
	}
}

// TestBatchCommitAppliesForExistingMembers proves the batch commit is an
// ordinary commit on the receive side: a member who was already in the group
// applies it and lands on the same epoch, and CommitSender still names the
// author (the authorization gate reads it before applying).
func TestBatchCommitAppliesForExistingMembers(t *testing.T) {
	ctx := context.Background()
	alice, aliceCred := engineForNewMember(t)
	bob, _ := engineForNewMember(t)
	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	bobKP, err := bob.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("bob KeyPackage: %v", err)
	}
	if _, welcome, err := alice.Invite(ctx, gid, bobKP); err != nil {
		t.Fatalf("Invite: %v", err)
	} else if _, err := bob.Join(ctx, welcome); err != nil {
		t.Fatalf("bob Join: %v", err)
	}

	joiners, kps := batchOf(t, ctx, 4)
	commit, welcome, _, err := alice.AddMembers(ctx, gid, kps)
	if err != nil {
		t.Fatalf("AddMembers: %v", err)
	}

	sender, err := bob.CommitSender(ctx, gid, commit)
	if err != nil {
		t.Fatalf("CommitSender: %v", err)
	}
	if !bytes.Equal(sender, aliceCred) {
		t.Fatal("batch commit does not name Alice as its author")
	}
	if err := bob.ApplyCommit(ctx, gid, commit); err != nil {
		t.Fatalf("bob ApplyCommit: %v", err)
	}
	aliceEpoch, _ := alice.Epoch(ctx, gid)
	bobEpoch, _ := bob.Epoch(ctx, gid)
	if aliceEpoch != bobEpoch {
		t.Fatalf("epochs diverged: alice %d, bob %d", aliceEpoch, bobEpoch)
	}
	for i, j := range joiners {
		if _, err := j.Join(ctx, welcome); err != nil {
			t.Fatalf("joiner %d Join: %v", i, err)
		}
	}
	ct, err := bob.Encrypt(ctx, gid, []byte("from an old member"))
	if err != nil {
		t.Fatalf("bob Encrypt: %v", err)
	}
	if _, err := joiners[0].Decrypt(ctx, gid, ct); err != nil {
		t.Fatalf("joiner Decrypt: %v", err)
	}
}

// TestBatchDropsBadKeyPackagesInsteadOfFailing is the no-poison property: a
// joiner whose leaf is still in the tree (the retry case) and a joiner with a
// malformed key package are both dropped, and the rest of the queue commits.
func TestBatchDropsBadKeyPackagesInsteadOfFailing(t *testing.T) {
	ctx := context.Background()
	alice, _ := engineForNewMember(t)
	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	stale, _ := engineForNewMember(t)
	staleKP, err := stale.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("KeyPackage: %v", err)
	}
	if _, _, err := alice.Invite(ctx, gid, staleKP); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	// A SECOND key package from the same member: fresh init key, same signature
	// key, so it collides with the leaf that is already seated. This is exactly
	// what a joiner retrying a lost welcome sends.
	staleRetryKP, err := stale.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("second KeyPackage: %v", err)
	}

	good, goodKPs := batchOf(t, ctx, 3)
	kps := [][]byte{
		[]byte("not a key package"),
		goodKPs[0],
		staleRetryKP,
		goodKPs[1],
		goodKPs[2],
	}
	epochBefore, _ := alice.Epoch(ctx, gid)
	_, welcome, accepted, err := alice.AddMembers(ctx, gid, kps)
	if err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	wantAccepted := []int{1, 3, 4}
	if len(accepted) != len(wantAccepted) {
		t.Fatalf("accepted %v, want %v", accepted, wantAccepted)
	}
	for i, got := range accepted {
		if got != wantAccepted[i] {
			t.Fatalf("accepted %v, want %v", accepted, wantAccepted)
		}
	}
	epochAfter, _ := alice.Epoch(ctx, gid)
	if epochAfter != epochBefore+1 {
		t.Fatalf("epoch went %d -> %d, want one step", epochBefore, epochAfter)
	}
	for i, j := range good {
		if _, err := j.Join(ctx, welcome); err != nil {
			t.Fatalf("good joiner %d Join: %v", i, err)
		}
	}
}

// TestBatchWithNothingAdmissibleCostsNoEpoch: a queue of rejects must not
// advance the group. An empty commit every member has to apply is exactly the
// churn the batch exists to avoid.
func TestBatchWithNothingAdmissibleCostsNoEpoch(t *testing.T) {
	ctx := context.Background()
	alice, _ := engineForNewMember(t)
	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	before, _ := alice.Epoch(ctx, gid)
	if _, _, _, err := alice.AddMembers(ctx, gid, [][]byte{[]byte("junk"), []byte("more junk")}); err == nil {
		t.Fatal("AddMembers accepted a batch of junk")
	}
	after, _ := alice.Epoch(ctx, gid)
	if after != before {
		t.Fatalf("epoch moved %d -> %d on a batch that admitted nobody", before, after)
	}
	// And the group is still usable: nothing was left staged.
	bob, _ := engineForNewMember(t)
	bobKP, err := bob.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("KeyPackage: %v", err)
	}
	_, welcome, err := alice.Invite(ctx, gid, bobKP)
	if err != nil {
		t.Fatalf("Invite after failed batch: %v", err)
	}
	if _, err := bob.Join(ctx, welcome); err != nil {
		t.Fatalf("Join after failed batch: %v", err)
	}
}

// TestRemoveDoesNotCarryStagedAdds is the hazard the engine's commit mutex and
// orphan sweep exist for: a Remove commits every proposal the group is holding
// and returns no welcome, so an Add left staged by an interrupted batch would
// seat a member nobody could ever reach. After a batch that failed to commit,
// a Remove must move exactly one leaf.
func TestRemoveDoesNotCarryStagedAdds(t *testing.T) {
	ctx := context.Background()
	alice, _ := engineForNewMember(t)
	gid, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	victim, victimCred := engineForNewMember(t)
	victimKP, err := victim.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("KeyPackage: %v", err)
	}
	if _, _, err := alice.Invite(ctx, gid, victimKP); err != nil {
		t.Fatalf("Invite: %v", err)
	}

	// A batch that admits nobody leaves nothing behind...
	_, _, _, _ = alice.AddMembers(ctx, gid, [][]byte{[]byte("junk")})
	// ...and one that succeeds does not either.
	_, _, _, err = alice.AddMembers(ctx, gid, func() [][]byte {
		_, kps := batchOf(t, ctx, 2)
		return kps
	}())
	if err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	countBefore, err := alice.Members(ctx, gid)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if _, err := alice.Remove(ctx, gid, victimCred); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	countAfter, err := alice.Members(ctx, gid)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(countAfter) != len(countBefore)-1 {
		t.Fatalf("Remove took the roster from %d to %d, want one leaf gone",
			len(countBefore), len(countAfter))
	}
}
