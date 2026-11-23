package app

import (
	"context"
	"testing"
	"time"
)

// A hello that fails is how a guild created on one device went missing on the
// other: the re-greet after CreateGuild was the only thing carrying the offer,
// and a failure was left for the next connection, which never came while the
// first one stayed up. The failed hello must be made again.
func TestAGuildReachesTheOtherDeviceWhenTheHelloCarryingItIsLost(t *testing.T) {
	if testing.Short() {
		t.Skip("two linked devices over a live rendezvous")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	desk.dropOwnHellos.Store(1)
	later, err := desk.CreateGuild("Lost Hello")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	waitUntil(t, 10*time.Second, func() bool { return desk.dropOwnHellos.Load() <= 0 },
		"the hello after CreateGuild was never made")

	// One retry is due 2 s after the loss; the connection stays up throughout,
	// so nothing else would carry the offer.
	waitUntil(t, 20*time.Second, func() bool {
		for _, g := range phone.Guilds() {
			if g.ID == later.ID {
				return true
			}
		}
		return false
	}, "a guild whose hello was lost never reached the other device")
}
