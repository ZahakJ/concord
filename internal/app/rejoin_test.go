package app

import (
	"context"
	"testing"
	"time"
)

// TestRejoiningAGuildYouAreInIsANoOp pins pasting a code for a guild you are
// already in: it used to remove and re-add your leaf and post a second "has
// joined the guild" to every member.
func TestRejoiningAGuildYouAreInIsANoOp(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	seen := &recorder{}
	owner.OnMessage(seen.add)
	g, err := owner.CreateGuild("Once Only")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)
	waitUntil(t, 10*time.Second, func() bool { return rejoinLines(seen, "has joined the guild") == 1 }, "the first join line never arrived")
	epoch, _ := owner.mls.Epoch(ctx, g.GroupID)

	again, err := member.JoinViaInvite(code)
	if err != nil || again.ID != g.ID {
		t.Fatalf("rejoining returned %q, %v", again.ID, err)
	}
	time.Sleep(3 * time.Second)
	if n := rejoinLines(seen, "has joined the guild"); n != 1 {
		t.Fatalf("the owner saw %d join lines, want 1", n)
	}
	if e, _ := owner.mls.Epoch(ctx, g.GroupID); e != epoch {
		t.Fatalf("rejoining moved the epoch from %d to %d", epoch, e)
	}
}

func rejoinLines(r *recorder, content string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, m := range r.msgs {
		if m.Content == content {
			n++
		}
	}
	return n
}
