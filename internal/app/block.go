package app

// block.go — blocking an account by fingerprint. A blocked account's incoming
// DM and guild invites are dropped on arrival (see dm.go handleDMInvite), so a
// blocked person can't add you to a DM or a guild; and every message they have
// already sent into a guild you share is hidden from every view (see
// SenderBlocked, applied at the bridge). The list is local to this device and
// mirrored in memory for a cheap IsBlocked check on the hot path.

// loadBlocked mirrors the persisted block list into memory. Called at startup.
func (s *Service) loadBlocked() {
	fprs, err := s.store.BlockedFingerprints()
	if err != nil {
		return
	}
	s.mu.Lock()
	for _, f := range fprs {
		s.blocked[f] = true
	}
	s.mu.Unlock()
}

// IsBlocked reports whether an account fingerprint is on the block list.
func (s *Service) IsBlocked(fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.blocked[fingerprint]
}

// SenderBlocked reports whether a message carrying this sender credential
// should be kept out of every view.
//
// It takes the raw credential rather than a fingerprint because the two are not
// interchangeable: a message from a linked device carries a device certificate,
// not the bare account key, and hashing that blob directly yields a fingerprint
// that matches nothing on the block list. accountFingerprintOf unwraps the
// certificate to the account that signed it, which is the identity the user
// actually blocked — so blocking someone hides their phone as well as their
// laptop, which is the only reading of "block" a user would accept.
//
// Note what this does NOT do: nothing is deleted. The rows stay in the store,
// the sync digests still cover them, and peers still converge on the same
// history. Blocking is a decision about what this device shows its owner, not a
// claim about what happened in the guild — so unblocking brings the messages
// straight back, and a blocked member's message can still be quoted by someone
// you have not blocked.
func (s *Service) SenderBlocked(sender []byte) bool {
	if len(sender) == 0 {
		return false
	}
	return s.IsBlocked(accountFingerprintOf(sender))
}

// BlockUser adds an account to the block list.
func (s *Service) BlockUser(fingerprint string) error {
	if fingerprint == "" {
		return nil
	}
	if err := s.store.BlockFingerprint(fingerprint); err != nil {
		return err
	}
	s.mu.Lock()
	s.blocked[fingerprint] = true
	s.mu.Unlock()
	return nil
}

// UnblockUser removes an account from the block list.
func (s *Service) UnblockUser(fingerprint string) error {
	if err := s.store.UnblockFingerprint(fingerprint); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.blocked, fingerprint)
	s.mu.Unlock()
	return nil
}

// BlockedUsers lists blocked account fingerprints.
func (s *Service) BlockedUsers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.blocked))
	for f := range s.blocked {
		out = append(out, f)
	}
	return out
}
