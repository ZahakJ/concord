package app

import (
	"context"
	"testing"
)

// TestBehindLatchStoreFollowsMemory crosses a late noteBehind with a
// clearBehind at the point where each has changed the latch in memory and not
// yet written it. Each used to write its own value, so the clear could write
// first and the note last: the device carried on cleared while the store kept
// the latch for its next start. Seen in "the owner's other device can repair"
// as "the persisted seen-behind latch survived the catch-up".
func TestBehindLatchStoreFollowsMemory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	const guildID = "latch-guild"

	noteChanged := make(chan struct{})
	clearDone := make(chan struct{})
	calls := 0
	testHookBeforeBehindPersist = func() {
		calls++
		if calls == 1 { // the note: hold its write until the clear is through
			close(noteChanged)
			<-clearDone
		}
	}
	defer func() { testHookBeforeBehindPersist = nil }()

	noted := make(chan struct{})
	go func() {
		s.noteBehind(guildID, 3)
		close(noted)
	}()
	<-noteChanged
	s.clearBehind(guildID)
	close(clearDone)
	<-noted

	if s.behindLatched(guildID) {
		t.Fatal("the latch is set in memory after the clear")
	}
	if v, _ := s.store.GetSetting(behindSeenPrefix + guildID); v != "" {
		t.Fatalf("memory says cleared, the store says latched at %q", v)
	}
}
