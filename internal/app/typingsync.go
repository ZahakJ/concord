package app

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"strconv"
	"time"
)

// The typing switch is account state, not device state: it follows the
// account's own devices over the Notes self-group meta topic (a group whose
// only members are our devices), the same wire read markers use. Newest stamp
// wins, and a stamp is never trusted further ahead than readSkew, so a device
// with a fast clock cannot pin the setting against a later choice elsewhere.

const (
	// typingPrefAtKey holds the stamp (UnixMilli) of the last choice.
	typingPrefAtKey = "typing_indicators_at"
	// typingOptInKey marks that the opt-in default has been applied. See
	// migrateTypingOptIn.
	typingOptInKey = "typing_optin_v1"
)

// migrateTypingOptIn makes the indicator opt-in. The stored switch cannot tell
// "never touched" from "turned on explicitly" (absent meant on), so every
// existing install is turned off exactly once. Whoever wants it back turns it
// on, which stamps a fresh choice. A new install also lands here, off.
func (s *Service) migrateTypingOptIn() {
	if v, _ := s.store.GetSetting(typingOptInKey); v == "1" {
		return
	}
	_ = s.store.SetSetting(typingPrefKey, "0")
	_ = s.store.SetSetting(typingPrefAtKey, "0")
	_ = s.store.SetSetting(typingOptInKey, "1")
}

func (s *Service) typingStamp() int64 {
	v, _ := s.store.GetSetting(typingPrefAtKey)
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

// storeTypingPref persists value and stamp together. Callers hold typingPrefMu.
func (s *Service) storeTypingPref(on bool, at int64) error {
	val := "0"
	if on {
		val = "1"
	}
	if err := s.store.SetSetting(typingPrefKey, val); err != nil {
		return err
	}
	if err := s.store.SetSetting(typingPrefAtKey, strconv.FormatInt(at, 10)); err != nil {
		return err
	}
	s.mu.Lock()
	s.typingOn = on
	s.mu.Unlock()
	return nil
}

// applyRemoteTypingPref folds in a choice made on another device of this
// account (authenticated in receiveGuildMeta). It never re-broadcasts.
func (s *Service) applyRemoteTypingPref(on bool, at int64) {
	if at <= 0 {
		return
	}
	at = capReadMark(at, time.Now())
	s.typingPrefMu.Lock()
	defer s.typingPrefMu.Unlock()
	if at <= s.typingStamp() {
		return
	}
	_ = s.storeTypingPref(on, at)
}

// broadcastTypingPref tells the account's other devices. A single-device
// account has no other members of Notes, so this costs nothing.
func (s *Service) broadcastTypingPref(on bool, at int64) {
	notes, ok := s.findNotesDM()
	if !ok {
		return
	}
	s.publishMeta(notes.GroupID, guildMeta{Type: "typing_pref", TypingOn: &on, UpdatedAt: at})
}

// typingOfferFor returns the typing choice to put in a hello for p, setting
// *at to its stamp, or nil for anyone who is not a current device of THIS
// account or when the choice has never been stamped (nothing to say).
func (s *Service) typingOfferFor(p peer.ID, at *int64) *bool {
	if !s.ownDevice(p) || s.revocationFor(p) != nil {
		return nil
	}
	s.typingPrefMu.Lock()
	defer s.typingPrefMu.Unlock()
	st := s.typingStamp()
	if st <= 0 {
		return nil
	}
	on := s.TypingEnabled()
	*at = st
	return &on
}

// adoptOfferedTypingPref folds in a choice that rode in on a hello, from a
// proven device of this account only, by the same newest-wins, capped rule as
// the live message. It never re-broadcasts.
func (s *Service) adoptOfferedTypingPref(from peer.ID, on *bool, at int64) {
	if on == nil || !s.ownDevice(from) || s.revocationFor(from) != nil {
		return
	}
	s.applyRemoteTypingPref(*on, at)
}
