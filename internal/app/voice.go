package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/domain"
)

// Voice/video is a browser-to-browser WebRTC mesh. Go's only jobs are:
//   1. Presence — announce who is in a channel's voice room (over gossipsub), so
//      each peer knows whom to open a media connection with.
//   2. Signaling relay — carry the WebRTC SDP/ICE blobs between two peers (over
//      the libp2p signal protocol), so their browsers can establish the direct,
//      end-to-end-encrypted (DTLS-SRTP) media connection.
//
// The audio/video itself never passes through Go; it flows peer-to-peer between
// the browsers. This keeps the backend simple (no native audio stack) and works
// identically in the browser-served and Wails desktop front ends.

const voiceHeartbeat = 3 * time.Second

// voiceAnnounce is the gossipsub presence message for a voice room. It also
// carries the "soft lock" control actions (lock/unlock/knock/admit) on the same
// topic — everyone watching the channel already receives it, so no new plumbing
// and, crucially, no changes to the WebRTC media path.
type voiceAnnounce struct {
	ChannelID string `json:"channelId"`
	// "join"|"leave"|"lock"|"unlock"|"knock"|"admit"|"move"|"disconnect"
	//
	// Two more actions exist but are deliberately NEVER published: "unknock"
	// (a knock withdrawn) and "refuse" (a knock declined) are emitted locally
	// for browser guests only, whose sessions live on this node alone. Putting
	// them on the topic would reach clients that treat an unknown action as a
	// join and grow a phantom participant.
	Action string `json:"action"`
	// The fingerprint being acted on: admitted, moved, or disconnected.
	Target string `json:"target,omitempty"`
	// "move": the voice channel to send them to.
	Dest string `json:"dest,omitempty"`
}

// watchVoice passively subscribes to a voice channel's presence topic so this
// peer learns who is in the call WITHOUT joining it (guild-wide presence, like
// Discord's sidebar). Called for every voice channel in every guild we're in;
// idempotent per channel.
func (s *Service) watchVoice(groupID []byte, channelID string) {
	s.voiceMu.Lock()
	if s.voiceWatched[channelID] {
		s.voiceMu.Unlock()
		return
	}
	s.voiceWatched[channelID] = true
	s.voiceMu.Unlock()

	topic := domain.VoiceTopicID(groupID, channelID)
	_ = s.ps.Subscribe(s.ctx, topic, func(from peer.ID, data []byte) {
		var a voiceAnnounce
		if json.Unmarshal(data, &a) == nil && a.ChannelID == channelID {
			// s.presence, NOT presenceFor: the fingerprint here is what the UI
			// names the participant by, checks permissions against, and compares
			// with its own to decide "this is me on another device". presenceFor
			// reads the key out of the PeerID, which for a LINKED device is that
			// device's own key — an account nobody has ever heard of. So your own
			// phone joined the call as a nameless stranger, on a client that had a
			// perfectly good "(other device)" label ready for it, and a moderator's
			// permissions were checked against an identity with no roles.
			s.emitVoicePresence(from.String(), s.presence(from).Fingerprint, channelID, a.Action, a.Target, a.Dest)
		}
	})
}

// JoinVoice enters the voice room for a channel: it periodically announces its
// presence so peers (including late joiners) discover it and open a media
// connection. Presence listening is already handled by watchVoice. Idempotent.
func (s *Service) JoinVoice(channelID string) error {
	groupID, err := s.groupForChannel(channelID)
	if err != nil {
		return err
	}
	// Make sure we're listening (normally already are via trackGuild/addChannel).
	s.watchVoice(groupID, channelID)

	s.voiceMu.Lock()
	if _, in := s.voiceRooms[channelID]; in {
		s.voiceMu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.voiceRooms[channelID] = cancel
	s.voiceMu.Unlock()

	topic := domain.VoiceTopicID(groupID, channelID)
	// Announce immediately, then heartbeat until we leave.
	s.announceVoice(topic, channelID, "join")
	go func() {
		t := time.NewTicker(voiceHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.announceVoice(topic, channelID, "join")
			}
		}
	}()
	return nil
}

// reannounceVoice re-publishes our presence in every room we are in.
//
// Called when a peer connects. Gossip is fire-and-forget and the heartbeat is
// every three seconds, so a peer that arrives just after a beat waits out the
// rest of it before it can know we are in a call — on top of however long it
// took to arrive. One publish per room on connect costs nothing and removes that
// whole window, which is the difference between "I joined the call and my
// desktop knew" and "I joined the call and then waited".
func (s *Service) reannounceVoice() {
	s.voiceMu.Lock()
	rooms := make([]string, 0, len(s.voiceRooms))
	for ch := range s.voiceRooms {
		rooms = append(rooms, ch)
	}
	s.voiceMu.Unlock()
	for _, ch := range rooms {
		if groupID, err := s.groupForChannel(ch); err == nil {
			s.announceVoice(domain.VoiceTopicID(groupID, ch), ch, "join")
		}
	}
}

// LeaveVoice leaves a channel's voice room, announcing departure.
func (s *Service) LeaveVoice(channelID string) error {
	s.voiceMu.Lock()
	cancel, in := s.voiceRooms[channelID]
	if in {
		delete(s.voiceRooms, channelID)
	}
	s.voiceMu.Unlock()
	if !in {
		return nil
	}
	cancel()

	if groupID, err := s.groupForChannel(channelID); err == nil {
		s.announceVoice(domain.VoiceTopicID(groupID, channelID), channelID, "leave")
	}
	return nil
}

// RelaySignal forwards an opaque WebRTC signaling blob to a specific peer.
//
// A browser guest in a meeting is a peer too — "guest:<session>" — but it has
// no libp2p identity, so its signaling goes back down the guest stream it
// arrived on (see guest.go). The mesh upstairs never has to know the
// difference: media is still direct, P2P, DTLS-SRTP.
func (s *Service) RelaySignal(toPeerID string, data []byte) error {
	if strings.HasPrefix(toPeerID, "guest:") {
		return s.relayToGuest(toPeerID, data)
	}
	pid, err := peer.Decode(toPeerID)
	if err != nil {
		return fmt.Errorf("app: bad peer id: %w", err)
	}
	return s.host.SendSignal(s.ctx, pid, data)
}

func (s *Service) announceVoice(topic, channelID, action string) {
	payload, _ := json.Marshal(voiceAnnounce{ChannelID: channelID, Action: action})
	_ = s.ps.Publish(s.ctx, topic, payload)
}

// PublishCallControl broadcasts a call control action (lock/unlock/knock/admit,
// or a moderator's move/disconnect) on a channel's voice topic. It's advisory —
// a well-behaved client honors a lock and knocks instead of barging in, and
// obeys a move only from someone it can verify holds the authority. Media
// negotiation is untouched.
//
// `target` is the fingerprint being acted on (admitted, moved, disconnected);
// `dest` is the channel a "move" sends them to. Authority is deliberately NOT
// checked here: the sender's own claim would prove nothing. Every receiver
// checks the sender's permissions against its own copy of the guild's
// governance state before obeying (see the voice-presence handler).
func (s *Service) PublishCallControl(channelID, action, target, dest string) error {
	// Browser guests ride the same verbs, but none of it goes on the wire: a
	// guest is a socket held by THIS node (guest.go), so this node is the only
	// one that can act on them and the only one that needs to hear about it.
	switch action {
	case "lock", "unlock":
		// The lock the user just set is also the guest door. Recorded from the
		// LOCAL action only — see noteGuestDoor for why a remote member's lock
		// does not decide who this node lets into its own relayed session.
		s.noteGuestDoor(channelID, action == "lock")
	case "admit", "refuse", "disconnect", "move":
		if strings.HasPrefix(target, "guest:") {
			// "move" is meaningless for a guest: their whole visit is scoped to one
			// meeting channel, so decideGuest ignores it rather than pretending.
			s.decideGuest(channelID, action, target)
			return nil
		}
		if action == "refuse" {
			// Refusal exists for guests, who are otherwise left hanging on an open
			// socket. A member's ignored knock just times out on their side, and
			// broadcasting an unknown verb would land in older clients' voice
			// rosters as a phantom participant.
			return nil
		}
	}
	groupID, err := s.groupForChannel(channelID)
	if err != nil {
		return err
	}
	s.watchVoice(groupID, channelID) // ensure we're subscribed so we can publish
	payload, _ := json.Marshal(voiceAnnounce{ChannelID: channelID, Action: action, Target: target, Dest: dest})
	return s.ps.Publish(s.ctx, domain.VoiceTopicID(groupID, channelID), payload)
}

func (s *Service) groupForChannel(channelID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	guildID, ok := s.channelToGuild[channelID]
	if !ok {
		return nil, fmt.Errorf("app: unknown channel %s", channelID)
	}
	return s.guilds[guildID].GroupID, nil
}

// ---- voice event callbacks (wired by the front-end transport) ----

// OnVoicePresence fires when a peer announces joining/leaving a voice room.
// from is the peer ID (used for signaling); fingerprint identifies the account.
func (s *Service) OnVoicePresence(fn func(from, fingerprint, channelID, action, target, dest string)) {
	s.mu.Lock()
	s.onVoicePresence = append(s.onVoicePresence, fn)
	s.mu.Unlock()
}

// OnVoiceSignal fires when a WebRTC signaling blob arrives from a peer.
func (s *Service) OnVoiceSignal(fn func(from string, data []byte)) {
	s.mu.Lock()
	s.onVoiceSignal = append(s.onVoiceSignal, fn)
	s.mu.Unlock()
}

func (s *Service) emitVoicePresence(from, fingerprint, channelID, action, target, dest string) {
	s.mu.RLock()
	cbs := append([]func(string, string, string, string, string, string){}, s.onVoicePresence...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(from, fingerprint, channelID, action, target, dest)
	}
}

func (s *Service) emitVoiceSignal(from string, data []byte) {
	s.mu.RLock()
	cbs := append([]func(string, []byte){}, s.onVoiceSignal...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(from, data)
	}
}
