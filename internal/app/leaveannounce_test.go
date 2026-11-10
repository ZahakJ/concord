package app

import (
	"context"
	"testing"
	"time"
)

// TestLeaverIsRemovedForEveryone pins the leave reaching the guild: the leaver
// drops out of everyone's roster (one committer takes the leaf out) and the
// room is told, instead of the leaver staying listed, online, indefinitely.
func TestLeaverIsRemovedForEveryone(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	leaver := startService(t, ctx)
	bystander := startService(t, ctx)
	seen := &recorder{}
	bystander.OnMessage(seen.add)

	g, err := owner.CreateGuild("Open Door")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	for _, s := range []*Service{leaver, bystander} {
		if _, err := s.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitMembers(t, 30*time.Second, 3, owner, leaver, bystander)
	sendUntilReceived(t, leaver, g.Channels[0].ID, "hello before I go", seen)

	if err := leaver.LeaveGuild(g.ID); err != nil {
		t.Fatalf("LeaveGuild: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		a, _ := owner.MemberCount(g.ID)
		b, _ := bystander.MemberCount(g.ID)
		return a == 2 && b == 2
	}, "the leaver is still in the roster on the members who stayed")
	if owner.isRemoved(g.ID, leaver.id.Fingerprint()) {
		t.Fatal("leaving wrote a kick into the log")
	}
	waitUntil(t, 10*time.Second, func() bool { return seen.has("left the guild") }, "nobody was told the member left")
}
