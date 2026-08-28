package app

import (
	"context"
	"testing"
	"time"
)

// TestKickSticksAgainstTheReAddHeal is the regression for the worst thing a
// moderation tool can do: report success and then quietly undo itself.
//
// The failure it pins had nothing to do with the roster being rebuilt from
// message senders — it never is. What happened was this: the MLS Remove commit
// landed correctly, but the one peer that cannot apply a commit removing its own
// leaf is the peer being removed. Its client therefore saw a guild whose traffic
// had stopped decrypting, concluded (correctly, on the evidence it had) that it
// was stranded, and ran the re-add heal — which sends the SAME request a joiner
// sends, to the same door, where the only membership question asked was "are
// they banned". Within one heal beat they were a member again, and their next
// message decrypted because by then it was true.
//
// So this test does not check that the commit was minted. It checks that the
// guild still refuses them a minute later, with the heal loop running the whole
// time, and that nothing they say arrives.
func TestKickSticksAgainstTheReAddHeal(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	member := startService(t, ctx)
	// A third member, who is not going anywhere. Two peers would leave the owner
	// alone in its own guild the moment the kick lands, and a guild of one is a
	// different situation with different rules — this test is about the ordinary
	// one, where the room carries on without the person who was removed.
	bystander := startService(t, ctx)

	seen := &recorder{}
	owner.OnMessage(seen.add)
	alsoSeen := &recorder{}
	bystander.OnMessage(alsoSeen.add)

	g, err := owner.CreateGuild("Riverside Makers")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	if _, err := bystander.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite (bystander): %v", err)
	}
	waitMembers(t, 30*time.Second, 3, owner, member, bystander)
	channel := g.Channels[0].ID
	memberFpr := member.id.Fingerprint()

	// Baseline: they really are in, and what they say really does arrive. Without
	// this the assertions below would pass on a guild that never worked.
	sendUntilReceived(t, member, channel, "hello before the kick", seen)

	if err := owner.KickMember(g.ID, memberFpr, ""); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	if !owner.isRemoved(g.ID, memberFpr) {
		t.Fatal("the kick wrote no governance record — the admission gate has nothing to consult")
	}
	waitUntil(t, 20*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 2
	}, "the MLS removal never took effect on the owner")

	// The heal beat is 20s; give it two clear passes with the guild's traffic
	// unreadable on the far side, which is exactly the condition that used to
	// route the re-add.
	member.healStrandedGuilds()
	member.recoverOutOfSync(g.ID)
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if n, _ := owner.MemberCount(g.ID); n != 2 {
			t.Fatalf("the kicked member is back in the group after %s", time.Until(deadline))
		}
		time.Sleep(500 * time.Millisecond)
	}

	// And they cannot talk their way back in. The send may fail locally or
	// succeed locally; what must not happen either way is the words arriving.
	_, _ = member.SendMessage(channel, "I am still here", "", "")
	time.Sleep(3 * time.Second)
	if seen.has("I am still here") {
		t.Fatal("a message from a removed member was delivered and rendered to the owner")
	}
	// And not on an ordinary member either: the drop is every honest client's,
	// not the moderator's private view.
	if alsoSeen.has("I am still here") {
		t.Fatal("a message from a removed member was delivered and rendered to a bystander")
	}
	if n, _ := owner.MemberCount(g.ID); n != 2 {
		t.Fatalf("owner's member count is %d after a removed member spoke, want 2", n)
	}
	if n, _ := bystander.MemberCount(g.ID); n != 2 {
		t.Fatalf("a bystander's member count is %d, want 2", n)
	}

	// The kicked device knows, and says so rather than spinning. This is the
	// difference between a moderation decision and a mystery.
	waitUntil(t, 20*time.Second, func() bool {
		return member.EvictedFrom(g.ID) == evictedKicked
	}, "the kicked member's own client never learned it had been removed")
	if member.OutOfSync(g.ID) {
		t.Fatal("a removed guild is flying the catching-up banner as well as the terminal one")
	}

	// A deliberate re-invite still works — that is the whole difference between
	// a kick and a ban, and a tombstone with no lever would have erased it.
	if err := owner.ReadmitMember(g.ID, memberFpr); err != nil {
		t.Fatalf("ReadmitMember: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool { return !owner.isRemoved(g.ID, memberFpr) }, "readmit did not lift the removal")
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("a readmitted member could not rejoin: %v", err)
	}
	waitMembers(t, 40*time.Second, 3, owner)
	if member.EvictedFrom(g.ID) != "" {
		t.Fatal("rejoining did not clear the terminal state")
	}
	sendUntilReceived(t, member, channel, "back by invitation", seen)
}

// TestBannedMemberIsToldAndStaysOut is the same shape for a ban, plus the half
// the ban never had: the person on the receiving end used to see a fully live
// guild and type into nothing.
func TestBannedMemberIsToldAndStaysOut(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	member := startService(t, ctx)
	seen := &recorder{}
	owner.OnMessage(seen.add)

	g, err := owner.CreateGuild("Riverside Makers")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, owner, member)
	channel := g.Channels[0].ID
	sendUntilReceived(t, member, channel, "hello before the ban", seen)

	if err := owner.BanMember(g.ID, member.id.Fingerprint(), ""); err != nil {
		t.Fatalf("BanMember: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 1
	}, "the ban's removal never took effect")

	// From the commit alone, all the banned device can honestly know is that it
	// was removed: whether the guild also barred it is governance state it can no
	// longer decrypt. So that is what it says at first, and it is true.
	waitUntil(t, 30*time.Second, func() bool {
		return member.EvictedFrom(g.ID) != ""
	}, "the banned member's client never learned it was out of the guild")

	// The stronger word arrives the moment they try to come back — which is the
	// only moment it changes what they should do.
	if _, err := member.JoinViaInvite(code); err == nil {
		t.Fatal("a banned member rejoined with the invite code")
	}
	if got := member.EvictedFrom(g.ID); got != evictedBanned {
		t.Fatalf("after a refused rejoin the client says %q, want %q", got, evictedBanned)
	}

	_, _ = member.SendMessage(channel, "let me back in", "", "")
	time.Sleep(2 * time.Second)
	if seen.has("let me back in") {
		t.Fatal("a message from a banned member was delivered")
	}
	// A ban is not liftable by a readmit — that is what makes the two words mean
	// different things.
	if err := owner.ReadmitMember(g.ID, member.id.Fingerprint()); err == nil {
		t.Fatal("ReadmitMember let a banned member back in")
	}
}
