package app

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
