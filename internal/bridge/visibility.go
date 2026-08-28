package bridge

// Who is actually looking at this node.
//
// The core slows every periodic loop to one shared multi-minute beat when the
// app is off screen (see internal/app/background.go). Deciding when that is
// true used to be Android's job alone: MainActivity.onStart/onStop called
// SetForeground, and no other shell called anything. A minimised desktop window
// and a browser tab left open in the background therefore ran the full
// foreground cadence forever — a DHT walk every fifteen seconds, guild
// reconcile, own-device dials — for a UI nobody had looked at since Tuesday.
//
// The awkward part is that "the app" is not one thing here. A single node can
// have several browser sessions attached at once, the Wails desktop window is
// just another client of the same /rpc surface, and on Android the node
// routinely outlives its WebView entirely (the foreground service keeps the
// process while the Activity is destroyed). So the answer cannot be "whatever
// the last caller said" — one hidden tab would put the node to sleep underneath
// a window somebody is typing in.
//
// Two independent inputs, ANDed:
//
//   - shellBackground, from SetForeground. Native lifecycle, Android only. It
//     is the only signal that knows the app has actually left the screen: an
//     Android WebView still considers itself perfectly visible in situations
//     where the OS has stopped drawing it, so a vote of the attached UIs alone
//     cannot be trusted to notice. When it says the app is off screen, that
//     wins outright.
//
//   - the client vote, from SetClientVisible. Any-visible-wins across every
//     attached UI, so the node stays eager while one of several tabs is on
//     screen and settles only when the last one is hidden.
//
// A client leaves the vote when its /events stream ends, which is what makes a
// closed laptop different from a hidden tab: a hidden tab votes "not looking"
// and a closed one stops voting at all. Without that, the last thing a
// disappearing client ever said would pin the node's cadence forever.

// maxVisibilityClients bounds the vote. The RPC surface is loopback plus a
// bearer token, and the ids come from our own frontend, so this is not a threat
// model so much as a refusal to let a map grow for the life of the process.
// Sixty-four simultaneously attached UIs is already far past absurd.
//
// It can be reached honestly, by one shell: the Wails desktop takes its events
// over the native runtime rather than /events, so it has no stream whose
// closing would drop it, and every reload of that webview mints a fresh id.
// The outgoing document's pagehide leaves the old id behind reporting hidden,
// which is harmless to the verdict but still occupies a slot.
//
// So a full vote evicts a client that is reporting hidden rather than refusing
// the newcomer. Only ever a hidden one: dropping a voter that says nobody is
// looking cannot change any-visible-wins, whereas turning away a new client
// could leave a node throttled while somebody is staring at it — the worst
// failure this feature has available. With no hidden client to evict the vote
// is genuinely full of people looking at it, and one more makes no difference.
const maxVisibilityClients = 64

// maxClientIDLen bounds one id. A UUID is 36 characters.
const maxClientIDLen = 64

// SetForeground is the native mobile shell reporting whether the app is on
// screen (Activity onStart/onStop — which covers backgrounding AND the screen
// turning off). Off screen, the core's periodic loops slow to one shared beat
// so the radio can sleep; connections, gossip delivery and the relay
// reservation are untouched, and returning to the foreground restores the eager
// cadence immediately. Safe to call while locked — the choice is remembered and
// applied when the service starts.
//
// Desktop and web shells never call this. They vote through SetClientVisible
// instead, and their shellBackground stays false, so the vote alone decides —
// which is right, because on those platforms the page's own visibilitychange is
// the whole truth about whether anyone is looking.
func (b *Bridge) SetForeground(fg bool) error {
	b.mu.Lock()
	b.shellBackground = !fg
	b.mu.Unlock()
	b.applyVisibility()
	return nil
}

// SetClientVisible is one attached UI reporting its own document visibility.
// clientID is the per-page identity the frontend mints and also hands to
// /events, so this report and that stream's lifetime describe the same client.
//
// An empty id is ignored rather than treated as a client: a caller that cannot
// name itself cannot be dropped from the vote when it goes away, and a vote
// entry that can never be removed is exactly the bug this design exists to
// avoid.
func (b *Bridge) SetClientVisible(clientID string, visible bool) error {
	if clientID == "" || len(clientID) > maxClientIDLen {
		return nil
	}
	b.mu.Lock()
	if b.clientVisible == nil {
		b.clientVisible = map[string]bool{}
	}
	if _, known := b.clientVisible[clientID]; known || b.roomInVoteLocked() {
		b.clientVisible[clientID] = visible
	}
	b.heardClient = true
	b.mu.Unlock()
	b.applyVisibility()
	return nil
}

// AttachClient enters a client into the vote as visible, called when its
// /events stream opens. Without it, a stream that drops and reconnects — an
// EventSource does that on its own — would leave a client that IS on screen out
// of the vote until its next visibility change, which for a tab somebody is
// reading is never.
//
// Presumed visible rather than presumed hidden because that is the fail-safe
// direction: the cost of being wrong is a node that stays eager slightly too
// long, and the client's own report corrects it within a frame.
func (b *Bridge) AttachClient(clientID string) {
	if clientID == "" || len(clientID) > maxClientIDLen {
		return
	}
	b.mu.Lock()
	if b.clientVisible == nil {
		b.clientVisible = map[string]bool{}
	}
	if _, known := b.clientVisible[clientID]; !known && b.roomInVoteLocked() {
		b.clientVisible[clientID] = true
	}
	b.heardClient = true
	b.mu.Unlock()
	// The same stream that decides the vote decides how long a call this client
	// started lives (voicelife.go). A redialing EventSource lands here, so this
	// is where the hang-up grace gets called off.
	b.voiceClientBack(clientID)
	b.applyVisibility()
}

// DropClient removes a client from the vote, called when its /events stream
// ends — the tab was closed, the browser quit, the laptop lid came down, the
// process was killed. The distinction this draws is the whole point: a hidden
// client votes, a gone one does not.
func (b *Bridge) DropClient(clientID string) {
	if clientID == "" {
		return
	}
	b.mu.Lock()
	_, known := b.clientVisible[clientID]
	delete(b.clientVisible, clientID)
	b.mu.Unlock()
	// A stream that ends is also the deadline on any call this client started —
	// after a grace, in case the EventSource is merely redialing. See
	// voicelife.go.
	b.voiceClientGone(clientID)
	if known {
		b.applyVisibility()
	}
}

// roomInVoteLocked makes space for one more client if it can, evicting a hidden
// voter when the map is full. Caller holds b.mu.
func (b *Bridge) roomInVoteLocked() bool {
	if len(b.clientVisible) < maxVisibilityClients {
		return true
	}
	for id, visible := range b.clientVisible {
		if !visible {
			delete(b.clientVisible, id)
			return true
		}
	}
	return false
}

// applyVisibility recomputes the verdict and hands it to the service. Cheap to
// call on every report: Service.SetBackground is idempotent and returns
// immediately unless the answer actually changed.
func (b *Bridge) applyVisibility() {
	b.mu.Lock()
	svc := b.svc
	bg := backgroundNow(b.shellBackground, b.heardClient, b.clientVisible)
	b.mu.Unlock()
	if svc != nil {
		svc.SetBackground(bg)
	}
}

// clientsAwake reports the attached-client half of the vote.
//
// heard is "has any client ever reported", and it is what makes an empty map
// mean two different things. Before the first report there is no opinion to
// have — the node may be booting with no UI yet, or running headless behind
// Android's foreground service — and the answer is the status quo, foreground.
// It is also the fail-safe: a shell that never learns to report visibility (an
// older bundle cached in the Android assets, say) keeps exactly today's
// behaviour instead of being throttled forever by a vote it cannot join.
//
// After the first report an empty map means every client that was attached has
// gone, and nobody is looking.
func clientsAwake(heard bool, visible map[string]bool) bool {
	if !heard {
		return true
	}
	for _, v := range visible {
		if v {
			return true
		}
	}
	return false
}

// backgroundNow is the whole decision: the native shell can veto on its own,
// and otherwise the attached UIs decide by any-visible-wins.
func backgroundNow(shellBackground, heard bool, visible map[string]bool) bool {
	return shellBackground || !clientsAwake(heard, visible)
}
