package app

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// "Delete for me": hide a message (anyone's, in any kind of conversation) on
// this account's devices only. Nothing is sent to the channel or anyone in it.
// The hide is held in the encrypted store, filtered out of every user-facing
// read there (store/hidden.go), and told to the account's OTHER devices the
// way read markers are: a publish on the Notes self-group's meta topic, a group
// whose only members are our own devices, plus a hello stream to each connected
// device for the reasons readstate.go gives (the gossip has no catch-up).
// Receivers accept it only from a leaf of our own account, so no one else can
// hide anything for us. Ops are idempotent; the latest op for an id wins.
//
// Attachment bytes are deliberately NOT dropped. A blob is content-addressed
// and may be referenced by other, visible messages (a forward re-sends the same
// token), finding out means decrypting the whole history, and this device may
// be the last replica other members fetch it from (handleAttachRequest serves
// our cache). Dropping it would be a guess that can destroy other people's
// copy; the blob stays under the normal LRU instead.

// maxHideOps bounds one frame's ids, and the length of any id, so a hostile or
// buggy own-device frame cannot make us store unbounded rows.
const (
	maxHideOps   = 500
	maxHideIDLen = 128
)

// hideOp is one queued change: hide (true) or unhide (false) of a message.
type hideOp struct {
	channelID string
	hide      bool
}

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
	if !validHideID(channelID) || !validHideID(messageID) {
		return fmt.Errorf("app: hide: bad channel or message id")
	}
	added, err := s.store.HideMessage(channelID, messageID)
	if err != nil {
		return err
	}
	if added {
		s.emitHidden(channelID, messageID, true)
	}
	s.queueHide(channelID, messageID, true)
	return nil
}

// UnhideMessage undoes HideMessage (the toast's Undo).
func (s *Service) UnhideMessage(channelID, messageID string) error {
	if !validHideID(channelID) || !validHideID(messageID) {
		return fmt.Errorf("app: unhide: bad channel or message id")
	}
	removed, err := s.store.UnhideMessage(messageID)
	if err != nil {
		return err
	}
	if removed {
		s.emitHidden(channelID, messageID, false)
	}
	s.queueHide(channelID, messageID, false)
	return nil
}

// HiddenMessages lists the ids hidden in one channel.
func (s *Service) HiddenMessages(channelID string) ([]string, error) {
	return s.store.HiddenIDs(channelID)
}

// applyRemoteHide folds in ops authored by another of this account's devices
// (authenticated by the caller). It never re-broadcasts.
func (s *Service) applyRemoteHide(hide, unhide map[string][]string) {
	n := 0
	for ch, ids := range unhide {
		for _, id := range ids {
			if n++; n > maxHideOps || !validHideID(ch) || !validHideID(id) {
				continue
			}
			if ok, err := s.store.UnhideMessage(id); err == nil && ok {
				s.emitHidden(ch, id, false)
			}
		}
	}
	for ch, ids := range hide {
		for _, id := range ids {
			if n++; n > maxHideOps || !validHideID(ch) || !validHideID(id) {
				continue
			}
			if ok, err := s.store.HideMessage(ch, id); err == nil && ok {
				s.emitHidden(ch, id, true)
			}
		}
	}
}

// queueHide coalesces ops for a short beat so a burst travels as one publish.
func (s *Service) queueHide(channelID, messageID string, hide bool) {
	s.hideMu.Lock()
	if s.pendingHide == nil {
		s.pendingHide = map[string]hideOp{}
	}
	s.pendingHide[messageID] = hideOp{channelID: channelID, hide: hide}
	if s.hideTimer == nil {
		s.hideTimer = time.AfterFunc(800*time.Millisecond, s.flushHides)
	}
	s.hideMu.Unlock()
}

// hideSplit turns queued ops into the wire's two maps.
func hideSplit(ops map[string]hideOp) (hide, unhide map[string][]string) {
	hide, unhide = map[string][]string{}, map[string][]string{}
	for id, op := range ops {
		if op.hide {
			hide[op.channelID] = append(hide[op.channelID], id)
		} else {
			unhide[op.channelID] = append(unhide[op.channelID], id)
		}
	}
	return hide, unhide
}

// flushHides publishes the queued ops to the Notes self-group and pushes them
// to each connected own device. No Notes group means nobody to tell.
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
	hide, unhide := hideSplit(ops)
	payload, _ := json.Marshal(guildMeta{Type: "hide_marker", Hide: hide, Unhide: unhide})
	if ct, err := s.mls.Encrypt(s.ctx, notes.GroupID, payload); err == nil {
		_ = s.ps.Publish(s.ctx, domain.GuildMetaTopicID(notes.GroupID), ct)
	}
	for _, p := range s.host.Peers() {
		if s.ownDevice(p) && s.revocationFor(p) == nil {
			go s.sayHides(p, hide, unhide)
		}
	}
}

// sayHides is sayReadMarks for hide ops: a hello with only our credential and
// the ops, retried like it, adopted by the receiver only from a proven device.
func (s *Service) sayHides(p peer.ID, hide, unhide map[string][]string) {
	req, err := marshalHello(helloFrame{Credential: s.myCredential, Hide: hide, Unhide: unhide})
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

// adoptOfferedHides applies hide ops that rode in on a hello, from a proven
// device of this account only.
func (s *Service) adoptOfferedHides(from peer.ID, hide, unhide map[string][]string) {
	if len(hide)+len(unhide) == 0 || !s.ownDevice(from) || s.revocationFor(from) != nil {
		return
	}
	s.applyRemoteHide(hide, unhide)
}
