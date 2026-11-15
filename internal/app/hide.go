package app

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"context"
	"fmt"
	"time"

	"github.com/ZahakJ/concord/internal/store"
)

// "Delete for me": hide a message (anyone's, in any kind of conversation) on
// this account's devices only. Nothing is sent to the channel or anyone in it.
// The hide is held in the encrypted store, filtered out of every user-facing
// read there (store/hidden.go), and told to the account's OTHER devices the
// way read markers are: a publish on the Notes self-group's meta topic, a group
// whose only members are our own devices, plus a hello stream to each connected
// device for the reasons readstate.go gives (the gossip has no catch-up).
// Receivers accept it only from a leaf of our own account, so no one else can
// hide anything for us.
//
// Each message's state is a timestamped register (store/hidden.go): last writer
// wins on a millisecond stamp, capped against a fast clock like read marks
// (capReadMark). An unhide is a tombstone, so it beats the hide it undoes
// wherever that hide turns up. Live changes are pushed as above; a device that
// was offline catches up on its next hello with any own device, which carries a
// cursor ("send me what changed after stamp N") and is answered with a bounded
// page of registers plus a "more" flag it keeps pulling on. State exchange, not
// op replay, so nothing can be resurrected.
//
// Attachment bytes are deliberately NOT dropped. A blob is content-addressed
// and may be referenced by other, visible messages (a forward re-sends the same
// token), finding out means decrypting the whole history, and this device may
// be the last replica other members fetch it from (handleAttachRequest serves
// our cache). Dropping it would be a guess that can destroy other people's
// copy; the blob stays under the normal LRU instead.

// Bounds: ids per received frame, id length, and registers per page/frame.
const (
	maxHideOps   = 500
	maxHideIDLen = 128
	hidePageSize = 150
)

// OnHidden registers a callback fired when a message is hidden or unhidden,
// locally or from another device.
func (s *Service) OnHidden(fn func(channelID, messageID string, hidden bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onHidden = append(s.onHidden, fn)
}

func (s *Service) emitHidden(channelID, messageID string, hidden bool) {
	s.mu.RLock()
	cbs := append([]func(string, string, bool){}, s.onHidden...)
	s.mu.RUnlock()
	for _, fn := range cbs {
		fn(channelID, messageID, hidden)
	}
}

func validHideID(id string) bool { return id != "" && len(id) <= maxHideIDLen }

// HideMessage hides a message on this account's devices only.
func (s *Service) HideMessage(channelID, messageID string) error {
	return s.setHiddenLocal(channelID, messageID, true)
}

// UnhideMessage undoes HideMessage (the toast's Undo).
func (s *Service) UnhideMessage(channelID, messageID string) error {
	return s.setHiddenLocal(channelID, messageID, false)
}

func (s *Service) setHiddenLocal(channelID, messageID string, hidden bool) error {
	if !validHideID(channelID) || !validHideID(messageID) {
		return fmt.Errorf("app: hide: bad channel or message id")
	}
	rec, changed, err := s.store.LocalHide(channelID, messageID, hidden)
	if err != nil {
		return err
	}
	if changed {
		s.emitHidden(channelID, messageID, hidden)
	}
	s.queueHide(rec)
	return nil
}

// HiddenMessages lists the ids hidden in one channel.
func (s *Service) HiddenMessages(channelID string) ([]string, error) {
	return s.store.HiddenIDs(channelID)
}

// applyRemoteHides folds in registers authored by another of this account's
// devices (authenticated by the caller). Stamps are capped against our clock;
// nothing is re-broadcast.
func (s *Service) applyRemoteHides(recs []store.HideRec) {
	now := time.Now()
	for i, r := range recs {
		if i >= maxHideOps || !validHideID(r.Channel) || !validHideID(r.ID) {
			continue
		}
		r.At = capReadMark(r.At, now)
		if changed, err := s.store.SetHidden(r); err == nil && changed {
			s.emitHidden(r.Channel, r.ID, r.Hidden)
		}
	}
}

// queueHide coalesces changes for a short beat so a burst travels as one publish.
func (s *Service) queueHide(r store.HideRec) {
	s.hideMu.Lock()
	if s.pendingHide == nil {
		s.pendingHide = map[string]store.HideRec{}
	}
	s.pendingHide[r.ID] = r
	if s.hideTimer == nil {
		s.hideTimer = time.AfterFunc(800*time.Millisecond, s.flushHides)
	}
	s.hideMu.Unlock()
}

// flushHides publishes the queued changes to the Notes self-group and pushes
// them to each connected own device. No Notes group means nobody to tell.
func (s *Service) flushHides() {
	s.hideMu.Lock()
	ops := s.pendingHide
	s.pendingHide = nil
	s.hideTimer = nil
	s.hideMu.Unlock()
	if len(ops) == 0 {
		return
	}
	notes, ok := s.findNotesDM()
	if !ok {
		return
	}
	recs := make([]store.HideRec, 0, len(ops))
	for _, r := range ops {
		recs = append(recs, r)
	}
	for len(recs) > 0 {
		n := min(len(recs), hidePageSize)
		chunk := recs[:n]
		recs = recs[n:]
		s.publishMeta(notes.GroupID, guildMeta{Type: "hide_marker", Hides: chunk})
		for _, p := range s.host.Peers() {
			if s.ownDevice(p) && s.revocationFor(p) == nil {
				go s.sayHides(p, chunk)
			}
		}
	}
}

// sayHides is sayReadMarks for hide registers: a hello with only our
// credential and the registers, retried like it.
func (s *Service) sayHides(p peer.ID, recs []store.HideRec) {
	s.sayOwn(p, helloFrame{Credential: s.myCredential, Hides: recs})
}

// sayOwn sends a hello to one own device with the sayReadMarks retry shape and
// folds in the answer.
func (s *Service) sayOwn(p peer.ID, f helloFrame) {
	req, err := marshalHello(f)
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

// hideReqFor is the catch-up cursor a hello to p carries: "send me what you
// changed after this stamp". nil for anyone who is not a proven own device.
func (s *Service) hideReqFor(p peer.ID) *int64 {
	if !s.ownDevice(p) || s.revocationFor(p) != nil {
		return nil
	}
	s.hideMu.Lock()
	c := s.hideCursor[p]
	s.hideMu.Unlock()
	return &c
}

// hidePageFor answers a peer's cursor with the next bounded page, only for a
// proven own device.
func (s *Service) hidePageFor(from peer.ID, since *int64, out *helloFrame) {
	if since == nil || !s.ownDevice(from) || s.revocationFor(from) != nil {
		return
	}
	recs, last, more, err := s.store.HideRecordsAfter(*since, hidePageSize)
	if err != nil || len(recs) == 0 {
		return
	}
	out.Hides, out.HideNext, out.HideMore = recs, last, more
}

// adoptOfferedHides applies registers that rode in on a hello, from a proven
// device of this account only. A page answering our cursor also advances it,
// and a "more" flag makes us ask for the next page.
func (s *Service) adoptOfferedHides(from peer.ID, f helloFrame) {
	if !s.ownDevice(from) || s.revocationFor(from) != nil {
		return
	}
	if len(f.Hides) > 0 {
		s.applyRemoteHides(f.Hides)
	}
	if f.HideNext > 0 {
		s.hideMu.Lock()
		if s.hideCursor == nil {
			s.hideCursor = map[peer.ID]int64{}
		}
		advanced := f.HideNext > s.hideCursor[from]
		if advanced {
			s.hideCursor[from] = f.HideNext
		}
		s.hideMu.Unlock()
		if f.HideMore && advanced {
			go s.sayOwn(from, helloFrame{Credential: s.myCredential, HideReq: s.hideReqFor(from)})
		}
	}
}
