package app

// Adversarial probes of the epoch-drift fix (commit c090e47), written to break
// it rather than to confirm it.
//
//  1. The fix's own drift tests run on loopback, where every peer dials every
//     other directly. The user's real topology is two NAT'd devices plus a
//     friend, connected only through the rendezvous relay — the topology that
//     invalidated every previous round of "messages don't arrive" fixes.
//     TestEpochDriftDeliveryRelayOnly is the same lost-commit drill with all
//     three members behind simulated NATs.
//
//  2. handleInviteRequest now REFUSES to serve invites while its own guild is
//     flagged out of sync. Refusal is sound for one committer, but this
//     account has TWO authorized committers (desk and phone). If both mint a
//     membership commit at the same epoch concurrently — two stranded members
//     healing against different owner devices at once — the owner devices fork
//     EACH OTHER, both flag out of sync, and then both refuse to serve the
//     re-add that is the only cure for a fork. TestConcurrentHealsOnTwoOwnerDevices
//     constructs exactly that.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/multiformats/go-multiaddr"
)

// natTrio builds the user's actual topology: desk+phone (one account) plus a
// friend, all guild members, each behind its own simulated NAT with the relay
// circuit as the only route between any two of them.
func natTrio(t *testing.T, ctx context.Context) (desk, phone, friend *Service, guildID, textCh string) {
	t.Helper()
	boot := productionRendezvous(t, ctx)
	deskDir, phoneDir, friendDir := t.TempDir(), t.TempDir(), t.TempDir()
	desk, phone, textCh, _ = linkedPair(t, ctx, deskDir, phoneDir, boot)
	guildID = desk.Guilds()[0].ID

	friend = startServiceOn(t, ctx, friendDir, boot)
	if err := friend.SetDisplayName("Friend"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	code, err := desk.InviteCode(guildID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatalf("friend JoinViaInvite: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := desk.MemberCount(guildID)
		return n == 3
	}, "the friend never joined the guild")

	// Everyone goes home to a different network.
	_ = desk.Close()
	_ = phone.Close()
	_ = friend.Close()
	for _, d := range []string{deskDir, phoneDir, friendDir} {
		_ = os.Remove(filepath.Join(d, "peers.json"))
	}
	desk = startNATed(t, ctx, deskDir, boot, "127.0.0.2", "127.0.0.3", "127.0.0.4")
	phone = startNATed(t, ctx, phoneDir, boot, "127.0.0.3", "127.0.0.2", "127.0.0.4")
	friend = startNATed(t, ctx, friendDir, boot, "127.0.0.4", "127.0.0.2", "127.0.0.3")

	// All three pairs must reconnect — through the relay only.
	pairs := [][2]*Service{{desk, phone}, {desk, friend}, {phone, friend}}
	waitUntil(t, 120*time.Second, func() bool {
		for _, pr := range pairs {
			if len(pr[0].host.Libp2p().Network().ConnsToPeer(pr[1].host.PeerID())) == 0 {
				return false
			}
		}
		return true
	}, "the NAT'd trio never fully reconnected through the relay")
	for _, pr := range pairs {
		for _, c := range pr[0].host.Libp2p().Network().ConnsToPeer(pr[1].host.PeerID()) {
			if _, err := c.RemoteMultiaddr().ValueForProtocol(multiaddr.P_CIRCUIT); err != nil {
				t.Fatalf("DIRECT connection %s — the simulated NAT leaks, this test proves nothing", c.RemoteMultiaddr())
			}
		}
	}
	return desk, phone, friend, guildID, textCh
}

// TestEpochDriftDeliveryRelayOnly is TestEpochDriftDelivery on the topology
// the user actually has. A membership commit falls on the floor at the desk;
// the very next message must still reach the phone and the friend across
// relay circuits, via the same stash + commit-bridge recovery the fix claims —
// and the recovery's sync streams must survive being opened over LIMITED
// connections.
func TestEpochDriftDeliveryRelayOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	desk, phone, friend, guildID, textCh := natTrio(t, ctx)

	// Warm the gossip meshes over the relay, then measure the good case.
	deliverAll(t, desk, textCh, "relay warmup", 120*time.Second, phone, friend)
	good := deliverAll(t, desk, textCh, "relay good case", 60*time.Second, phone, friend)
	t.Logf("relay-only good case (same epoch): %v to the slowest of 2 receivers", good)

	loseOneCommit(t, ctx, desk, guildID)
	bad := deliverAll(t, desk, textCh, "relay send after the lost commit", 180*time.Second, phone, friend)
	t.Logf("relay-only bad case (receivers one epoch behind): %v to the slowest of 2 receivers", bad)

	// The connections that carried the recovery must still be relay circuits.
	dumpConns(t, "after recovery desk<->phone", desk, phone)
	dumpConns(t, "after recovery desk<->friend", desk, friend)

	waitUntil(t, 60*time.Second, func() bool {
		return !phone.OutOfSync(guildID) && !friend.OutOfSync(guildID)
	}, "a guild stayed flagged out of sync after relay-only recovery")
}

// TestForkedGuildConverges is TestConcurrentHealsOnTwoOwnerDevices with the
// race taken out. That test has to WIN a race to produce a fork — two owner
// devices minting from one base epoch in the same instant — which it manages
// about a quarter of the time, so a guild that never heals surfaces as a rare
// ninety-second timeout rather than as a failure anybody can read. Here the two
// branches are built deliberately: each owner device privately advances its own
// group state with commits the other never sees (the same trick loseOneCommit
// already uses), and one friend then catches up from each device, which puts it
// on that device's tree and gives both halves a member that answers them
// readably.
//
// That last part is the whole point. A fork with one member per branch heals
// even without the fix, because the far branch is the ONLY peer either side can
// ask. Give each branch a companion and the verdict never sticks: the companion
// is on our own tree, so its payload always decrypts, and the answer it gives
// wipes the verdict the far branch just set.
//
// The epochs are asymmetric on purpose — one commit for the desk, two for the
// phone — because the stale half is the half that has to move, and it is the
// only one that can prove which half it is in.
func TestForkedGuildConverges(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	guildID := desk.Guilds()[0].ID
	groupID := desk.Guilds()[0].GroupID

	var friends []*Service
	for i := 0; i < 2; i++ {
		f := startServiceOn(t, ctx, t.TempDir(), boot)
		if err := f.SetDisplayName("Friend"); err != nil {
			t.Fatalf("SetDisplayName: %v", err)
		}
		code, err := desk.InviteCode(guildID)
		if err != nil {
			t.Fatalf("InviteCode: %v", err)
		}
		if _, err := f.JoinViaInvite(code); err != nil {
			t.Fatalf("friend JoinViaInvite: %v", err)
		}
		friends = append(friends, f)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := desk.MemberCount(guildID)
		return n == 4
	}, "the friends never joined the guild")
	// Friends dial the desk that served their join; the phone has to be reachable
	// too or the branch it owns has no admissions route.
	if err := friends[1].host.Connect(ctx, phone.host.AddrInfo()); err != nil {
		t.Fatalf("friend2 connect to phone: %v", err)
	}
	deliverAll(t, desk, textCh, "warmup", 60*time.Second, phone, friends[0], friends[1])

	// Let the connect tails finish. Each new connection schedules a catch-up and,
	// if that one fails, another ten seconds later; a fork built underneath one
	// of those is bridged by a sync that was already in flight, which proves
	// nothing about what happens to a split that forms while everyone is settled.
	time.Sleep(12 * time.Second)

	base, err := desk.mls.Epoch(ctx, groupID)
	if err != nil {
		t.Fatalf("epoch: %v", err)
	}
	loseOneCommit(t, ctx, desk, guildID)
	loseOneCommit(t, ctx, phone, guildID)
	loseOneCommit(t, ctx, phone, guildID)
	// One friend onto each branch. Neither commit was ever published, so each
	// friend is still at the common epoch and a plain catch-up from one device
	// hands it that device's branch and nothing else — no new commits, nothing
	// on the control topic, no race to win.
	if err := friends[0].syncGuildFromPeer(guildID, desk.host.PeerID()); err != nil {
		t.Fatalf("friend1 catch-up from the desk: %v", err)
	}
	if err := friends[1].syncGuildFromPeer(guildID, phone.host.PeerID()); err != nil {
		t.Fatalf("friend2 catch-up from the phone: %v", err)
	}
	de, _ := desk.mls.Epoch(ctx, groupID)
	pe, _ := phone.mls.Epoch(ctx, groupID)
	f0e, _ := friends[0].mls.Epoch(ctx, groupID)
	f1e, _ := friends[1].mls.Epoch(ctx, groupID)
	if de != base+1 || f0e != base+1 || pe != base+2 || f1e != base+2 {
		t.Fatalf("epochs desk=%d friend1=%d phone=%d friend2=%d; want %d,%d and %d,%d — the branches were not built as intended",
			de, f0e, pe, f1e, base+1, base+1, base+2, base+2)
	}
	// …and they really are two trees, not just two epoch numbers.
	probe, err := desk.mls.Encrypt(ctx, groupID, []byte("fork probe"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := phone.mls.Decrypt(ctx, groupID, probe); err == nil {
		t.Fatal("the phone can still read the desk's ciphertext: this test is not testing a fork")
	}
	t.Logf("fork built: desk branch at epoch %d, phone branch at epoch %d", de, pe)

	// Nothing else is wrong. Everyone is online, everyone is a member, and every
	// message the desk sends from here is unreadable to half the guild.
	all := []*Service{phone, friends[0], friends[1]}
	sent := deliverAll(t, desk, textCh, "across the fork", 45*time.Second, all...)
	t.Logf("delivery across a forked guild: %v", sent)

	waitUntil(t, 45*time.Second, func() bool {
		return !desk.OutOfSync(guildID) && !phone.OutOfSync(guildID) &&
			!friends[0].OutOfSync(guildID) && !friends[1].OutOfSync(guildID)
	}, "somebody is still flying the catching-up banner after the fork healed")
	waitUntil(t, 45*time.Second, func() bool {
		want, err := desk.mls.Epoch(ctx, groupID)
		if err != nil {
			return false
		}
		for _, svc := range all {
			if e, err := svc.mls.Epoch(ctx, groupID); err != nil || e != want {
				return false
			}
		}
		return true
	}, "the two branches never converged on one epoch")
	de, _ = desk.mls.Epoch(ctx, groupID)
	t.Logf("converged at epoch %d (branches were %d and %d)", de, base+1, base+2)
}

// TestConcurrentHealsOnTwoOwnerDevices aims two simultaneous re-add heals at
// the two owner devices. Each device serves Remove+Add commits minted from the
// same base epoch, neither having seen the other's — a fork between the only
// two authorized committers. The fix's fork detector then flags BOTH owner
// devices out of sync, and its new refuse-while-stranded rule makes each
// refuse the other's heal: the only cure for a fork, administered by the only
// people allowed to administer it, is refused by both forever.
func TestConcurrentHealsOnTwoOwnerDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	guildID := desk.Guilds()[0].ID

	// Two friends, so each owner device has a distinct heal to serve.
	var friends []*Service
	for i := 0; i < 2; i++ {
		f := startServiceOn(t, ctx, t.TempDir(), boot)
		if err := f.SetDisplayName("Friend"); err != nil {
			t.Fatalf("SetDisplayName: %v", err)
		}
		code, err := desk.InviteCode(guildID)
		if err != nil {
			t.Fatalf("InviteCode: %v", err)
		}
		if _, err := f.JoinViaInvite(code); err != nil {
			t.Fatalf("friend JoinViaInvite: %v", err)
		}
		friends = append(friends, f)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := desk.MemberCount(guildID)
		return n == 4
	}, "the friends never joined the guild")
	deliverAll(t, desk, textCh, "warmup", 60*time.Second, phone, friends[0], friends[1])

	// Make sure the second heal can actually reach the phone: in this harness
	// friends connect to the desk (who served their join) but not necessarily
	// to the phone yet, and a heal against an unreachable committer fails at
	// the transport and proves nothing about forking.
	if err := friends[1].host.Connect(ctx, phone.host.AddrInfo()); err != nil {
		t.Fatalf("friend2 connect to phone: %v", err)
	}

	groupID := desk.Guilds()[0].GroupID
	base, err := desk.mls.Epoch(ctx, groupID)
	if err != nil {
		t.Fatalf("epoch: %v", err)
	}
	t.Logf("base epoch before concurrent heals: %d", base)

	// Two members decide to heal at the same moment and pick different owner
	// devices (authorizedCommittersOnline orders peers from a map walk, so in
	// the wild the pick is effectively random). healViaCommitter is exactly
	// what healOutOfSync runs per candidate. Loop until both owner devices
	// actually mint in the same round — that is the moment the fork can form.
	for round := 1; round <= 10; round++ {
		var ok1, ok2 bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); ok1 = friends[0].healViaCommitter(guildID, desk.host.PeerID()) }()
		go func() { defer wg.Done(); ok2 = friends[1].healViaCommitter(guildID, phone.host.PeerID()) }()
		wg.Wait()
		de, _ := desk.mls.Epoch(ctx, groupID)
		pe, _ := phone.mls.Epoch(ctx, groupID)
		t.Logf("round %d: desk-heal=%v phone-heal=%v epochs desk=%d phone=%d outOfSync desk=%v phone=%v",
			round, ok1, ok2, de, pe, desk.OutOfSync(guildID), phone.OutOfSync(guildID))
		if ok1 && ok2 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Let gossip and any recovery run, then demand the guild still works: a
	// message from the desk reaches everyone, and nobody is left permanently
	// wearing the banner.
	all := []*Service{phone, friends[0], friends[1]}
	sent := deliverAll(t, desk, textCh, "after concurrent heals", 90*time.Second, all...)
	t.Logf("delivery after concurrent owner-device heals: %v", sent)

	waitUntil(t, 90*time.Second, func() bool {
		return !desk.OutOfSync(guildID) && !phone.OutOfSync(guildID) &&
			!friends[0].OutOfSync(guildID) && !friends[1].OutOfSync(guildID)
	}, "someone is permanently stranded after two owner devices committed concurrently")

	de, _ := desk.mls.Epoch(ctx, groupID)
	pe, _ := phone.mls.Epoch(ctx, groupID)
	t.Logf("final epochs: desk=%d phone=%d", de, pe)
}
