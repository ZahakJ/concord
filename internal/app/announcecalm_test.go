package app

import (
	"context"
	"testing"
	"time"
)

// TestProfileAnnounceSkipsUnchangedBytes pins the reconnect-churn fix. The
// announce carries the whole profile — an avatar and a profile banner, up to
// 320 KiB of base64 — MLS-encrypted and flooded to the guild mesh, and it fires
// on peer connect, once per guild. A member who drops and redials therefore
// cost every other member that quarter-megabyte again, times the number of
// guilds the two share, for news nobody was waiting on.
func TestProfileAnnounceSkipsUnchangedBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	if err := a.SetProfile(Profile{Name: "avicenna", Avatar: image(40 << 10)}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	g, err := a.CreateGuild("hamadan")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	// The first announce to a guild always speaks: nobody there has heard us.
	if !a.publishProfile(g.ID, false) {
		t.Fatal("the first announce to a guild said nothing")
	}
	// A reconnect (or forty) must not repeat it.
	for i := range 40 {
		if a.publishProfile(g.ID, false) {
			t.Fatalf("re-announced an unchanged profile on reconnect %d", i)
		}
	}
	// A real edit must still travel — SetProfile announces it itself, which is
	// exactly why the reconnect above had nothing to say.
	a.mu.RLock()
	before := a.announcedProfile[g.ID].stamp
	a.mu.RUnlock()
	if err := a.SetProfile(Profile{Name: "avicenna", Avatar: image(50 << 10)}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	a.mu.RLock()
	after := a.announcedProfile[g.ID].stamp
	a.mu.RUnlock()
	if after == before {
		t.Fatal("a changed avatar was never announced")
	}
	if a.publishProfile(g.ID, false) {
		t.Fatal("the changed avatar was announced a second time on the next reconnect")
	}
	// And a member we have just met gets told regardless — they were not there
	// for the announce above.
	if !a.publishProfile(g.ID, true) {
		t.Fatal("a forced announce (a newly met member) stayed silent")
	}
}

// TestProfileAnnounceStillReachesAMemberWhoMissedIt is the other half, and it
// is the half that broke first: skipping on content alone silently dropped the
// only fast path a friend has after being offline for an edit. A publish is a
// broadcast — it reaches whoever is subscribed at that instant and nobody else —
// so "we already said this" is only an answer for the members who were there.
// Without this, that friend waited for the next anti-entropy beat (a minute)
// instead of a second, which is what the profile-sync integration test caught.
func TestProfileAnnounceStillReachesAMemberWhoMissedIt(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("hamadan")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)

	a.publishProfile(g.ID, true) // baseline: say it once, with B connected
	bPeer := b.host.PeerID()
	a.mu.RLock()
	heard := a.announcedProfile[g.ID].heard[bPeer]
	a.mu.RUnlock()
	if !heard {
		t.Fatal("a connected member was not recorded as having heard the announce")
	}
	if a.publishProfile(g.ID, false) {
		t.Error("re-announced the same profile to a member who had already heard it")
	}
	// Now the case that matters: the same member, connected, who was NOT there
	// when we last spoke.
	a.mu.Lock()
	delete(a.announcedProfile[g.ID].heard, bPeer)
	a.mu.Unlock()
	if !a.publishProfile(g.ID, false) {
		t.Error("a connected member who never heard our current profile was told nothing")
	}
}

// TestPendingInviteRepushBacksOff: a pending member who never accepts used to
// be handed the invite again on every heal tick — an encrypted stream and a
// wake-up for the other end, three times a minute, forever.
func TestPendingInviteRepushBacksOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	g, err := a.CreateGuild("hamadan")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	const fpr = "SOME PENDING MEMBER"
	a.addPending(g.ID, fpr)

	now := time.Now()
	if !a.pendingPushDue(g.ID, fpr, now) {
		t.Fatal("the first push to a reachable pending member was withheld")
	}
	// The heal loop beats every 20s and the reconcile cadence every 60s; neither
	// may push again inside the floor.
	for _, after := range []time.Duration{20 * time.Second, 60 * time.Second, pendingRepushEvery - time.Second} {
		if a.pendingPushDue(g.ID, fpr, now.Add(after)) {
			t.Errorf("re-pushed the same invite %s later, inside the %s floor", after, pendingRepushEvery)
		}
	}
	if !a.pendingPushDue(g.ID, fpr, now.Add(pendingRepushEvery+time.Second)) {
		t.Error("a pending member stopped being retried altogether")
	}
	// Somebody else's invite is on its own clock.
	if !a.pendingPushDue(g.ID, "ANOTHER PENDING MEMBER", now) {
		t.Error("one member's backoff silenced another's first push")
	}
}

// TestEventAnnounceLoopIsPaced: every periodic loop stretches to the shared
// background beat on a phone, because the cost of these sweeps is not the work,
// it is waking the radio and the CPU. This one escaped the sweep and kept a
// backgrounded phone busy every thirty seconds forever.
func TestEventAnnounceLoopIsPaced(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	if got := a.bgPace(eventAnnounceTick); got != eventAnnounceTick {
		t.Fatalf("foreground pace is %s, want %s", got, eventAnnounceTick)
	}
	a.SetBackground(true)
	defer a.SetBackground(false)
	if got := a.bgPace(eventAnnounceTick); got != backgroundBeat {
		t.Errorf("backgrounded, the event sweep still runs every %s instead of the %s beat", got, backgroundBeat)
	}
}
