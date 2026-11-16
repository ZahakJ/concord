package mls

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	upstream "github.com/thomas-vilte/mls-go"
)

// JoinAs joins only the group the caller names. A welcome for any other group
// is refused before anything is stored: no state is created for that group,
// a group the device already holds under the welcome's id keeps its state,
// and the key package stays pending so the right welcome still joins.
func TestJoinAsRefusesAWelcomeForAnotherGroup(t *testing.T) {
	ctx := context.Background()
	alice, _ := engineForNewMember(t)
	bob, _ := engineForNewMember(t)

	// Bob holds g1.
	g1, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	kp1, _ := bob.KeyPackage(ctx)
	_, w1, err := alice.Invite(ctx, g1, kp1)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := bob.JoinAs(ctx, w1, g1); err != nil || !bytes.Equal(got, g1) {
		t.Fatalf("JoinAs for the named group: %x, %v", got, err)
	}

	// A welcome for g2, joined as g1: refused, and nothing for g2 is stored.
	g2, err := alice.CreateGroup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	kp2, _ := bob.KeyPackage(ctx)
	_, w2, err := alice.Invite(ctx, g2, kp2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.JoinAs(ctx, w2, g1); !errors.Is(err, upstream.ErrWrongGroup) {
		t.Fatalf("a welcome for another group: err %v, want ErrWrongGroup", err)
	}
	if _, err := bob.Epoch(ctx, g2); err == nil {
		t.Fatal("a refused welcome left state for its group")
	}

	// A welcome whose group id is that of the held group g1 (a second group
	// Carol created under the same id), joined as g2: refused, and the held
	// state is untouched (same epoch, same roster).
	epoch, _ := bob.Epoch(ctx, g1)
	members, _ := bob.Members(ctx, g1)
	carol, _ := engineForNewMember(t)
	ckp, _ := carol.KeyPackage(ctx)
	if _, err := carol.(*mlsEngine).c.CreateGroupWithExtensions(ctx, g1, ckp, nil); err != nil {
		t.Fatal(err)
	}
	kp3, _ := bob.KeyPackage(ctx)
	_, w3, err := carol.Invite(ctx, g1, kp3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.JoinAs(ctx, w3, g2); !errors.Is(err, upstream.ErrWrongGroup) {
		t.Fatalf("a welcome for the held group under another name: err %v, want ErrWrongGroup", err)
	}
	if e, err := bob.Epoch(ctx, g1); err != nil || e != epoch {
		t.Fatalf("held group's epoch %d -> %d, %v", epoch, e, err)
	}
	if m, err := bob.Members(ctx, g1); err != nil || !sameMembers(m, members) {
		t.Fatalf("held group's roster changed: %x -> %x, %v", members, m, err)
	}
	ct1, err := alice.Encrypt(ctx, g1, []byte("in g1"))
	if err != nil {
		t.Fatal(err)
	}
	if msg, err := bob.Decrypt(ctx, g1, ct1); err != nil || string(msg.Plaintext) != "in g1" {
		t.Fatalf("the held group no longer reads its own traffic: %v", err)
	}

	// The key package was kept: the welcome for g2, joined as g2, works.
	if got, err := bob.JoinAs(ctx, w2, g2); err != nil || !bytes.Equal(got, g2) {
		t.Fatalf("JoinAs after a refusal: %x, %v", got, err)
	}
	ct, err := alice.Encrypt(ctx, g2, []byte("in g2"))
	if err != nil {
		t.Fatal(err)
	}
	if msg, err := bob.Decrypt(ctx, g2, ct); err != nil || string(msg.Plaintext) != "in g2" {
		t.Fatalf("decrypt in g2 after JoinAs: %v", err)
	}

	// An empty name is refused outright.
	if _, err := bob.JoinAs(ctx, w3, nil); err == nil {
		t.Fatal("JoinAs with no group named joined")
	}
}

// sameMembers compares two rosters as sets; the engine lists them in no fixed
// order.
func sameMembers(a, b [][]byte) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.SortFunc(a, bytes.Compare)
	slices.SortFunc(b, bytes.Compare)
	return slices.EqualFunc(a, b, bytes.Equal)
}
