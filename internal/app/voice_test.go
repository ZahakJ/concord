package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestVoiceSignalingBackbone verifies the two Go responsibilities for voice:
// presence discovery (peers learn who is in a voice room) and signaling relay
// (opaque WebRTC blobs reach a specific peer). The media path itself is
// browser-side and out of scope here.
func TestVoiceSignalingBackbone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked voice test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)

	// Form a shared guild so both share the channel/group used for the voice room.
	g, err := a.CreateGuild("voice-guild")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B join: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	// Collect voice presence + signals observed by each side.
	var mu sync.Mutex
	aSawB := false
	var bGotSignal []byte

	bPeerID := b.PeerID()
	a.OnVoicePresence(func(from, fingerprint, ch, action, target string) {
		mu.Lock()
		if from == bPeerID && ch == channel && action == "join" {
			aSawB = true
		}
		mu.Unlock()
	})
	b.OnVoiceSignal(func(from string, data []byte) {
		mu.Lock()
		if from == a.PeerID() {
			bGotSignal = append([]byte{}, data...)
		}
		mu.Unlock()
	})

	// Both join the voice room; A should discover B's presence announcement.
	if err := a.JoinVoice(channel); err != nil {
		t.Fatalf("A JoinVoice: %v", err)
	}
	if err := b.JoinVoice(channel); err != nil {
		t.Fatalf("B JoinVoice: %v", err)
	}
	waitUntil(t, 15*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return aSawB
	}, "A did not discover B in the voice room")

	// A relays a signaling blob to B; B must receive exactly those bytes.
	want := []byte(`{"type":"offer","sdp":"v=0..."}`)
	if err := a.RelaySignal(bPeerID, want); err != nil {
		t.Fatalf("RelaySignal: %v", err)
	}
	waitUntil(t, 10*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return string(bGotSignal) == string(want)
	}, "B did not receive A's relayed signal")

	// Leaving is clean (no panic, idempotent).
	if err := a.LeaveVoice(channel); err != nil {
		t.Fatalf("LeaveVoice: %v", err)
	}
	if err := a.LeaveVoice(channel); err != nil {
		t.Fatalf("LeaveVoice (repeat): %v", err)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal(msg)
}
