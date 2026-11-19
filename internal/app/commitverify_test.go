package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/thomas-vilte/mls-go/framing"
)

func reframeCommit(t *testing.T, commit []byte, sender uint32, epoch uint64) []byte {
	t.Helper()
	msg, err := framing.UnmarshalMLSMessage(commit)
	if err != nil {
		t.Fatal(err)
	}
	pub, ok := msg.AsPublic()
	if !ok {
		t.Fatal("not a public message")
	}
	pub.Content.Sender.LeafIndex = sender
	pub.Content.Epoch = epoch
	return framing.NewMLSMessagePublic(pub).Marshal()
}

// A removal commit whose signature does not check out evicts nobody.
func TestUnverifiedRemovalChangesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("networked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner, a, b := startService(t, ctx), startService(t, ctx), startService(t, ctx)
	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := a.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, owner, a)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 3, owner, a, b)
	gid := theGuild(t, owner).GroupID
	epoch, _ := a.mls.Epoch(ctx, gid)
	raw, err := b.mls.Remove(ctx, gid, a.myCredential)
	if err != nil {
		t.Fatal(err)
	}
	var bad []byte
	for i := uint32(0); i < 8 && bad == nil; i++ {
		c := reframeCommit(t, raw, i, epoch)
		if who, err := a.mls.CommitSender(ctx, gid, c); err == nil && bytes.Equal(who, owner.myCredential) {
			bad = c
		}
	}
	if bad == nil {
		t.Fatal("setup: no leaf of the owner's")
	}
	if a.mls.VerifyCommit(ctx, gid, bad) == nil {
		t.Fatal("setup: the reframed commit verifies")
	}
	_ = b.ps.Publish(ctx, domain.ControlTopicID(gid), bad)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if v := a.EvictedFrom(g.ID); v != "" {
			t.Fatalf("recorded itself removed (%q) on a commit that does not verify", v)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e, _ := a.mls.Epoch(ctx, gid); e != epoch {
		t.Fatalf("epoch moved: %d -> %d", epoch, e)
	}
	if _, err := owner.CreateChannel(g.ID, "after", "text", ""); err != nil {
		t.Fatal(err)
	}
}
