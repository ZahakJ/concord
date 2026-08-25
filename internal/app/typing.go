package app

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
)

// Typing indicators are the one presence signal Concord broadcasts that the
// user has no say over, and the switch is reciprocal: turn it off and you stop
// sending "typing…" AND stop seeing it. Reciprocity is the honest arrangement
// (it's what Signal settled on), and it is the only one that survives here —
// there is no server to enforce a one-way deal, so a client that kept receiving
// while withholding would just be a client that lies to its friends.
//
// Read receipts need no equivalent switch: Concord's read state never leaves
// the account. It is published to the Notes self-group — a group whose only
// members are your own devices — so nobody else was ever told what you read.

// typingPrefKey persists the switch. Absent means on: indicators are what chat
// has felt like since 1996, and a privacy default that silently changes how a
// friend group reads each other is a worse surprise than the setting is a win.
const typingPrefKey = "typing_indicators"

// receiveTyping attributes an inbound typing signal to a member account and
// surfaces it — or drops it, because the two failure modes of "just emit what
// the PeerID says" both put an encoded stranger in the composer:
//
//   - A linked device's PeerID is its DEVICE key. presence() maps it to the
//     account via s.deviceAccounts, but when the certificate hasn't been
//     learned yet (their phone's add-commit is applied on another goroutine a
//     moment after the phone's first keystroke arrives) the fallback is the
//     fingerprint of the raw device key — an identity belonging to no member,
//     rendered as a truncated raw key. The roster in hand carries every leaf's
//     device certificate, so on a miss we re-read it once and retry.
//   - The typing topic is plaintext gossip: anyone who learns the topic ID can
//     publish to it. Requiring the resolved account to be a current member of
//     THIS guild means an outsider's signal shows nothing at all instead of an
//     unnameable ghost typing in the channel.
//
// A signal from your OWN account's other device is surfaced too, attributed to
// the account like anyone else's. v0.49 suppressed it ("you know you are
// typing"), and the user overruled that: seeing your own name light up when
// you type on your phone is confirmation the devices are talking, and hiding
// it read as breakage, not politeness. What stays fixed from v0.49 is the
// attribution — the ACCOUNT name, never the phone's raw device key.
//
// When attribution fails even after the roster re-read — our copy of the
// roster may simply not include the sender's add-commit yet — the signal is
// dropped, but we also ask the sender to introduce itself (solicitHello, an
// empty hello frame that reveals nothing of us). A member device answers with
// its certificate and the NEXT keystroke attributes; an outsider answers
// nothing and has cost us one stream on this connection, ever.
func (s *Service) receiveTyping(guildID string, groupID []byte, channelID string, from peer.ID) {
	fpr := s.presence(from).Fingerprint
	if !s.guildHasMember(guildID, fpr) {
		// Unlearned linked device, or an outsider. The roster is the authority
		// and carries the device certs; re-read it once and re-resolve.
		s.relearnDevices(groupID)
		fpr = s.presence(from).Fingerprint
		if !s.guildHasMember(guildID, fpr) {
			// Cannot attribute: show nothing rather than a raw key — but kick a
			// hello at the sender so attribution converges in seconds instead of
			// on the next reconnect. Off this goroutine: it dials.
			go s.solicitHello(from)
			return
		}
	}
	s.emitTyping(fpr, channelID)
}

// ---- typing topics and the screen ----
//
// A typing indicator is pure UX for a screen somebody is looking at. Nobody
// types into a backgrounded app, and nobody reads "Sara is typing…" on a phone
// in a pocket — so while the app is off screen those topics are carrying gossip
// that cannot possibly be used.
//
// The cost is not the typing hints themselves, which are rare and one byte
// long. It is that a joined gossipsub topic is a MESH: it wakes on every
// heartbeat, maintains its own set of peers, and emits its own gossip whether
// or not a single message ever crosses it. Concord opens two topics per channel,
// so half of every guild's per-channel gossip floor is typing — and it was
// running all night. Leaving them is the one saving available that costs the
// user nothing at all, because the feature is invisible in exactly the state
// where we switch it off.
//
// Keyed off the existing background flag rather than a new one. Today only the
// Android shell ever sets it; a desktop that learns to report its window
// minimised gets this behaviour for free the day it does.

// subscribeTyping joins a channel's typing topic, unless the app is off screen —
// in which case the topic is deliberately not meshed and syncTypingTopics will
// join it when the user comes back. Idempotent: PubSub.Subscribe returns
// immediately if we are already subscribed.
func (s *Service) subscribeTyping(guildID string, groupID []byte, channelID string) {
	if s.backgrounded() {
		return
	}
	_ = s.ps.Subscribe(s.ctx, domain.TypingTopicID(groupID, channelID), func(from peer.ID, _ []byte) {
		s.receiveTyping(guildID, groupID, channelID, from)
	})
}

// syncTypingTopics joins or leaves every channel's typing topic to match
// whether the app is on screen. Called on both edges of SetBackground.
//
// It re-reads the flag under its own lock rather than taking a direction from
// the caller, so two screen transitions arriving close together converge on the
// truth instead of finishing in whichever order their goroutines happened to
// run.
//
// Re-joining is clean but not instant: leaving a topic puts us in the other
// peers' unsubscribe backoff for ten seconds, so a GRAFT sent immediately after
// is refused and the mesh re-forms on the next heartbeat past it. For a typing
// indicator that is invisible — the user has just unlocked their phone and is
// reading, not watching for a composer dot — and it is a reason not to flap the
// flag, not a reason to keep forty-two meshes alive overnight.
func (s *Service) syncTypingTopics() {
	s.typingMu.Lock()
	defer s.typingMu.Unlock()
	want := !s.backgrounded()

	type room struct {
		guildID   string
		groupID   []byte
		channelID string
	}
	var rooms []room
	s.mu.RLock()
	for _, g := range s.guilds {
		for _, c := range g.Channels {
			rooms = append(rooms, room{g.ID, g.GroupID, c.ID})
		}
	}
	s.mu.RUnlock()

	for _, c := range rooms {
		if want {
			_ = s.ps.Subscribe(s.ctx, domain.TypingTopicID(c.groupID, c.channelID), func(from peer.ID, _ []byte) {
				s.receiveTyping(c.guildID, c.groupID, c.channelID, from)
			})
			continue
		}
		s.ps.Unsubscribe(domain.TypingTopicID(c.groupID, c.channelID))
	}
}

// loadTypingPref mirrors the persisted switch into memory at startup.
func (s *Service) loadTypingPref() {
	v, _ := s.store.GetSetting(typingPrefKey)
	s.mu.Lock()
	s.typingOn = v != "0"
	s.mu.Unlock()
}

// TypingEnabled reports whether typing indicators are exchanged.
func (s *Service) TypingEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.typingOn
}

// SetTypingEnabled flips the switch and persists it. It takes effect at once,
// in both directions.
func (s *Service) SetTypingEnabled(on bool) error {
	val := "0"
	if on {
		val = "1"
	}
	if err := s.store.SetSetting(typingPrefKey, val); err != nil {
		return err
	}
	s.mu.Lock()
	s.typingOn = on
	s.mu.Unlock()
	return nil
}
