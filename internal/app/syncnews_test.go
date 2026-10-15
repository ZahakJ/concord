package app

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// TestSyncDoesNotSilenceALiveMessage forces the interleaving behind a rare
// silent message: a catch-up already in flight stores a message somebody wrote
// a moment ago, and its live delivery, arriving second, is a duplicate the
// live path drops without a word. Labelled by the lane that won the race, a
// message written seconds ago went out as history, and every notifier stayed
// quiet. The label is now a property of the message — its age, and whether it
// was written after this device had the guild — so the race cannot flip it,
// while a row from before either still arrives as history.
func TestSyncDoesNotSilenceALiveMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, channel := guildOf(t, ctx, 2)
	a, b := svcs[0], svcs[1]
	mustSync(t, b, guildID, a)
	// B has had the guild for hours, so only age separates the two rows below.
	_ = b.store.SetSetting(guildArrivedPrefix+guildID,
		strconv.FormatInt(time.Now().Add(-3*time.Hour).UnixNano(), 10))

	var mu sync.Mutex
	backfill := map[string]bool{}
	b.OnMessage(func(m domain.Message) {
		mu.Lock()
		backfill[m.ID] = m.Backfill
		mu.Unlock()
	})

	fresh := plantRow(t, a, channel, "", "written a moment ago", time.Now())
	old := plantRow(t, a, channel, "", "written while B slept", time.Now().Add(-2*time.Hour))
	// The catch-up wins the race...
	mustSync(t, b, guildID, a)
	// ...and the live copy, arriving second, is a duplicate.
	if inserted, err := b.store.SaveMessage(fresh); err != nil || inserted {
		t.Fatalf("the live copy was not a duplicate (inserted=%v, %v)", inserted, err)
	}

	mu.Lock()
	defer mu.Unlock()
	bf, ok := backfill[fresh.ID]
	if !ok {
		t.Fatal("the fresh message was never emitted")
	}
	if bf {
		t.Fatal("a message written a moment ago reached the UI as history, because sync stored it first")
	}
	if bf, ok := backfill[old.ID]; !ok || !bf {
		t.Fatalf("a two-hour-old row arrived as news (emitted=%v backfill=%v)", ok, bf)
	}
	delete(backfill, fresh.ID)
	mu.Unlock()

	// A reaction on the fresh message, reaching B by sync, re-emits a row B
	// already announced: that is not news a second time.
	if _, err := a.store.ToggleReaction(fresh.ID, a.Fingerprint(), "👍"); err != nil {
		t.Fatalf("ToggleReaction: %v", err)
	}
	mustSync(t, b, guildID, a)
	mu.Lock()
	if bf, ok := backfill[fresh.ID]; !ok || !bf {
		t.Fatalf("a synced reaction re-announced a fresh message as news (emitted=%v backfill=%v)", ok, bf)
	}
}
