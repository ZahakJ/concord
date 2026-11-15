package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// unlinkWorld is Alice, Carol, and Bob on two devices (desktop and a linked
// phone), all on one rendezvous, each with a message recorder.
type unlinkWorld struct {
	ctx                      context.Context
	boot                     string
	alice, carol, bob, phone *Service
	ra, rc, rb               *recorder
	phoneKey, phoneDir       string
}

func newUnlinkWorld(t *testing.T, ctx context.Context) *unlinkWorld {
	t.Helper()
	w := &unlinkWorld{ctx: ctx, boot: testRendezvous(t, ctx)}
	w.alice = startServiceOn(t, ctx, t.TempDir(), w.boot)
	w.carol = startServiceOn(t, ctx, t.TempDir(), w.boot)
	w.bob = startServiceOn(t, ctx, t.TempDir(), w.boot)
	for svc, name := range map[*Service]string{w.alice: "Alice", w.carol: "Carol", w.bob: "Bob"} {
		if err := svc.SetDisplayName(name); err != nil {
			t.Fatal(err)
		}
	}
	code, err := w.bob.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	w.phoneDir = t.TempDir()
	if _, err := RedeemLink(ctx, w.phoneDir, code, "test-pass", ""); err != nil {
		t.Fatalf("link Bob's phone: %v", err)
	}
	w.phone = startServiceOn(t, ctx, w.phoneDir, w.boot)
	w.phoneKey = deviceKeyOf(t, w.phone)
	w.ra, w.rc, w.rb = &recorder{}, &recorder{}, &recorder{}
	w.alice.OnMessage(w.ra.add)
	w.carol.OnMessage(w.rc.add)
	w.bob.OnMessage(w.rb.add)
	return w
}

// guildOf has owner create a guild and the others join it in turn.
func (w *unlinkWorld) guildOf(t *testing.T, owner *Service, joiners ...*Service) domain.Guild {
	t.Helper()
	g, err := owner.CreateGuild("shared")
	if err != nil {
		t.Fatal(err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	all := []*Service{owner}
	for _, j := range joiners {
		if _, err := j.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
		all = append(all, j)
		waitMemberCount(t, 60*time.Second, g.ID, len(all), all...)
	}
	return g
}

// delivered sends content from sender into channel until every recorder has
// it, reporting false (not failing) if that does not happen in time.
func delivered(sender *Service, channel, content string, within time.Duration, rs ...*recorder) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		_, _ = sender.SendMessage(channel, content, "", "")
		all := true
		for _, r := range rs {
			if !r.has(content) {
				all = false
			}
		}
		if all {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func memberCount(s *Service, guildID string) int {
	n, _ := s.MemberCount(guildID)
	return n
}

// Bob, an ordinary member of Alice's guild (no manage-members), unlinks his
// phone. He can remove the phone's leaf only by a commit Alice and Carol
// refuse, so his desktop must not make that commit: it must stay in the
// guild with them, both ways, and report the guild as not done.
func TestUnlinkInAGuildBobCannotCommitInKeepsHimInIt(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	g := w.guildOf(t, w.alice, w.bob, w.phone, w.carol)
	ch := g.Channels[0].ID
	if !delivered(w.bob, ch, "before", 30*time.Second, w.ra, w.rc) {
		t.Fatal("setup: Bob's desktop could not reach the guild before the unlink")
	}

	_ = w.phone.Close()
	err := w.bob.UnlinkDevice(w.phoneKey)
	t.Logf("UnlinkDevice: %v", err)
	t.Logf("leaves after unlink: alice=%d carol=%d bob=%d", memberCount(w.alice, g.ID), memberCount(w.carol, g.ID), memberCount(w.bob, g.ID))

	bobToAlice := delivered(w.bob, ch, "bob after unlink", 30*time.Second, w.ra, w.rc)
	aliceToBob := delivered(w.alice, ch, "alice after unlink", 30*time.Second, w.rb)
	carolToBob := delivered(w.carol, ch, "carol after unlink", 30*time.Second, w.rb)
	t.Logf("after unlink: bob→alice,carol=%v alice→bob=%v carol→bob=%v", bobToAlice, aliceToBob, carolToBob)
	t.Logf("leaves later: alice=%d carol=%d bob=%d", memberCount(w.alice, g.ID), memberCount(w.carol, g.ID), memberCount(w.bob, g.ID))

	if !bobToAlice || !aliceToBob || !carolToBob {
		t.Errorf("Bob's desktop was cut out of a guild he cannot commit in by unlinking his phone (bob→others %v, alice→bob %v, carol→bob %v)", bobToAlice, aliceToBob, carolToBob)
	}
	var inc *UnlinkIncompleteError
	if !errors.As(err, &inc) || len(inc.Pending) != 1 || inc.Pending[0] != (UnlinkSpot{Kind: "guild", ID: g.ID, Name: g.Name}) || len(inc.Failed) != 0 {
		t.Errorf("UnlinkDevice did not report the guild as waiting for someone who can remove the phone: %v", err)
	}
	if n := memberCount(w.alice, g.ID); n != 4 {
		t.Errorf("Alice sees %d leaves, want 4 (the phone's leaf stays until someone who can commit removes it)", n)
	}
}

// When Bob owns the guild, the unlink removes the phone's leaf for everyone
// and Bob stays in it.
func TestUnlinkInAGuildBobOwnsRemovesThePhone(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	g := w.guildOf(t, w.bob, w.phone, w.alice, w.carol)
	ch := g.Channels[0].ID

	_ = w.phone.Close()
	if err := w.bob.UnlinkDevice(w.phoneKey); err != nil {
		t.Fatalf("UnlinkDevice in a guild Bob owns: %v", err)
	}
	waitMemberCount(t, 60*time.Second, g.ID, 3, w.alice, w.carol, w.bob)
	if !delivered(w.bob, ch, "owner after unlink", 30*time.Second, w.ra, w.rc) {
		t.Fatal("after unlinking in his own guild, Bob's message did not reach Alice and Carol")
	}
	if !delivered(w.alice, ch, "alice to owner", 30*time.Second, w.rb) {
		t.Fatal("after unlinking in his own guild, Alice's message did not reach Bob")
	}
}

// The same in a 1:1 DM Carol started: Bob's devices are members, Carol is
// its owner. Bob's unlink must not cut his desktop out of it.
func TestUnlinkInADMCarolStartedKeepsBobInIt(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	// Carol must be able to reach Bob's account: share a guild first.
	w.guildOf(t, w.alice, w.bob, w.phone, w.carol)
	dm, err := w.carol.StartDM(w.bob.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	ch := dm.Channels[0].ID
	ok := false
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && !ok {
		ok = memberCount(w.carol, dm.ID) == 3 && memberCount(w.bob, dm.ID) == 3
		time.Sleep(time.Second)
	}
	if !ok {
		t.Fatalf("setup: the DM never held Carol and both of Bob's devices (carol=%d bob=%d)", memberCount(w.carol, dm.ID), memberCount(w.bob, dm.ID))
	}
	if !delivered(w.bob, ch, "dm before", 30*time.Second, w.rc) {
		t.Fatal("setup: Bob's desktop could not reach the DM before the unlink")
	}

	waitUntil(t, 60*time.Second, func() bool { return w.bob.ProfileOf(w.carol.Fingerprint()).Name == "Carol" },
		"setup: Bob's desktop never learned Carol's name")
	_ = w.phone.Close()
	err = w.bob.UnlinkDevice(w.phoneKey)
	t.Logf("UnlinkDevice: %v", err)
	t.Logf("DM leaves after unlink: carol=%d bob=%d", memberCount(w.carol, dm.ID), memberCount(w.bob, dm.ID))
	bobToCarol := delivered(w.bob, ch, "dm bob after unlink", 30*time.Second, w.rc)
	carolToBob := delivered(w.carol, ch, "dm carol after unlink", 30*time.Second, w.rb)
	t.Logf("DM after unlink: bob→carol=%v carol→bob=%v", bobToCarol, carolToBob)
	if !bobToCarol || !carolToBob {
		t.Errorf("Bob's desktop was cut out of Carol's DM by unlinking his phone (bob→carol %v, carol→bob %v)", bobToCarol, carolToBob)
	}
	var inc *UnlinkIncompleteError
	// The shared guild is Alice's, so it is pending too, and listed first.
	if !errors.As(err, &inc) || len(inc.Pending) != 2 || inc.Pending[0].Kind != "guild" ||
		inc.Pending[1] != (UnlinkSpot{Kind: "dm", ID: dm.ID, Name: "Carol", RemovableBy: "Carol"}) {
		t.Errorf("UnlinkDevice did not report the guild and then Carol's DM as waiting for her to remove the phone: %v", err)
	}
	if n := memberCount(w.carol, dm.ID); n != 3 {
		t.Errorf("Carol sees %d leaves in the DM, want 3 (the phone's leaf stays until she removes it)", n)
	}
}

func TestUnlinkIncompleteErrorNamesADirectMessageAsOne(t *testing.T) {
	err := &UnlinkIncompleteError{Pending: []UnlinkSpot{
		{Kind: "guild", ID: "g1", Name: "Book club"},
		{Kind: "dm", ID: "g2", Name: "Carol", RemovableBy: "Carol"},
	}}
	const want = "app: unlinked, but that device stays listed in 2 conversations until someone there who can remove members does so (Book club, direct message with Carol)"
	if got := err.Error(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestUnlinkSpotsComeInAFixedOrder(t *testing.T) {
	want := []UnlinkSpot{
		{Kind: "guild", ID: "g2", Name: "Book club"},
		{Kind: "guild", ID: "g1", Name: "Games"},
		{Kind: "guild", ID: "g3", Name: "Games"},
		{Kind: "dm", ID: "d2", Name: "Alice"},
		{Kind: "dm", ID: "d1", Name: "Carol"},
	}
	got := []UnlinkSpot{want[4], want[2], want[0], want[3], want[1]}
	sortUnlinkSpots(got)
	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}
