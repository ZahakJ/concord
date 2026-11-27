package app

import (
	"log"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// applyAction applies an action message (a reaction, an edit, a pin or a
// delete) to the message it names, once per action id (store.applied_actions).
// The error is the disk's: an action it refuses (no permission, not the
// author's) is no error and stays refused.
func (s *Service) applyAction(m domain.Message) error {
	switch m.Kind {
	case "delete":
		s.applyDelete(m.ReplyTo, m.Sender, m.ChannelID, len(m.Sig) > 0)
	case "reaction":
		return s.applyReaction(m.ID, m.ReplyTo, m.Content, m.Sender)
	case "edit":
		return s.applyEdit(m.ID, m.ReplyTo, m.Content, m.Sender, m.RowSig, m.Dir)
	case "pin":
		return s.applyPin(m.ID, m.ReplyTo, m.Sender, m.ChannelID)
	}
	return nil
}

// applyOwnLater keeps applying an action this device published but could not
// apply to its own copy, as saveOwnLater does for a message's row. Until it
// lands the action is held in unsaved, so a retry of the same send id finds it
// gone out and sends nothing.
func (s *Service) applyOwnLater(msg domain.Message, err error) {
	log.Printf("concord/app: %s %s was sent but not applied here yet: %v; retrying in the background", msg.Kind, msg.ID, err)
	s.unsavedMu.Lock()
	if s.unsaved == nil {
		s.unsaved = map[string]domain.Message{}
	}
	s.unsaved[msg.ID] = msg
	s.unsavedMu.Unlock()
	go func() {
		wait := applyOwnFirstWait
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(wait):
			}
			if s.applyAction(msg) == nil {
				s.unsavedMu.Lock()
				delete(s.unsaved, msg.ID)
				s.unsavedMu.Unlock()
				return
			}
			wait = min(wait*2, time.Minute)
		}
	}()
}

// applyOwnFirstWait is the first pause before an own action is applied again.
var applyOwnFirstWait = 50 * time.Millisecond

// sendActionOnce sends an action under a client's send id, as SendMessageOnce
// does a message: every attempt at one user action is one action id, so a
// retry is never a second toggle, here or on any peer.
func (s *Service) sendActionOnce(channelID, content, kind, targetID, dir, sendID string) error {
	_, err := s.sendOnceAs(channelID, sendID, func(id string) (domain.Message, error) {
		return s.sendCore(channelID, content, kind, targetID, "", dir, nil, false, id)
	})
	return err
}

// InjectStoreFault fails the named store steps (store.InjectFault). Tests
// only, for packages above this one: the phone's path through the bridge is
// tested against a failing disk with it.
func (s *Service) InjectStoreFault(f func(op string) error) { s.store.InjectFault(f) }
