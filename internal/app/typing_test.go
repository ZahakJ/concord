package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestTypingIndicatorsReciprocal covers both halves of the switch on one pair:
// with it off nothing is published (their screen stays quiet) and nothing is
// surfaced (ours does too). Reciprocity is the setting's whole claim, so the
// test asserts both directions rather than just the send side.
func TestTypingIndicatorsReciprocal(t *testing.T) {
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
	seenByB := 0
	b.OnTyping(func(_, _ string) {
		mu.Lock()
		seenByB++
		mu.Unlock()
	})
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return seenByB
	}

	// Baseline: on by default, so a hint gets through.
	if !a.TypingEnabled() {
		t.Fatal("typing indicators are off by default")
	}
	deadline := time.Now().Add(15 * time.Second)
	for count() == 0 && time.Now().Before(deadline) {
		if err := a.SendTyping(channel); err != nil {
			t.Fatalf("SendTyping: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if count() == 0 {
		t.Fatal("typing hint never reached the peer with the setting on")
	}

	// Send side: A switches off, and nothing more arrives at B.
	if err := a.SetTypingEnabled(false); err != nil {
		t.Fatalf("SetTypingEnabled: %v", err)
	}
	before := count()
	for i := 0; i < 5; i++ {
		if err := a.SendTyping(channel); err != nil {
			t.Fatalf("SendTyping while off: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(time.Second)
	if got := count(); got != before {
		t.Fatalf("B saw %d typing hints after A switched off, want %d", got, before)
	}

	// Receive side: B switches off too and stops being shown A's hints even
	// though A is back to broadcasting them.
	if err := a.SetTypingEnabled(true); err != nil {
		t.Fatalf("SetTypingEnabled(true): %v", err)
	}
	if err := b.SetTypingEnabled(false); err != nil {
		t.Fatalf("B SetTypingEnabled(false): %v", err)
	}
	before = count()
	for i := 0; i < 5; i++ {
		if err := a.SendTyping(channel); err != nil {
			t.Fatalf("SendTyping: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(time.Second)
	if got := count(); got != before {
		t.Fatalf("B was shown %d typing hints with its own switch off, want %d", got, before)
	}
}

// TestTypingPrefPersists: the switch is account state, not a UI toggle that
// forgets itself on restart.
func TestTypingPrefPersists(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	if !s.TypingEnabled() {
		t.Fatal("default should be on")
	}
	if err := s.SetTypingEnabled(false); err != nil {
		t.Fatalf("SetTypingEnabled: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again := startServiceInDir(t, ctx, dir)
	if again.TypingEnabled() {
		t.Fatal("typing indicators came back on after a restart")
	}
}
