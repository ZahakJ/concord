package app

import (
	"encoding/json"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// Read state ("which channels have I read, through when") is account-level
// state, not per-client state: reading a message in one window must clear the
// unread badge everywhere, fast. It is held in the encrypted store (so every
// session served by this backend shares it), surfaced to all connected UIs via
// the read-state event, and propagated to the account's OTHER devices over the
// Notes self-DM's meta topic — a group whose only members are our own devices,
// so read receipts never leave the account. Times are UnixMilli; newest wins,
// which makes concurrent markers from several devices converge cleanly.
//
// Newest-wins has one way to go wrong: a mark from the future. The UI used to
// stamp marks with its own clock, so a device running six hours fast marked
// every channel it opened read six hours ahead, and because a mark never moves
// back, everything said in those channels for the next six hours was born read
// on every device of the account. The UI now marks through the newest message
// it showed, by that message's own timestamp, and this file refuses to hold any
// mark further ahead of this device's clock than readSkew — whether it came
// from the UI, from another device, or from a database written before this
// rule (capStoredReadMarks repairs those once, at start).
//
// An older device still on the clock-stamped scheme keeps working: its markers
// are merged here exactly as before, only capped, and the markers it receives
// from us are ordinary values it merges with max as it always has.

// readSkew is how far ahead of this device's clock a read mark may sit. A
// message from a peer whose clock runs a little fast is marked through in full;
// one stamped further ahead can push the mark only this far, so a single
// skewed sender (or device) can hide at most this much of a channel's future.
const readSkew = 2 * time.Minute

// capReadMark applies readSkew against now.
func capReadMark(at int64, now time.Time) int64 {
	if lim := now.Add(readSkew).UnixMilli(); at > lim {
		return lim
	}
	return at
}

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
// connected UI session and to the account's other devices. It returns the mark
// the channel holds afterwards, which is what the caller should believe: it
// may be lower than asked (capped, see readSkew) or higher (already read
// further elsewhere).
func (s *Service) MarkRead(channelID string, at int64) (int64, error) {
	if channelID == "" || at <= 0 {
		return 0, nil
	}
	at = capReadMark(at, time.Now())
	advanced, err := s.store.AdvanceReadState(channelID, at)
	if err != nil {
		return 0, err
	}
	if advanced {
		s.emitReadState(channelID, at)
		s.broadcastReadMarker(channelID, at)
	}
	return s.store.ReadMark(channelID)
}

// MarkReadLatest marks each channel read through the newest message this
// device holds in it, for the callers that have no rows on screen to mark
// through ("Mark as read" on a guild, mark-all-read). Channels holding no
// messages are left alone. Returns the resulting mark per channel.
func (s *Service) MarkReadLatest(channelIDs []string) (map[string]int64, error) {
	newest, err := s.store.NewestSent(channelIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(newest))
	for ch, sent := range newest {
		if sent <= 0 {
			continue
		}
		at, err := s.MarkRead(ch, time.Unix(0, sent).UnixMilli())
		if err != nil {
			return out, err
		}
		out[ch] = at
	}
	return out, nil
}

// capStoredReadMarks repairs marks a clock-stamping UI (or device) left in the
// future. It lowers them to now — everything up to now was already claimed
// read by that mark, so this un-reads nothing that existed — and is the one
// place a mark ever moves back.
func capStoredReadMarks(st *store.Store, now time.Time) {
	_, _ = st.CapReadState(now.Add(readSkew).UnixMilli(), now.UnixMilli())
}

// ReadState returns every channel's read-through time (UnixMilli).
func (s *Service) ReadState() (map[string]int64, error) {
	return s.store.ReadState()
}

// applyRemoteReadMarker folds in a read marker authored by another of this
// account's devices (authenticated in receiveGuildMeta). It never re-broadcasts
// — the origin device already told everyone.
func (s *Service) applyRemoteReadMarker(channelID string, at int64) {
	at = capReadMark(at, time.Now())
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

	// Same predicate as everywhere else, from the one place that defines it.
	// This used to be a second hand-written copy, which meant two loops over an
	// unordered map could disagree about which of two Notes groups was THE one.
	notes, ok := s.findNotesDM()
	if !ok {
		return
	}
	payload, _ := json.Marshal(guildMeta{Type: "read_marker", Markers: markers})
	if ct, err := s.mls.Encrypt(s.ctx, notes.GroupID, payload); err == nil {
		_ = s.ps.Publish(s.ctx, domain.GuildMetaTopicID(notes.GroupID), ct)
	}
}
