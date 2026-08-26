package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
)

// ghostAdmissions advances a guild by n published commits, each admitting a
// throwaway member. It is the shape of ordinary churn — people joining while
// somebody is away — compressed into a loop.
func ghostAdmissions(t *testing.T, ctx context.Context, owner *Service, guildID string, n int) {
	t.Helper()
	owner.mu.RLock()
	groupID := owner.guilds[guildID].GroupID
	owner.mu.RUnlock()
	for i := 0; i < n; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		ghost, err := mls.New(pub, priv)
		if err != nil {
			t.Fatalf("ghost engine: %v", err)
		}
		kp, err := ghost.KeyPackage(ctx)
		if err != nil {
			t.Fatalf("ghost key package: %v", err)
		}
		commit, _, err := owner.mls.Invite(ctx, groupID, kp)
		if err != nil {
			t.Fatalf("ghost invite %d: %v", i, err)
		}
		owner.logCommit(groupID, commit)
		_ = owner.ps.Publish(ctx, domain.ControlTopicID(groupID), commit)
		_ = ghost.Close()
	}
}

func epochOf(t *testing.T, s *Service, guildID string) uint64 {
	t.Helper()
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		t.Fatalf("guild %s is not tracked", guildID)
	}
	e, err := s.mls.Epoch(s.ctx, groupID)
	if err != nil {
		t.Fatalf("Epoch: %v", err)
	}
	return e
}

// TestPrunedCommitLogStillHeals is the safety proof for bounding the commit
// log. Trimming the log is only defensible if the member it strands has a way
// back, so this test makes the prune the CAUSE of a strand and then insists on
// the recovery: a member who was away long enough that the commits bridging its
// gap have been deleted must come back, be refused a backfill, and be re-added.
//
// Everything here is arranged so the prune is the only thing that can be
// blamed. The horizon is shrunk to one epoch for the duration; the member is
// genuinely offline while the guild churns; and the assertion is not merely
// "it converged" but "it converged the expensive way" — the owner's epoch moved
// by the two commits a re-add costs (a Remove and an Add), which nothing but
// the heal path produces.
func TestPrunedCommitLogStillHeals(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := owner.SetDisplayName("Owner"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := owner.CreateGuild("pruned")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID

	awayDir := t.TempDir()
	away := startServiceOn(t, ctx, awayDir, boot)
	if err := away.SetDisplayName("Away"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := away.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 2
	}, "the second member never joined")

	// Away goes offline, and the guild carries on without it.
	strandedAt := epochOf(t, away, g.ID)
	if err := away.Close(); err != nil {
		t.Fatalf("close away: %v", err)
	}
	ghostAdmissions(t, ctx, owner, g.ID, 6)

	owner.mu.RLock()
	groupID := owner.guilds[g.ID].GroupID
	owner.mu.RUnlock()

	// Before the prune the owner could have bridged the whole gap. This is the
	// control: without it, "it healed" would prove nothing about the prune.
	rows, err := owner.store.CommitsAfter(groupID, strandedAt)
	if err != nil {
		t.Fatalf("CommitsAfter: %v", err)
	}
	if !bridges(rows, strandedAt, epochOf(t, owner, g.ID)) {
		t.Fatal("the owner could not bridge the gap even BEFORE pruning — the test proves nothing")
	}

	// Shrink the horizon to the tightest legal setting and sweep. Anything
	// older than the last epoch and older than "now" goes, which is every
	// commit the absent member needs.
	keep, age := commitLogKeep, commitLogMaxAge
	commitLogKeep, commitLogMaxAge = 1, 0
	owner.pruneCommitLogs()
	commitLogKeep, commitLogMaxAge = keep, age

	rows, err = owner.store.CommitsAfter(groupID, strandedAt)
	if err != nil {
		t.Fatalf("CommitsAfter after prune: %v", err)
	}
	if bridges(rows, strandedAt, epochOf(t, owner, g.ID)) {
		t.Fatal("the prune left the gap bridgeable — the recovery below would not be the one under test")
	}

	epochBeforeHeal := epochOf(t, owner, g.ID)

	// Away comes back to a guild whose commit log can no longer explain the
	// intervening epochs.
	away = startServiceOn(t, ctx, awayDir, boot)
	waitUntil(t, 3*time.Minute, func() bool {
		return epochOf(t, away, g.ID) == epochOf(t, owner, g.ID) && !away.OutOfSync(g.ID)
	}, "the returning member never caught up with the guild")

	if got := epochOf(t, owner, g.ID); got != epochBeforeHeal+2 {
		t.Fatalf("the guild advanced %d -> %d; a re-add heal costs exactly two commits "+
			"(a Remove and an Add), so this converged some other way", epochBeforeHeal, got)
	}

	// And the ratchet really is whole: what the owner says now is readable.
	rec := &recorder{}
	away.OnMessage(rec.add)
	sendUntilReceived(t, owner, channel, "back-in-the-room", rec)
}
