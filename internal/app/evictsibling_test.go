package app

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
)

// A commit that removes ANOTHER device of this account is not this device
// being removed. Bob owns a guild and unlinks his phone, which commits the
// removal of the phone's leaf; his tablet must apply that commit and stay a
// member. It used to read the commit as its own removal, refuse it, and tell
// Bob he was no longer in his own guild.
//
// And the other direction still holds: when the account is kicked, each of
// its devices learns it is out.
func TestRemovingOneDeviceLeafDoesNotEvictItsSiblings(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	code, err := w.bob.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	tabletDir := t.TempDir()
	if _, err := RedeemLink(ctx, tabletDir, code, "test-pass", ""); err != nil {
		t.Fatalf("link Bob's tablet: %v", err)
	}
	tablet := startServiceOn(t, ctx, tabletDir, w.boot)
	rt := &recorder{}
	tablet.OnMessage(rt.add)

	own := guildWith(t, w.bob, w.phone, tablet, w.alice)
	if !delivered(w.alice, own.Channels[0].ID, "before", 60*time.Second, rt, w.rb) {
		t.Fatal("setup: Alice's message did not reach Bob's devices")
	}

	_ = w.phone.Close()
	if err := w.bob.UnlinkDevice(w.phoneKey); err != nil {
		t.Fatalf("UnlinkDevice in a guild Bob owns: %v", err)
	}
	waitMemberCount(t, 60*time.Second, own.ID, 3, w.bob, w.alice)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && memberCount(tablet, own.ID) != 3 && tablet.EvictedFrom(own.ID) == "" {
		time.Sleep(200 * time.Millisecond)
	}
	if why, n := tablet.EvictedFrom(own.ID), memberCount(tablet, own.ID); why != "" || n != 3 {
		t.Fatalf("Bob's tablet after his phone's leaf was removed: evicted=%q leaves=%d; want still a member with 3", why, n)
	}
	if !delivered(w.alice, own.Channels[0].ID, "after", 60*time.Second, rt, w.rb) {
		t.Error("after the phone's leaf was removed, Alice's message did not reach Bob's tablet and desktop")
	}

	// Alice kicks Bob from her guild: both of his devices are out, and know.
	hers := guildWith(t, w.alice, w.bob, tablet)
	if err := w.alice.KickMember(hers.ID, w.bob.Fingerprint(), ""); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return w.bob.EvictedFrom(hers.ID) == evictedKicked && tablet.EvictedFrom(hers.ID) == evictedKicked
	}, "a kicked account's devices did not both learn they were removed")
}

// linkTablet links a second device into Bob's account and starts it.
func linkTablet(t *testing.T, ctx context.Context, bob *Service, boot string) (*Service, string) {
	t.Helper()
	code, err := bob.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := RedeemLink(ctx, dir, code, "test-pass", ""); err != nil {
		t.Fatalf("link Bob's tablet: %v", err)
	}
	return startServiceOn(t, ctx, dir, boot), dir
}

// guildWith has owner create a guild and the others join it, and waits for
// the whole roster. guildOf waits for each joiner in turn, which a linked
// device breaks: it is offered its account's guilds and may be seated before
// its own turn comes, so a count taken in between is one too high.
func guildWith(t *testing.T, owner *Service, joiners ...*Service) domain.Guild {
	t.Helper()
	g, err := owner.CreateGuild("shared")
	if err != nil {
		t.Fatal(err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range joiners {
		if _, err := j.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitMemberCount(t, 60*time.Second, g.ID, 1+len(joiners), append([]*Service{owner}, joiners...)...)
	return g
}

// A device that an earlier build left holding a wrong "removed" verdict heals
// after upgrading. The earlier build refused the commit that removed a sibling
// device and recorded itself as removed, so its group stayed one epoch behind
// with the sibling's leaf still in it. Bob's tablet is put in exactly that
// state: the verdict on disk and the removal commit never applied. When it
// comes back it must fetch the missed commit, apply it, end with the same
// roster as everyone else, and drop the verdict.
func TestAVerdictLeftByTheOldSiblingCheckClearsOnCatchUp(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	tablet, tabletDir := linkTablet(t, ctx, w.bob, w.boot)
	own := guildWith(t, w.bob, w.phone, tablet, w.alice)

	// What the earlier build left on the tablet: the verdict, and a group that
	// still holds the phone's leaf because the removal was never applied.
	tablet.noteEvicted(own.ID, evictedKicked)
	if why, n := tablet.EvictedFrom(own.ID), memberCount(tablet, own.ID); why != evictedKicked || n != 4 {
		t.Fatalf("setup: tablet reads evicted=%q leaves=%d, want %q with 4", why, n, evictedKicked)
	}
	_ = tablet.Close()
	_ = w.phone.Close()
	if err := w.bob.UnlinkDevice(w.phoneKey); err != nil {
		t.Fatalf("UnlinkDevice: %v", err)
	}
	waitMemberCount(t, 60*time.Second, own.ID, 3, w.bob, w.alice)

	tablet = startServiceOn(t, ctx, tabletDir, w.boot)
	healed := func() bool {
		return tablet.EvictedFrom(own.ID) == "" && memberCount(tablet, own.ID) == 3 &&
			epochOf(t, tablet, own.ID) == epochOf(t, w.bob, own.ID)
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && !healed() {
		time.Sleep(200 * time.Millisecond)
	}
	if !healed() {
		t.Fatalf("tablet with the stale verdict after catching up: evicted=%q leaves=%d epoch=%d (Bob's desktop at %d); want a member with 3 leaves at the same epoch",
			tablet.EvictedFrom(own.ID), memberCount(tablet, own.ID), epochOf(t, tablet, own.ID), epochOf(t, w.bob, own.ID))
	}
}

// The same stale verdict on a device that is online when the next membership
// commit arrives: applying it on the live path proves membership too.
func TestAWrongRemovedVerdictClearsWhenALiveCommitApplies(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	tablet, _ := linkTablet(t, ctx, w.bob, w.boot)
	own := guildWith(t, w.bob, w.phone, tablet, w.alice)

	tablet.noteEvicted(own.ID, evictedKicked)
	_ = w.phone.Close()
	if err := w.bob.UnlinkDevice(w.phoneKey); err != nil {
		t.Fatalf("UnlinkDevice: %v", err)
	}
	waitMemberCount(t, 60*time.Second, own.ID, 3, w.bob, w.alice)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && (memberCount(tablet, own.ID) != 3 || tablet.EvictedFrom(own.ID) != "") {
		time.Sleep(200 * time.Millisecond)
	}
	if why, n := tablet.EvictedFrom(own.ID), memberCount(tablet, own.ID); why != "" || n != 3 {
		t.Fatalf("tablet with a wrong verdict after applying the next commit: evicted=%q leaves=%d; want a member with 3", why, n)
	}
}

// failSecondRemoval makes the second removal of one account's leaves fail, the
// way a kick breaks part-way when the engine refuses one commit.
type failSecondRemoval struct {
	mls.Engine
	target atomic.Value // account fingerprint, string
	seen   atomic.Int32
}

func (f *failSecondRemoval) Remove(ctx context.Context, gid mls.GroupID, cred []byte) ([]byte, error) {
	if target, _ := f.target.Load().(string); target != "" && accountFingerprintOf(cred) == target {
		if f.seen.Add(1) == 2 {
			return nil, errors.New("injected: removal refused")
		}
	}
	return f.Engine.Remove(ctx, gid, cred)
}

// A kick that removes only some of an account's devices still puts the whole
// account out. Alice kicks Bob, who is on a desktop and a tablet; the removal
// of his second leaf fails. Alice is told the kick did not finish, and both of
// Bob's devices know they are out: one because its leaf went, the other
// because the guild's governance says the account was removed. The device
// whose leaf is still there keeps applying commits, and that must not lift a
// verdict governance stands behind.
func TestAKickThatFailsPartWayStillPutsEveryDeviceOut(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	fail := &failSecondRemoval{}
	alice, err := Start(ctx, Config{
		DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true,
		BootstrapPeers: []string{boot},
		wrapMLS:        func(e mls.Engine) mls.Engine { fail.Engine = e; return fail },
	})
	if err != nil {
		t.Fatalf("Start Alice: %v", err)
	}
	t.Cleanup(func() { _ = alice.Close() })
	bob := startServiceOn(t, ctx, t.TempDir(), boot)
	tablet, _ := linkTablet(t, ctx, bob, boot)

	hers := guildWith(t, alice, bob, tablet)

	fail.target.Store(bob.Fingerprint())
	if err := alice.KickMember(hers.ID, bob.Fingerprint(), ""); err == nil {
		t.Error("KickMember reported success although one of Bob's devices was not removed")
	}
	if n := fail.seen.Load(); n != 2 {
		t.Fatalf("setup: the kick tried %d removals of Bob's leaves, want 2", n)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return bob.EvictedFrom(hers.ID) == evictedKicked && tablet.EvictedFrom(hers.ID) == evictedKicked
	}, "after a kick that failed part-way, Bob's devices did not both learn they were removed")

	// The device whose leaf survived is the one still applying commits.
	creds, err := alice.GuildMembers(hers.ID)
	if err != nil {
		t.Fatal(err)
	}
	var survivor *Service
	for _, c := range creds {
		for _, d := range []*Service{bob, tablet} {
			if bytes.Equal(c, d.myCredential) {
				survivor = d
			}
		}
	}
	if survivor == nil {
		t.Fatal("setup: neither of Bob's leaves is left in Alice's guild")
	}
	carol := startServiceOn(t, ctx, t.TempDir(), boot)
	code, err := alice.InviteCode(hers.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := carol.JoinViaInvite(code); err != nil {
		t.Fatalf("Carol JoinViaInvite: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return epochOf(t, survivor, hers.ID) == epochOf(t, alice, hers.ID)
	}, "setup: the surviving device did not apply the commit that added Carol")
	if why := survivor.EvictedFrom(hers.ID); why != evictedKicked {
		t.Fatalf("after applying a commit, the kicked account's surviving device reads evicted=%q; want %q", why, evictedKicked)
	}
}

// A device that was offline while its sibling was unlinked meets the removal
// through catch-up rather than the live control topic, and must apply it and
// stay a member.
func TestADeviceOfflineForItsSiblingsRemovalCatchesUpAsAMember(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	tablet, tabletDir := linkTablet(t, ctx, w.bob, w.boot)
	own := guildWith(t, w.bob, w.phone, tablet, w.alice)

	_ = tablet.Close()
	_ = w.phone.Close()
	if err := w.bob.UnlinkDevice(w.phoneKey); err != nil {
		t.Fatalf("UnlinkDevice: %v", err)
	}
	waitMemberCount(t, 60*time.Second, own.ID, 3, w.bob, w.alice)

	tablet = startServiceOn(t, ctx, tabletDir, w.boot)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && memberCount(tablet, own.ID) != 3 && tablet.EvictedFrom(own.ID) == "" {
		time.Sleep(200 * time.Millisecond)
	}
	if why, n := tablet.EvictedFrom(own.ID), memberCount(tablet, own.ID); why != "" || n != 3 {
		t.Fatalf("tablet back online after its sibling was unlinked: evicted=%q leaves=%d; want a member with 3", why, n)
	}
}

// A device that was offline while its account was kicked learns it is out
// when it catches up.
func TestADeviceOfflineForItsAccountsKickLearnsItOnCatchUp(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	tablet, tabletDir := linkTablet(t, ctx, w.bob, w.boot)
	// The phone has no part here, and left running it would be offered
	// Alice's guild as one of its account's.
	_ = w.phone.Close()
	hers := guildWith(t, w.alice, w.bob, tablet)

	_ = tablet.Close()
	if err := w.alice.KickMember(hers.ID, w.bob.Fingerprint(), ""); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool { return w.bob.EvictedFrom(hers.ID) == evictedKicked },
		"Bob's desktop did not learn it was kicked")

	tablet = startServiceOn(t, ctx, tabletDir, w.boot)
	waitUntil(t, 90*time.Second, func() bool {
		_, _ = w.alice.SendMessage(hers.Channels[0].ID, "after the kick", "", "")
		time.Sleep(time.Second)
		return tablet.EvictedFrom(hers.ID) != ""
	}, "Bob's tablet, offline during the kick, never learned it was removed")
	if why := tablet.EvictedFrom(hers.ID); why != evictedKicked {
		t.Fatalf("Bob's tablet after catching up on the kick: evicted=%q, want %q", why, evictedKicked)
	}
}
