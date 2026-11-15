package app

import (
	"context"
	"testing"
	"time"
)

// A 1:1 DM started with an account on two devices must seat both of them,
// without either device restarting. The starter's invite reaches one of
// them; the other gets the DM from its sibling's offer, which is a code
// addressed at the starter. Over loopback the sibling used to find no address
// of the starter's it would hand on, left the DM out of the offer, and the
// second device was never seated.
func TestDMStartedWithATwoDeviceAccountSeatsBothDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	rp := &recorder{}
	w.phone.OnMessage(rp.add)
	// Carol must be able to reach Bob's account: share a guild first.
	w.guildOf(t, w.alice, w.bob, w.phone, w.carol)
	dm, err := w.carol.StartDM(w.bob.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	waitDMSeated(t, w, dm.ID, 90*time.Second)
	if !delivered(w.carol, dm.Channels[0].ID, "to both devices", 30*time.Second, w.rb, rp) {
		t.Fatalf("Carol's message did not reach both of Bob's devices (desktop %v, phone %v)",
			w.rb.has("to both devices"), rp.has("to both devices"))
	}
}

// The same when Bob's phone is offline while the DM forms: Carol's invite
// seats the desktop and is done with, so the phone, coming online later, gets
// the DM only from the desktop's offer, and must still be seated.
func TestDMReachesASecondDeviceThatComesOnlineLater(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	w.guildOf(t, w.alice, w.bob, w.phone, w.carol)
	_ = w.phone.Close()
	dm, err := w.carol.StartDM(w.bob.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 90*time.Second, func() bool {
		return memberCount(w.carol, dm.ID) == 2 && memberCount(w.bob, dm.ID) == 2
	}, "setup: the DM never held Carol and Bob's desktop")

	w.phone = startServiceOn(t, ctx, w.phoneDir, w.boot)
	rp := &recorder{}
	w.phone.OnMessage(rp.add)
	waitDMSeated(t, w, dm.ID, 90*time.Second)
	if !delivered(w.carol, dm.Channels[0].ID, "to the late phone", 30*time.Second, rp) {
		t.Fatal("Carol's message did not reach the phone that came online later")
	}
}

func waitDMSeated(t *testing.T, w *unlinkWorld, dmID string, within time.Duration) {
	t.Helper()
	seated := func() bool {
		return memberCount(w.carol, dmID) == 3 && memberCount(w.bob, dmID) == 3 && memberCount(w.phone, dmID) == 3
	}
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) && !seated() {
		time.Sleep(500 * time.Millisecond)
	}
	if !seated() {
		t.Fatalf("the DM never held Carol and both of Bob's devices (leaves seen by carol=%d desktop=%d phone=%d)",
			memberCount(w.carol, dmID), memberCount(w.bob, dmID), memberCount(w.phone, dmID))
	}
}

// An offer to our own device that had to leave a guild out (no address of the
// guild's committer to hand on) is made again, and once an address is known
// the device gets the guild, with neither device reconnecting.
func TestAnOfferThatLeftAGuildOutIsMadeAgain(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	cy := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := cy.SetDisplayName("Cy"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := cy.CreateGuild("Cy's Den")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := cy.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}

	withholdOwnerCodes.Store(true)
	t.Cleanup(func() { withholdOwnerCodes.Store(false) })
	if _, err := desk.JoinViaInvite(code); err != nil {
		t.Fatalf("desk JoinViaInvite: %v", err)
	}
	// The offer the join makes goes out now, without Cy's guild in it.
	time.Sleep(2 * time.Second)
	if holdsGuild(phone, g.ID) {
		t.Fatal("setup: the phone got the guild from an offer that should have left it out")
	}
	withholdOwnerCodes.Store(false)
	waitUntil(t, 60*time.Second, func() bool { return holdsGuild(phone, g.ID) },
		"the phone never got the guild its first offer left out")
}
