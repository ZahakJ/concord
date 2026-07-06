package app

import (
	"context"
	"testing"
	"time"

	"github.com/zahak/concord/internal/domain"
)

// TestUnauthorizedCommitRejected is the foundational acceptance test for the
// relaxed-committer governance gate: a non-owner member who crafts and publishes
// a membership commit (here, removing another member) must NOT be able to change
// the group. Honest peers resolve the commit's MLS author, see it isn't an
// authorized committer, and drop it — so the targeted member stays in the guild
// and the attacker only desyncs itself.
func TestUnauthorizedCommitRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)  // A: guild owner, the only authorized committer
	attacker := startService(t, ctx) // B: a member who will try to kick C
	victim := startService(t, ctx)   // C: the member B tries to remove

	ra, rc := &recorder{}, &recorder{}
	owner.OnMessage(ra.add)
	victim.OnMessage(rc.add)

	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, _ := owner.InviteCode(g.ID)
	if _, err := attacker.JoinViaInvite(code); err != nil {
		t.Fatalf("attacker join: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, owner, attacker)
	if _, err := victim.JoinViaInvite(code); err != nil {
		t.Fatalf("victim join: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, owner, attacker, victim)

	groupID := owner.Guilds()[0].GroupID

	// The attacker (a non-owner member) authors a valid MLS commit removing the
	// victim, then publishes it on the control topic exactly as the owner would.
	// This desyncs the attacker's own state to a 2-member epoch.
	rmCommit, err := attacker.mls.Remove(attacker.ctx, groupID, victim.PublicKey())
	if err != nil {
		t.Fatalf("attacker craft removal: %v", err)
	}
	if err := attacker.ps.Publish(attacker.ctx, domain.ControlTopicID(groupID), rmCommit); err != nil {
		t.Fatalf("attacker publish: %v", err)
	}

	// Give the malicious commit ample time to propagate and (if the gate were
	// missing) be applied.
	time.Sleep(3 * time.Second)

	// The owner and victim must still see all three members — the removal was
	// refused, not applied.
	if n, _ := owner.MemberCount(g.ID); n != 3 {
		t.Fatalf("owner member count = %d after unauthorized removal, want 3 (gate failed)", n)
	}
	if n, _ := victim.MemberCount(g.ID); n != 3 {
		t.Fatalf("victim member count = %d, want 3 (victim was wrongly removed)", n)
	}

	// Decisive check: the owner sends a fresh message and the victim can still
	// decrypt it — proof the victim remains in the ratchet at the shared epoch.
	sendUntilReceived(t, owner, channel, "you are still here", rc)
}
