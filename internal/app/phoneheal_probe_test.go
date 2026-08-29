package app

// Probe: the fix's verification claims "with two owner devices + a friend,
// BOTH owner devices are authorized committers (device certs resolve to the
// owner fingerprint), so a committer was reachable in this topology". The
// first probe run showed the friend holds NO connection and NO addresses for
// the phone: authorizedCommittersOnline listed only the desk, and a heal
// aimed at the phone died with "no addresses".
//
// TestDriftRecoveryWithDeskOffline plays that hand out: the desk (the only
// committer the friend can reach) goes offline, the phone — a legitimate
// committer — advances the epoch with a commit the friend never receives,
// then messages. The fix's whole recovery ladder (stash, sync-bridge, re-add
// heal) now depends on the friend reaching the PHONE. If the friend never
// connects to it, the guild is stranded for as long as the desk stays asleep —
// the user's desktop being asleep is the normal state of a desktop.

import (
	"context"
	"testing"
	"time"
)

func TestDriftRecoveryWithDeskOffline(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	guildID := theGuild(t, desk).ID

	friend := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := friend.SetDisplayName("Friend"); err != nil {
		t.Fatal(err)
	}
	code, err := desk.InviteCode(guildID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := desk.MemberCount(guildID)
		return n == 3
	}, "friend never joined")
	deliverAll(t, desk, textCh, "warmup", 60*time.Second, phone, friend)

	// The desktop goes to sleep.
	_ = desk.Close()
	time.Sleep(2 * time.Second)

	connected := func() bool {
		return len(friend.host.Libp2p().Network().ConnsToPeer(phone.host.PeerID())) > 0
	}
	t.Logf("friend connected to phone right after desk sleeps: %v", connected())

	// The phone — an authorized committer — advances the epoch, and the friend
	// misses the commit (the exact lost-frame drill the fix's tests use, with
	// the phone in the committer seat instead of the desk).
	loseOneCommit(t, ctx, phone, guildID)

	// The phone speaks. This is the fix's headline scenario minus the desk.
	t0 := time.Now()
	if _, err := phone.SendMessage(textCh, "phone speaks while the desk sleeps", "", ""); err != nil {
		t.Fatalf("phone SendMessage: %v", err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if sees(friend, textCh, "phone speaks while the desk sleeps") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sees(friend, textCh, "phone speaks while the desk sleeps") {
		t.Errorf("friend never received the phone's message within 3 minutes of the desk sleeping; "+
			"friend->phone connected=%v, friend committers online=%d, friend outOfSync=%v",
			connected(), len(friend.authorizedCommittersOnline(guildID)), friend.OutOfSync(guildID))
	} else {
		t.Logf("friend received the phone's message %v after send (connected to phone: %v)",
			time.Since(t0), connected())
	}
}
