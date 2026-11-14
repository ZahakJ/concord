package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
)

// TestConcurrentAdoptionsKeepTheNewest is the race that left a device with a
// newer stamp over older content: two copies adopted at once (an edit's push
// crossing a reconnect's greeting), the check passed for both, and the older
// one's fields landed last.
func TestConcurrentAdoptionsKeepTheNewest(t *testing.T) {
	alice := recordService(t, mustID(t))
	for i := 0; i < 200; i++ {
		older := recordProfile(int64(1000 + 2*i))
		older.Games, older.GamesSet, older.Bio = nil, true, "older"
		newer := recordProfile(int64(1001 + 2*i))
		newer.Games, newer.GamesSet, newer.Bio = []Game{{Name: "Outer Wilds"}}, true, "newer"
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); alice.AdoptLinkedProfile(newer) }()
		go func() { defer wg.Done(); alice.AdoptLinkedProfile(older) }()
		wg.Wait()
		got := alice.SelfProfile()
		if got.UpdatedAt != newer.UpdatedAt || got.Bio != "newer" || !hasGame(got.Games, "Outer Wilds") {
			t.Fatalf("round %d: stamp %d bio %q games %v — the older copy landed over the newer one", i, got.UpdatedAt, got.Bio, got.Games)
		}
	}
}

// TestProfileEditReachesOwnDeviceWithoutGossipOrFirstHello: the phone hears
// nothing on any guild topic (as when the mesh is not formed yet), and the
// first push to it is lost. Two edits in a row, the second too big for a hello,
// must both arrive within seconds and without a reconnect.
func TestProfileEditReachesOwnDeviceWithoutGossipOrFirstHello(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	waitDevice(t, desk, phone.PeerID())

	// Nothing on gossip: the phone leaves every guild meta topic it is on.
	for _, g := range phone.Guilds() {
		phone.ps.Unsubscribe(domain.GuildMetaTopicID(g.GroupID))
	}
	// And the first profile push of each edit is lost on the way.
	var dropped atomic.Int32
	var lastDrop atomic.Int64
	desk.testSayProfile = func(ctx context.Context, p peer.ID, req []byte) ([]byte, error) {
		at := desk.profileStamp()
		if lastDrop.Load() != at {
			lastDrop.Store(at)
			dropped.Add(1)
			return nil, errors.New("test: push lost")
		}
		return desk.host.SayHello(ctx, p, req)
	}

	first := sentinelProfile("first edit")
	start := time.Now()
	if err := desk.SetProfile(first); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 15*time.Second, func() bool { return profileMatches(phone.SelfProfile(), first) },
		"the first edit never reached the phone")
	t.Logf("first edit on the phone after %v", time.Since(start).Round(100*time.Millisecond))

	second := pictureProfile("second edit, with pictures")
	start = time.Now()
	if err := desk.SetProfile(second); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 15*time.Second, func() bool { return profileMatches(phone.SelfProfile(), second) },
		"the second (large) edit never reached the phone on the same connection")
	t.Logf("second edit on the phone after %v", time.Since(start).Round(100*time.Millisecond))
	if dropped.Load() == 0 {
		t.Fatal("no push was dropped; the test did not exercise the loss")
	}
}
