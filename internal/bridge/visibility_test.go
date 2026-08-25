package bridge

import (
	"fmt"
	"testing"
)

// Before anybody has said anything the answer is "foreground", and it has to
// be. Two live cases depend on it: a node booting with no UI attached yet, and
// Android's foreground service running the core with the Activity destroyed —
// where shellBackground is the signal that matters and the vote must not have
// an opinion of its own. It is also the fail-safe for a shell too old to report
// visibility at all: no reports, no throttle, exactly today's behaviour.
func TestNoClientHasSpokenMeansNoOpinion(t *testing.T) {
	if backgroundNow(false, false, nil) {
		t.Error("a node nobody has reported to went to the background beat on its own")
	}
	if !backgroundNow(true, false, nil) {
		t.Error("the native shell said the app is off screen and was overruled by silence")
	}
}

// The multi-client rule. One hidden tab must not put a node to sleep underneath
// a window somebody is typing in.
func TestAnyVisibleClientKeepsTheNodeEager(t *testing.T) {
	visible := map[string]bool{"a": false, "b": true, "c": false}
	if backgroundNow(false, true, visible) {
		t.Error("two hidden tabs outvoted the one the user is actually looking at")
	}
	if !backgroundNow(false, true, map[string]bool{"a": false, "b": false}) {
		t.Error("every attached client is hidden and the node still runs the eager cadence")
	}
}

// A client that disappears entirely stops voting. The case in mind is a laptop
// closing: the browser is gone, so the last thing it said must not be what the
// node believes forever.
func TestAGoneClientStopsVoting(t *testing.T) {
	b := &Bridge{}
	b.AttachClient("laptop")
	b.AttachClient("phone")
	_ = b.SetClientVisible("laptop", true)
	_ = b.SetClientVisible("phone", false)
	if b.background() {
		t.Fatal("a visible client did not hold the node in the foreground")
	}
	b.DropClient("laptop")
	if !b.background() {
		t.Error("the visible client went away and its vote outlived it")
	}
}

// The native shell's veto is absolute, because on Android it is the only signal
// that knows the truth: a WebView considers itself visible in situations where
// the OS has stopped drawing it, so an ANDed vote is what keeps the phone
// behaving exactly as it did before any of this existed.
func TestTheNativeShellCanVetoAVisibleWebView(t *testing.T) {
	b := &Bridge{}
	b.AttachClient("webview")
	_ = b.SetClientVisible("webview", true)
	if b.background() {
		t.Fatal("a visible client should hold the foreground cadence")
	}
	_ = b.SetForeground(false)
	if !b.background() {
		t.Error("the Activity stopped and the WebView's own opinion overrode it")
	}
	_ = b.SetForeground(true)
	if b.background() {
		t.Error("the app came back and the node stayed throttled")
	}
}

// A reconnecting EventSource must not fall out of the vote. It drops and
// redials on its own; a client left out until its next visibility change is a
// client on screen that never reports again.
func TestReattachingPutsAClientBackInTheVote(t *testing.T) {
	b := &Bridge{}
	b.AttachClient("tab")
	_ = b.SetClientVisible("tab", true)
	b.DropClient("tab")
	if !b.background() {
		t.Fatal("the only client is gone and the node is still eager")
	}
	b.AttachClient("tab")
	if b.background() {
		t.Error("the stream came back and the client was left out of the vote")
	}
}

// An attached client that reported hidden and then reconnects keeps whatever it
// says next; what must not happen is AttachClient silently overwriting a
// standing "I am hidden" for a client already in the vote.
func TestAttachDoesNotOverruleAStandingReport(t *testing.T) {
	b := &Bridge{}
	b.AttachClient("tab")
	_ = b.SetClientVisible("tab", false)
	b.AttachClient("tab")
	if !b.background() {
		t.Error("a re-attach flipped a client that had reported itself hidden back to visible")
	}
}

// An id we cannot drop is an id we must not accept: it would vote forever.
func TestAnUnnameableClientIsNotLetIntoTheVote(t *testing.T) {
	b := &Bridge{}
	b.AttachClient("real")
	_ = b.SetClientVisible("real", false)
	_ = b.SetClientVisible("", true)
	b.AttachClient("")
	if !b.background() {
		t.Error("an anonymous report joined the vote and pinned the node to the foreground")
	}
}

func TestTheVoteIsBounded(t *testing.T) {
	b := &Bridge{}
	for i := 0; i < maxVisibilityClients*3; i++ {
		b.AttachClient(fmt.Sprintf("client-%d", i))
	}
	b.mu.Lock()
	n := len(b.clientVisible)
	b.mu.Unlock()
	if n > maxVisibilityClients {
		t.Errorf("the vote grew to %d clients, past the %d cap", n, maxVisibilityClients)
	}
}

// The Wails desktop leaves a hidden entry behind on every reload — it has no
// /events stream to drop it — so a full vote must make room by evicting one of
// those rather than turning the live client away. Turning it away would leave
// the node throttled while somebody is looking straight at it.
func TestAFullVoteEvictsAHiddenClientRatherThanRefuseANewOne(t *testing.T) {
	b := &Bridge{}
	for i := 0; i < maxVisibilityClients; i++ {
		id := fmt.Sprintf("stale-%d", i)
		b.AttachClient(id)
		_ = b.SetClientVisible(id, false) // what pagehide leaves behind
	}
	if !b.background() {
		t.Fatal("a vote of nothing but hidden clients should have settled the node")
	}
	_ = b.SetClientVisible("the-live-one", true)
	if b.background() {
		t.Error("a full vote turned away the one client actually on screen")
	}
	b.mu.Lock()
	n := len(b.clientVisible)
	b.mu.Unlock()
	if n > maxVisibilityClients {
		t.Errorf("the vote grew to %d, past the %d cap", n, maxVisibilityClients)
	}
}

// Eviction must never drop a client that says it IS looking: that is the one
// vote that can change the verdict.
func TestAFullVoteOfVisibleClientsEvictsNobody(t *testing.T) {
	b := &Bridge{}
	for i := 0; i < maxVisibilityClients; i++ {
		b.AttachClient(fmt.Sprintf("watcher-%d", i))
	}
	_ = b.SetClientVisible("one-more", true)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.clientVisible) != maxVisibilityClients {
		t.Errorf("the vote holds %d clients, want the %d cap unchanged",
			len(b.clientVisible), maxVisibilityClients)
	}
	for id, visible := range b.clientVisible {
		if !visible {
			t.Errorf("client %s was left hidden; a visible client was evicted for it", id)
		}
	}
}

// background reads the verdict the way applyVisibility computes it, without
// needing a running service.
func (b *Bridge) background() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return backgroundNow(b.shellBackground, b.heardClient, b.clientVisible)
}
