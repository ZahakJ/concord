package app

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

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
	s.pushReadMarksToDevices(markers)
}

// helloReadMarks bounds how many marks one hello carries: the newest ones,
// which are the channels anyone is still reading. Older marks still converge
// the moment either device marks those channels again.
const helloReadMarks = 512

// helloFrameBudget keeps a hello under the 64 KiB frame every build's hello
// stream accepts (net.maxHelloFrame), with room to spare. Past it the whole
// frame is dropped by the reader, invites and profile included, so read marks
// are what gives way.
const helloFrameBudget = 60 << 10

// readMarksFor returns the account's read marks to offer a peer, or nil for
// anyone who has not proven itself a current device of THIS account. Which
// channels someone has read, and when, is nobody else's business.
func (s *Service) readMarksFor(p peer.ID) map[string]int64 {
	if !s.ownDevice(p) || s.revocationFor(p) != nil {
		return nil
	}
	rs, err := s.store.ReadState()
	if err != nil || len(rs) == 0 {
		return nil
	}
	return newestMarks(rs, helloReadMarks)
}

// adoptOfferedReadMarks folds in marks that rode in on a hello, from a proven
// device of this account only, by the same max-and-cap rule as a live marker.
func (s *Service) adoptOfferedReadMarks(from peer.ID, marks map[string]int64) {
	if len(marks) == 0 || !s.ownDevice(from) || s.revocationFor(from) != nil {
		return
	}
	for ch, at := range marks {
		if ch != "" && at > 0 {
			s.applyRemoteReadMarker(ch, at)
		}
	}
}

// newestMarks keeps the n most recent marks.
func newestMarks(rs map[string]int64, n int) map[string]int64 {
	if len(rs) <= n {
		return rs
	}
	chans := make([]string, 0, len(rs))
	for ch := range rs {
		chans = append(chans, ch)
	}
	sort.Slice(chans, func(i, j int) bool { return rs[chans[i]] > rs[chans[j]] })
	out := make(map[string]int64, n)
	for _, ch := range chans[:n] {
		out[ch] = rs[ch]
	}
	return out
}

// marshalHello encodes a hello frame, halving the read marks it carries
// until the frame fits helloFrameBudget.
func marshalHello(f helloFrame) ([]byte, error) {
	for {
		b, err := json.Marshal(f)
		if err != nil || len(b) <= helloFrameBudget || len(f.ReadMarks) == 0 {
			return b, err
		}
		f.ReadMarks = newestMarks(f.ReadMarks, len(f.ReadMarks)/2)
	}
}

// pushReadMarksToDevices hands markers to every connected device of this
// account over the hello stream as well as the publish. The publish on the
// Notes meta topic is gossip with no catch-up, and it is least reliable exactly
// when a second device is new: one linked a moment ago may not be listening
// yet, and while the two settle their connections the topic can list a device
// whose copy is then lost (in a traced failure the desktop listed the phone at
// the publish and not a moment later, and the marker never arrived). A hello
// is a stream to that one device, retried on open, and cheap: a few hundred
// bytes per batch per device, and single-device accounts send nothing. Older
// devices ignore the field and still get the publish.
func (s *Service) pushReadMarksToDevices(markers map[string]int64) {
	for _, p := range s.host.Peers() {
		if !s.ownDevice(p) || s.revocationFor(p) != nil {
			continue
		}
		go s.sayReadMarks(p, markers)
	}
}

// sayReadMarks sends a hello carrying only our credential and the markers. The
// receiver adopts them exactly as from any hello; an older build ignores them.
//
// Tried a few times. Two devices that dialled each other at once can be left
// with a connection one side has already dropped and the other still writes
// to; the first stream on it fails ("stateless reset", in the traced case),
// and that failure is what retires it, so the next attempt finds a live one.
// The same dead connection is what swallowed the publish.
func (s *Service) sayReadMarks(p peer.ID, markers map[string]int64) {
	req, err := marshalHello(helloFrame{Credential: s.myCredential, ReadMarks: markers})
	if err != nil {
		return
	}
	for attempt, wait := 0, time.Second; attempt < sayReadMarksTries; attempt, wait = attempt+1, wait*2 {
		if attempt > 0 {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(wait):
			}
			if !s.ownDevice(p) {
				return
			}
		}
		ctx, cancel := context.WithTimeout(s.ctx, helloTimeout)
		resp, err := s.host.SayHello(ctx, p, req)
		cancel()
		if err == nil {
			s.ingestHello(p, resp)
			return
		}
	}
}

// sayReadMarksTries bounds the attempts: 1 s and 2 s apart, then the marks
// wait for the next batch or the next connection's hello.
const sayReadMarksTries = 3
