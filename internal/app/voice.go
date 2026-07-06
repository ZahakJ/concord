package app

import (
	"context"
	"encoding/json"
	"fmt"
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

// voiceAnnounce is the gossipsub presence message for a voice room.
type voiceAnnounce struct {
	ChannelID string `json:"channelId"`
	Action    string `json:"action"` // "join" | "leave"
}

// JoinVoice enters the voice room for a channel: it subscribes to the room's
// presence topic and periodically announces its presence so peers (including
// late joiners) discover it. Idempotent per channel.
func (s *Service) JoinVoice(channelID string) error {
	groupID, err := s.groupForChannel(channelID)
	if err != nil {
		return err
	}

	s.voiceMu.Lock()
	if _, in := s.voiceRooms[channelID]; in {
		s.voiceMu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.voiceRooms[channelID] = cancel
	s.voiceMu.Unlock()

	topic := domain.VoiceTopicID(groupID, channelID)
	if err := s.ps.Subscribe(s.ctx, topic, func(from peer.ID, data []byte) {
		var a voiceAnnounce
		if json.Unmarshal(data, &a) == nil && a.ChannelID == channelID {
			s.emitVoicePresence(from.String(), channelID, a.Action)
		}
	}); err != nil {
		return err
	}

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
func (s *Service) RelaySignal(toPeerID string, data []byte) error {
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
func (s *Service) OnVoicePresence(fn func(from, channelID, action string)) {
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

func (s *Service) emitVoicePresence(from, channelID, action string) {
	s.mu.RLock()
	cbs := append([]func(string, string, string){}, s.onVoicePresence...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(from, channelID, action)
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
