package app

// block.go — blocking an account by fingerprint. A blocked account's incoming
// DM and guild invites are dropped on arrival (see dm.go handleDMInvite), so a
// blocked person can't add you to a DM or a guild; and every message they have
// already sent into a guild you share is hidden from every view (see
// SenderBlocked, applied at the bridge). The list is local to this device and
// mirrored in memory for a cheap IsBlocked check on the hot path.
//
// Blocking is not only about messages, because a message is not the only thing
// somebody can put in front of you. The same list is asked at every other point
// where their presence reaches a screen: their reactions on other people's
// messages and their forum threads (stripped where those views are built),
// their typing line and their moments (dropped at the bridge), their arrival in
// a voice room and the WebRTC offer that would follow it (dropped at the
// subscription, in voice.go and service.go), and the unread count, which is
// done in SQL and so has to subtract them by hand rather than by decrypting
// (see UnreadCounts). Each of those was a way to be heard by somebody who had
// asked not to hear you.
//
// The list itself does not travel. There is no sync record for it, no digest
// entry and nothing in the passphrase archive: a linked phone has its own list,
// and the settings copy says so rather than letting people find out the hard
// way. That is a deliberate consequence of blocking being a viewing preference
// — the alternative is publishing "I have blocked this person" onto a wire the
// blocked person can also read.

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

// hasBlocks reports whether anything is on the block list at all. It exists so
// the paths that would otherwise do extra work per channel — the unread
// breakdown — can stay exactly as cheap as they were on the overwhelmingly
// common device where nobody has ever been blocked.
func (s *Service) hasBlocks() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.blocked) > 0
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

// BlockUser adds an account to the block list and closes any 1:1 conversation
// with them.
//
// Closing the DM belongs here rather than in a client. Hiding the messages is
// not enough on its own: the conversation keeps its row in the DM list, sorted
// by activity, so a blocked person who can no longer say a word to you can
// still push their name back to the top of it whenever they like — an empty
// conversation you cannot make go away. It is a hide, not a delete (LeaveGuild's
// 1:1 path), so unblocking and one message from them brings it back with its
// history intact; and it must happen on every shell, not just the one whose
// front end remembered to ask.
func (s *Service) BlockUser(fingerprint string) error {
	if fingerprint == "" {
		return nil
	}
	if err := s.store.BlockFingerprint(fingerprint); err != nil {
		return err
	}
	s.mu.Lock()
	s.blocked[fingerprint] = true
	var dms []string
	for guildID, peer := range s.dmPeers {
		if peer == fingerprint && !s.hiddenDMs[guildID] {
			dms = append(dms, guildID)
		}
	}
	s.mu.Unlock()
	for _, id := range dms {
		s.hideDM(id)
	}
	return nil
}

// dmReopenBlocked reports whether a guild is a 1:1 conversation with someone on
// the block list.
//
// A closed DM normally reopens the moment anything new lands in it, which is
// right — closing a conversation is not the same as refusing one. But that rule
// handed the blocked account the close button: block them, and their next
// message put the conversation straight back in the list, empty, ready to be
// closed again and reopened again. So arrival paths ask this first, while the
// paths the USER drives (accepting a request, opening Notes, starting a DM
// yourself) go on reopening unconditionally — those are decisions you made.
func (s *Service) dmReopenBlocked(guildID string) bool {
	if guildID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	peer := s.dmPeers[guildID]
	return peer != "" && s.blocked[peer]
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
