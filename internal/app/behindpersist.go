package app

import "strconv"

// persistBehind writes the seen-behind latch for a guild as it stands in
// memory now.
//
// noteBehind and clearBehind each change the latch under mu and then write it
// to the store outside the lock. Each used to write its own value, so when a
// late noteBehind and a clearBehind crossed, the memory could end cleared and
// the store latched: the device carried on as level, then at its next start
// loadBehindSeen read the stale latch back and could flag the guild and bar
// its commits again. Reading the value under the write's own lock makes the
// last write carry the last change, so the store always ends where the
// memory does.
func (s *Service) persistBehind(guildID string) {
	if testHookBeforeBehindPersist != nil {
		testHookBeforeBehindPersist()
	}
	s.behindPersistMu.Lock()
	defer s.behindPersistMu.Unlock()
	s.mu.RLock()
	seen := s.behindSeen[guildID]
	s.mu.RUnlock()
	v := ""
	if seen > 0 {
		v = strconv.FormatUint(seen, 10)
	}
	_ = s.store.SetSetting(behindSeenPrefix+guildID, v)
}

// testHookBeforeBehindPersist runs after the latch changed in memory and
// before it is written, so a test can cross a note and a clear there. Nothing
// in the product sets it.
var testHookBeforeBehindPersist func()
