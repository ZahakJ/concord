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
