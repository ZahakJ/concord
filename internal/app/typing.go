package app

import "github.com/libp2p/go-libp2p/core/peer"

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
