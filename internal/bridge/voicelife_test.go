package bridge

import (
	"testing"
	"time"
)

// shortGrace runs a test against a grace measured in milliseconds. The real one
// is ten seconds, and three of those per test is a minute of nothing.
func shortGrace(t *testing.T, d time.Duration) {
	t.Helper()
	prev := voiceOwnerGrace
	voiceOwnerGrace = d
	t.Cleanup(func() { voiceOwnerGrace = prev })
}

func owns(b *Bridge, channelID string) bool {
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	_, ok := v.rooms[channelID]
	return ok
}

// The bug this exists to close: a browser tab closed mid-call left the node
// announcing "I am in this call" every three seconds for the life of the
// process, so everyone else held a media connection to a client that was not
// there. The stream that ended is the proof, and after a grace the call ends
// with it.
func TestAClosedClientEndsTheCallItStarted(t *testing.T) {
	shortGrace(t, 20*time.Millisecond)
	b := &Bridge{}
	b.noteVoiceOwner("study-hall", "tab-1")
	if !owns(b, "study-hall") {
		t.Fatal("the call was not recorded against the client that asked for it")
	}
	b.DropClient("tab-1")
	if !owns(b, "study-hall") {
		t.Fatal("the call ended the instant the stream dropped, with no grace at all")
	}
	time.Sleep(120 * time.Millisecond)
	if owns(b, "study-hall") {
		t.Error("the tab has been gone for six graces and the node still holds its call")
	}
}

// An EventSource redials on its own, and a laptop that sleeps for a moment
// looks exactly like a closed tab for a second or two. Neither is a hang-up.
func TestAReconnectingStreamKeepsItsCall(t *testing.T) {
	shortGrace(t, 80*time.Millisecond)
	b := &Bridge{}
	b.noteVoiceOwner("study-hall", "tab-1")
	b.DropClient("tab-1")
	time.Sleep(20 * time.Millisecond)
	b.AttachClient("tab-1") // the stream came back inside the grace
	time.Sleep(200 * time.Millisecond)
	if !owns(b, "study-hall") {
		t.Error("a stream that redialed inside the grace was hung up on anyway")
	}
}

// Leaving properly must take the ownership record with it, or a later drop of
// that same client id would try to leave a room nobody is in.
func TestLeavingForgetsTheOwner(t *testing.T) {
	b := &Bridge{}
	b.noteVoiceOwner("study-hall", "tab-1")
	b.forgetVoiceOwner("study-hall")
	if owns(b, "study-hall") {
		t.Error("a room left cleanly is still recorded as held by its client")
	}
}

// Every caller that worked before this existed must keep working. A shell that
// calls JoinVoice through the Go API names no client, owns nothing, and is
// never hung up on — which is what keeps the Android core alive across an
// Activity the OS destroyed while the call's foreground service runs on.
func TestAnAnonymousCallerIsNeverHungUpOn(t *testing.T) {
	shortGrace(t, 20*time.Millisecond)
	b := &Bridge{}
	b.noteVoiceOwner("study-hall", "")
	b.DropClient("")
	b.DropClient("tab-1")
	time.Sleep(120 * time.Millisecond)
	if owns(b, "study-hall") {
		t.Error("an unowned room was recorded as owned")
	}
}

// A second call from the same client while a grace is armed means that client
// is demonstrably alive; the pending hang-up is stale and must be called off.
func TestANewCallDisarmsAPendingHangUp(t *testing.T) {
	shortGrace(t, 80*time.Millisecond)
	b := &Bridge{}
	b.noteVoiceOwner("study-hall", "tab-1")
	b.DropClient("tab-1")
	time.Sleep(20 * time.Millisecond)
	b.noteVoiceOwner("back-room", "tab-1")
	time.Sleep(200 * time.Millisecond)
	if !owns(b, "study-hall") || !owns(b, "back-room") {
		t.Error("a client that just made a call was hung up on by a stale timer")
	}
}
