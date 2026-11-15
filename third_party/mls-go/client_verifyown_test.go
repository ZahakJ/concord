package mls

import (
	"bytes"
	"context"
	"testing"

	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/schedule"
)

// VerifyOwnCommit accepts only this client's own leaf and key; a leaf that
// merely carries the same credential bytes is not accepted.
func TestVerifyOwnCommitWantsOwnLeafAndKey(t *testing.T) {
	ctx := context.Background()
	cs := ciphersuite.MLS128DHKEMP256
	alice, _ := NewClient([]byte("alice"), cs)
	bob, _ := NewClient([]byte("bob"), cs)
	second, err := NewClient([]byte("alice"), cs) // alice's credential bytes, its own key
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := alice.CreateGroup(ctx)
	kp, _ := bob.FreshKeyPackageBytes(ctx)
	_, w, err := alice.InviteMember(ctx, gid, kp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.JoinGroup(ctx, w); err != nil {
		t.Fatal(err)
	}
	skp, _ := second.FreshKeyPackageBytes(ctx)
	c1, w2, err := bob.InviteMember(ctx, gid, skp)
	if err != nil {
		t.Fatalf("bob adds the second leaf: %v", err)
	}
	if err := alice.ProcessCommit(ctx, gid, c1); err != nil {
		t.Fatalf("setup: alice did not apply the add: %v", err)
	}
	if _, err := second.JoinGroup(ctx, w2); err != nil {
		t.Fatal(err)
	}

	// A commit from the second leaf that alice cannot apply.
	pre := loadGroupForTest(t, second, gid)
	mk := append([]byte{}, pre.EpochSecrets().MembershipKey.AsSlice()...)
	gc := append([]byte{}, pre.GroupContext().Marshal()...)
	commit, err := second.SelfUpdate(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := framing.UnmarshalMLSMessage(commit)
	pm, _ := msg.AsPublic()
	c := *pm
	c.Auth.ConfirmationTag = append([]byte{}, pm.Auth.ConfirmationTag...)
	c.Auth.ConfirmationTag[0] ^= 0xff
	ac := &framing.AuthenticatedContent{WireFormat: framing.WireFormatPublicMessage, Content: c.Content, Auth: c.Auth, GroupContext: gc}
	tbm := append(append([]byte{}, ac.MarshalTBS()...), c.Auth.Marshal(c.Content.ContentType())...)
	c.MembershipTag = schedule.ComputeMembershipTag(cs, mk, tbm)
	other := framing.NewMLSMessagePublic(&c).Marshal()

	members, _ := alice.ListMembers(ctx, gid)
	var ident []byte
	for _, m := range members {
		if m.LeafIndex == c.Content.Sender.LeafIndex {
			ident = m.Identity
		}
	}
	if !bytes.Equal(ident, []byte("alice")) {
		t.Fatalf("setup: the sender leaf's identity is %q, want alice's", ident)
	}
	if err := alice.VerifyCommit(ctx, gid, other); err != nil {
		t.Fatalf("setup: the commit does not pass VerifyCommit: %v", err)
	}
	if err := alice.VerifyOwnCommit(ctx, gid, other); err == nil {
		t.Fatal("a commit from another leaf with alice's credential passed VerifyOwnCommit")
	}
	if err := alice.ProcessCommit(ctx, gid, other); err == nil {
		t.Fatal("setup: the commit applied")
	}
}
