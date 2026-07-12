package app

import (
	"encoding/json"
	"time"

	"github.com/zahak/concord/internal/domain"
)

// Read state ("which channels have I read, through when") is account-level
// state, not per-client state: reading a message in one window must clear the
// unread badge everywhere, fast. It is held in the encrypted store (so every
// session served by this backend shares it), surfaced to all connected UIs via
// the read-state event, and propagated to the account's OTHER devices over the
// Notes self-DM's meta topic — a group whose only members are our own devices,
// so read receipts never leave the account. Times are UnixMilli; newest wins,
// which makes concurrent markers from several devices converge cleanly.

// OnReadState registers a callback fired whenever a channel's read cursor
// advances — locally or from another device.
func (s *Service) OnReadState(fn func(channelID string, at int64)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onReadState = append(s.onReadState, fn)
}

func (s *Service) emitReadState(channelID string, at int64) {
	s.mu.RLock()
	cbs := append([]func(string, int64){}, s.onReadState...)
	s.mu.RUnlock()
	for _, fn := range cbs {
		fn(channelID, at)
	}
}

// MarkRead records that the user has read channelID through at (UnixMilli).
// Stale or repeated marks are no-ops; an advancing mark is broadcast to every
// connected UI session and to the account's other devices.
func (s *Service) MarkRead(channelID string, at int64) error {
	if channelID == "" || at <= 0 {
		return nil
	}
	advanced, err := s.store.AdvanceReadState(channelID, at)
	if err != nil || !advanced {
		return err
	}
	s.emitReadState(channelID, at)
	s.broadcastReadMarker(channelID, at)
	return nil
}

// ReadState returns every channel's read-through time (UnixMilli).
func (s *Service) ReadState() (map[string]int64, error) {
	return s.store.ReadState()
}

// applyRemoteReadMarker folds in a read marker authored by another of this
// account's devices (authenticated in receiveGuildMeta). It never re-broadcasts
// — the origin device already told everyone.
func (s *Service) applyRemoteReadMarker(channelID string, at int64) {
	advanced, err := s.store.AdvanceReadState(channelID, at)
	if err != nil || !advanced {
		return
	}
	s.emitReadState(channelID, at)
}

// broadcastReadMarker queues a read marker for the account's other devices.
// Markers coalesce for a short beat so a burst (opening a channel, Shift+Esc
// mark-all-read) travels as ONE encrypted publish instead of dozens.
func (s *Service) broadcastReadMarker(channelID string, at int64) {
	s.readMarkMu.Lock()
	if s.pendingReadMark == nil {
		s.pendingReadMark = map[string]int64{}
	}
	if at > s.pendingReadMark[channelID] {
		s.pendingReadMark[channelID] = at
	}
	if s.readMarkTimer == nil {
		s.readMarkTimer = time.AfterFunc(1200*time.Millisecond, s.flushReadMarkers)
	}
	s.readMarkMu.Unlock()
}

// flushReadMarkers publishes the queued markers to the Notes self-group. No
// Notes group (or nothing queued) simply means nothing to sync to —
// single-device accounts pay nothing.
func (s *Service) flushReadMarkers() {
	s.readMarkMu.Lock()
	markers := s.pendingReadMark
	s.pendingReadMark = nil
	s.readMarkTimer = nil
	s.readMarkMu.Unlock()
	if len(markers) == 0 {
		return
	}

	s.mu.RLock()
	var notes *domain.Guild
	for _, g := range s.guilds {
		if g.Kind == "dm" && len(g.OwnerID) > 0 && string(g.OwnerID) == string(s.PublicKey()) && g.Name == notesGuildName {
			notes = g
			break
		}
	}
	s.mu.RUnlock()
	if notes == nil {
		return
	}
	payload, _ := json.Marshal(guildMeta{Type: "read_marker", Markers: markers})
	if ct, err := s.mls.Encrypt(s.ctx, notes.GroupID, payload); err == nil {
		_ = s.ps.Publish(s.ctx, domain.GuildMetaTopicID(notes.GroupID), ct)
	}
}
