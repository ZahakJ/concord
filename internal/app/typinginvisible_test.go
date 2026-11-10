package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

// An explicit stop ends a typing line at once, and an invisible user sends no
// typing at all — "is typing…" beside a grey dot says they are not offline.
func TestTypingStopArrivesAndInvisibleSendsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("typing")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("join: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)
	channel := g.Channels[0].ID

	var mu sync.Mutex
	starts, stops := 0, 0
	b.OnTyping(func(_, _ string, stop bool) {
		mu.Lock()
		if stop {
			stops++
		} else {
			starts++
		}
		mu.Unlock()
	})
	counts := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return starts, stops
	}

	deadline := time.Now().Add(15 * time.Second)
	for s, _ := counts(); s == 0 && time.Now().Before(deadline); s, _ = counts() {
		_ = a.SendTyping(channel)
		time.Sleep(300 * time.Millisecond)
	}
	if s, _ := counts(); s == 0 {
		t.Fatal("typing never reached the peer")
	}
	if err := a.StopTyping(channel); err != nil {
		t.Fatalf("StopTyping: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for _, st := counts(); st == 0 && time.Now().Before(deadline); _, st = counts() {
		time.Sleep(100 * time.Millisecond)
	}
	if _, st := counts(); st == 0 {
		t.Fatal("the stop signal never arrived as a stop")
	}

	p := a.SelfProfile()
	p.Presence = "invisible"
	if err := a.SetProfile(p); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	before, _ := counts()
	for i := 0; i < 6; i++ {
		_ = a.SendTyping(channel)
		time.Sleep(250 * time.Millisecond)
	}
	time.Sleep(time.Second)
	if got, _ := counts(); got != before {
		t.Fatalf("an invisible user's typing reached the peer (%d new signals)", got-before)
	}
}
