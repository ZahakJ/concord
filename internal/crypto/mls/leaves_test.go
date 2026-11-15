package mls

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func newTestEngine(t *testing.T, id string) (Engine, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New([]byte(id), priv)
	if err != nil {
		t.Fatal(err)
	}
	return e, pub
}

func TestLeavesReadFromTree(t *testing.T) {
	ctx := context.Background()
	a, apub := newTestEngine(t, "alice")
	b, bpub := newTestEngine(t, "bob")
	c, cpub := newTestEngine(t, "carol")
	gid, _ := a.CreateGroup(ctx)
	kb, _ := b.KeyPackage(ctx)
	cm, w, err := a.Invite(ctx, gid, kb)
	if err != nil {
		t.Fatal(err)
	}
	_ = cm
	if _, err := b.Join(ctx, w); err != nil {
		t.Fatal(err)
	}

	// An honest batch add: one Add, the committer's own unchanged path leaf.
	kc, _ := c.KeyPackage(ctx)
	before, _ := b.Leaves(ctx, gid)
	cm2, w2, _, err := a.AddMembers(ctx, gid, [][]byte{kc})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := b.ReadCommitLeaves(ctx, gid, cm2)
	if err != nil || cl == nil {
		t.Fatalf("ReadCommitLeaves: %v %v", cl, err)
	}
	if len(cl.Added) != 1 || !bytes.Equal(cl.Added[0].Credential, []byte("carol")) || !bytes.Equal(cl.Added[0].Key, cpub) {
		t.Fatalf("added: %+v", cl.Added)
	}
	if len(cl.Removed) != 0 || cl.ByReference || cl.External {
		t.Fatalf("unexpected: %+v", cl)
	}
	for _, r := range cl.Replaced {
		var prior Leaf
		for _, l := range before {
			if l.Index == r.Index {
				prior = l
			}
		}
		if !bytes.Equal(prior.Credential, r.Credential) || !bytes.Equal(prior.Key, r.Key) {
			t.Fatalf("an honest path leaf differs from the leaf it replaces: %+v vs %+v", r, prior)
		}
	}
	if err := b.ApplyCommit(ctx, gid, cm2); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Join(ctx, w2); err != nil {
		t.Fatal(err)
	}
	// A commit for an epoch we already left reads as nothing.
	if cl, err := b.ReadCommitLeaves(ctx, gid, cm2); err != nil || cl != nil {
		t.Fatalf("stale commit read as %+v, %v", cl, err)
	}

	leaves, _ := c.Leaves(ctx, gid)
	keys := map[string][]byte{}
	for _, l := range leaves {
		keys[string(l.Credential)] = l.Key
	}
	if !bytes.Equal(keys["alice"], apub) || !bytes.Equal(keys["bob"], bpub) || !bytes.Equal(keys["carol"], cpub) {
		t.Fatalf("leaves: %+v", leaves)
	}
	own, _ := c.OwnLeaf(ctx, gid)
	ml, _ := c.MemberLeaves(ctx, gid)
	if !bytes.Equal(ml[own], []byte("carol")) {
		t.Fatalf("own leaf %d maps to %q", own, ml[own])
	}

	// Decrypt names the sending leaf and its key.
	ct, _ := a.Encrypt(ctx, gid, []byte("hi"))
	m, err := c.Decrypt(ctx, gid, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.SenderKey, apub) || !bytes.Equal(m.SenderLeafID, []byte("alice")) || !bytes.Equal(m.SenderID, []byte("alice")) {
		t.Fatalf("sender: %+v", m)
	}

	// RemoveLeaf: refuses a key that is not the one at that index, refuses
	// this member's own leaf, and removes exactly the leaf named.
	var bobIdx uint32
	for _, l := range leaves {
		if bytes.Equal(l.Credential, []byte("bob")) {
			bobIdx = l.Index
		}
	}
	if _, err := a.RemoveLeaf(ctx, gid, bobIdx, cpub); !errors.Is(err, ErrLeafChanged) {
		t.Fatalf("wrong key: %v", err)
	}
	aIdx, _ := a.OwnLeaf(ctx, gid)
	if _, err := a.RemoveLeaf(ctx, gid, aIdx, apub); err == nil {
		t.Fatal("removed its own leaf")
	}
	rm, err := a.RemoveLeaf(ctx, gid, bobIdx, bpub)
	if err != nil {
		t.Fatal(err)
	}
	cl, err = c.ReadCommitLeaves(ctx, gid, rm)
	if err != nil || cl == nil || len(cl.Removed) != 1 || cl.Removed[0] != bobIdx {
		t.Fatalf("removal read as %+v, %v", cl, err)
	}
	if err := c.ApplyCommit(ctx, gid, rm); err != nil {
		t.Fatal(err)
	}
	after, _ := c.Leaves(ctx, gid)
	if len(after) != 2 {
		t.Fatalf("leaves after removal: %+v", after)
	}
}
