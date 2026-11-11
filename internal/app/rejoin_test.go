package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
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

// TestRepastingACodeAsAHealthyMemberCommitsNothing pins the probe a seated
// member's paste now sends: the committer is asked, answers "already a
// member", and nothing moves — no epoch on either side, no join line anywhere.
func TestRepastingACodeAsAHealthyMemberCommitsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	ownerSeen, memberSeen := &recorder{}, &recorder{}
	owner.OnMessage(ownerSeen.add)
	member.OnMessage(memberSeen.add)
	g, err := owner.CreateGuild("Still Here")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)
	waitUntil(t, 10*time.Second, func() bool {
		return rejoinLines(ownerSeen, "has joined the guild") == 1 && rejoinLines(memberSeen, "has joined the guild") == 1
	}, "the first join line never arrived")
	ownerEpoch, _ := owner.mls.Epoch(ctx, g.GroupID)
	memberEpoch, _ := member.mls.Epoch(ctx, g.GroupID)

	// The committer's side of it, asked directly: a probe from a live leaf is
	// answered as a member, with no Welcome.
	kp, err := member.mls.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("KeyPackage: %v", err)
	}
	req, _ := json.Marshal(inviteRequest{GuildID: g.ID, KeyPackage: kp, Credential: member.myCredential, Probe: true})
	raw, err := owner.handleInviteRequest(ctx, member.host.PeerID(), req)
	if err != nil {
		t.Fatalf("handleInviteRequest: %v", err)
	}
	var resp inviteResponse
	if err := json.Unmarshal(raw, &resp); err != nil || !resp.Member || len(resp.Welcome) != 0 || resp.Error != "" {
		t.Fatalf("probe answered member=%v welcome=%d bytes error=%q (%v), want a member answer and no Welcome", resp.Member, len(resp.Welcome), resp.Error, err)
	}

	for i := 0; i < 2; i++ {
		again, err := member.JoinViaInvite(code)
		if err != nil || again.ID != g.ID {
			t.Fatalf("re-paste %d returned %q, %v", i, again.ID, err)
		}
	}
	time.Sleep(3 * time.Second)
	if e, _ := owner.mls.Epoch(ctx, g.GroupID); e != ownerEpoch {
		t.Fatalf("re-pasting moved the owner's epoch from %d to %d", ownerEpoch, e)
	}
	if e, _ := member.mls.Epoch(ctx, g.GroupID); e != memberEpoch {
		t.Fatalf("re-pasting moved the member's epoch from %d to %d", memberEpoch, e)
	}
	if n, m := rejoinLines(ownerSeen, "has joined the guild"), rejoinLines(memberSeen, "has joined the guild"); n != 1 || m != 1 {
		t.Fatalf("join lines after re-pasting: owner %d, member %d, want 1 and 1", n, m)
	}
}

// TestARepasteAfterAnUnnoticedKickIsRefused pins the case the "already a
// member" shortcut got wrong: a member kicked a moment ago, whose device has
// not heard about it yet, pastes a fresh code. It used to be told it was in;
// the committer now gets asked and says it was removed, and the device records
// that the way any refused join does.
func TestARepasteAfterAnUnnoticedKickIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g, err := owner.CreateGuild("Door")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)

	// Keep the kick's commit from reaching the member, so the paste below is
	// made by a device that still believes it is seated.
	member.ps.Unsubscribe(domain.ControlTopicID(g.GroupID))
	if err := owner.KickMember(g.ID, member.Fingerprint(), ""); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 1
	}, "the kicked member was not evicted")
	epoch, _ := owner.mls.Epoch(ctx, g.GroupID)
	if _, healthy := member.healthyMembership(g.ID); !healthy {
		t.Fatal("the member already knew it was kicked; the case under test did not arise")
	}

	code2, _ := owner.InviteCode(g.ID)
	_, err = member.JoinViaInvite(code2)
	if err == nil || !strings.Contains(err.Error(), removedRefusal) {
		t.Fatalf("re-paste after an unnoticed kick returned %v, want the removal refusal", err)
	}
	if got := member.EvictedFrom(g.ID); got != evictedKicked {
		t.Fatalf("member recorded eviction %q, want %q", got, evictedKicked)
	}
	if n, _ := owner.MemberCount(g.ID); n != 1 {
		t.Fatalf("owner has %d members after the re-paste, want 1", n)
	}
	if e, _ := owner.mls.Epoch(ctx, g.GroupID); e != epoch {
		t.Fatalf("the refused re-paste moved the owner's epoch from %d to %d", epoch, e)
	}
}

// TestAProbeAnsweredByAnOlderCommitterStillSeatsUs pins what happens when the
// committer predates the probe and serves it as a join: it removes the live
// leaf and re-adds it (two commits, as every re-paste cost before), so the
// Welcome must be joined or the device would be left out — but the member does
// not announce a second arrival.
func TestAProbeAnsweredByAnOlderCommitterStillSeatsUs(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	seen := &recorder{}
	owner.OnMessage(seen.add)
	g, err := owner.CreateGuild("Old Door")
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

	ignoreProbes.Store(true)
	defer ignoreProbes.Store(false)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("re-paste against an older committer: %v", err)
	}
	if e, _ := owner.mls.Epoch(ctx, g.GroupID); e != epoch+2 {
		t.Fatalf("owner epoch %d after the older committer's answer, want %d (remove + re-add)", e, epoch+2)
	}
	waitUntil(t, 20*time.Second, func() bool {
		me, _ := member.mls.Epoch(ctx, g.GroupID)
		oe, _ := owner.mls.Epoch(ctx, g.GroupID)
		return me == oe && member.EvictedFrom(g.ID) == "" && !member.OutOfSync(g.ID)
	}, "the member was not re-seated at the owner's epoch")
	if _, err := member.SendMessage(g.Channels[0].ID, "still here", "", ""); err != nil {
		t.Fatalf("send after re-seat: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool { return seen.has("still here") }, "the re-seated member's message never reached the owner")
	if n := rejoinLines(seen, "has joined the guild"); n != 1 {
		t.Fatalf("the owner saw %d join lines, want 1", n)
	}
}
