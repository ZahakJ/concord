package app

// block.go — blocking an account by fingerprint. A blocked account's incoming
// DM and guild invites are dropped on arrival (see dm.go handleDMInvite), so a
// blocked person can't add you to a DM or a server. The list is local to this
// device and mirrored in memory for a cheap IsBlocked check on the hot path.

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
