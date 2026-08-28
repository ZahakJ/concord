package bridge

import (
	"sync"
	"time"
)

// How long a call outlives the UI that started it.
//
// A dropped /events stream is not proof of anything on its own: an EventSource
// redials by itself, and a laptop that sleeps for a moment or a Wi-Fi handover
// both look exactly like a closed tab for a second or two. The grace is what
// separates those from the real thing. Ten seconds is three heartbeats — long
// enough that a reconnect lands inside it, short enough that nobody sits
// talking to a tile whose owner left.
// A var, not a const, only so the tests can run in milliseconds instead of
// waiting out three heartbeats each.
var voiceOwnerGrace = 10 * time.Second

// Why any of this exists.
//
// JoinVoice starts a goroutine that announces "I am in this call" on a gossip
// topic every three seconds, and the only thing that ever stopped it was
// LeaveVoice. So a browser tab closed mid-call left the node announcing a
// participant who no longer existed, for the life of the process: the survivors
// held a media connection to nobody, the roster never emptied, and the ghost
// was still on the stage minutes later. The tab's own goodbye is worth sending
// (lib/api.js does), but it is the one signal that cannot be relied on — it
// never fires when a process is killed, when a laptop lid closes on a dying
// battery, or when the browser is force-quit.
//
// The node already knows something better. Each attached UI names itself in the
// /events query string, and that stream's lifetime IS that UI's lifetime — it
// is exactly the fact the background-pacing vote is built on (visibility.go).
// So a call gets an owner: the client that asked for it. When that client's
// stream ends and does not come back inside the grace, the call it started ends
// with it.
//
// Deliberately keyed on a client that NAMED itself. A caller with no client id
// — the gomobile shell calling JoinVoice through the Go API, a test — owns
// nothing and is never hung up on, which keeps every path that worked before
// working exactly as it did.

type voiceOwners struct {
	mu sync.Mutex
	// channelID -> the set of client ids that asked for this room's call.
	//
	// A set, not a single owner: two windows of the same account on one node
	// are one participant in the room, and the node must stay in it until the
	// LAST of them has gone. With a single owner the second window to join
	// replaced the first, and closing it hung up on a window that was still
	// open and still showing itself in the call — the outcome noteVoiceOwner's
	// own comment calls far worse than leaving a ghost.
	rooms map[string]map[string]bool
	// clientID -> the timer that will end its rooms, armed when its stream
	// dropped and disarmed if it comes back.
	pending map[string]*time.Timer
}

// noteVoiceOwner records which client asked for a call. An empty id clears any
// previous owner rather than leaving a stale one: the room is now held by
// somebody who cannot be tracked, and hanging up on a client that has gone
// while a different, live one is in the room would be far worse than leaving a
// ghost.
func (b *Bridge) noteVoiceOwner(channelID, clientID string) {
	if channelID == "" {
		return
	}
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	if clientID == "" || len(clientID) > maxClientIDLen {
		delete(v.rooms, channelID)
		return
	}
	if v.rooms[channelID] == nil {
		v.rooms[channelID] = map[string]bool{}
	}
	v.rooms[channelID][clientID] = true
	// This client is demonstrably alive — it just made a call. Anything armed
	// against it is stale.
	if t, ok := v.pending[clientID]; ok {
		t.Stop()
		delete(v.pending, clientID)
	}
}

// forgetVoiceOwner drops the ownership record for a room we have left.
func (b *Bridge) forgetVoiceOwner(channelID string) {
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.rooms, channelID)
}

// voiceClientGone arms the grace timer for a client whose /events stream ended.
func (b *Bridge) voiceClientGone(clientID string) {
	if clientID == "" {
		return
	}
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.ownsAnyLocked(clientID) {
		return // not in a call: nothing to wind down
	}
	if t, ok := v.pending[clientID]; ok {
		t.Stop()
	}
	v.pending[clientID] = time.AfterFunc(voiceOwnerGrace, func() {
		b.endVoiceFor(clientID)
	})
}

// voiceClientBack disarms the grace timer: the stream came back.
func (b *Bridge) voiceClientBack(clientID string) {
	if clientID == "" {
		return
	}
	v := b.voice()
	v.mu.Lock()
	defer v.mu.Unlock()
	if t, ok := v.pending[clientID]; ok {
		t.Stop()
		delete(v.pending, clientID)
	}
}

// endVoiceFor leaves every call the departed client was the last one holding.
// A room another attached window also asked for is left alone: this client is
// gone, that one is not, and the node is still genuinely in the call.
func (b *Bridge) endVoiceFor(clientID string) {
	v := b.voice()
	v.mu.Lock()
	delete(v.pending, clientID)
	var rooms []string
	for ch, holders := range v.rooms {
		if !holders[clientID] {
			continue
		}
		delete(holders, clientID)
		if len(holders) == 0 {
			delete(v.rooms, ch)
			rooms = append(rooms, ch)
		}
	}
	v.mu.Unlock()
	if len(rooms) == 0 {
		return
	}
	svc, err := b.service()
	if err != nil {
		return
	}
	for _, ch := range rooms {
		// LeaveVoice cancels the heartbeat goroutine AND announces the
		// departure, so the room hears about it on the same beat it would have
		// heard another "join".
		_ = svc.LeaveVoice(ch)
	}
}

func (v *voiceOwners) ownsAnyLocked(clientID string) bool {
	for _, holders := range v.rooms {
		if holders[clientID] {
			return true
		}
	}
	return false
}

// voice lazily builds the ownership table. Bridges are constructed in several
// places (each shell has its own) and none of them would remember a new map.
func (b *Bridge) voice() *voiceOwners {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.voiceOwn == nil {
		b.voiceOwn = &voiceOwners{rooms: map[string]map[string]bool{}, pending: map[string]*time.Timer{}}
	}
	return b.voiceOwn
}
