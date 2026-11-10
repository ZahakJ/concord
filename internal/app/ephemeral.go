package app

import "time"

// ephemeralTick is how often the core looks for disappearing messages that are
// due. The shortest timer offered is five minutes, so a quarter-minute late is
// within what anybody would call "on time".
const ephemeralTick = 15 * time.Second

// runEphemeralLoop erases disappearing messages when their timer runs out, on
// this device, whether or not anything is showing them.
//
// The expiry used to be enforced by the view alone: the frontend's ticker erased
// a message only if it was loaded in the open channel. A device that never
// opened that channel — the sender's own, a phone in a drawer, a linked desktop
// sitting in #general — kept the plaintext forever and returned it to search.
// A promise of "erases itself on every device" is the core's to keep.
func (s *Service) runEphemeralLoop() {
	s.sweepEphemeral(time.Now())
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.bgWakeCh():
		case <-time.After(s.bgPace(ephemeralTick)):
		}
		s.sweepEphemeral(time.Now())
	}
}

// sweepEphemeral erases every live row whose timer is at or before now: the
// body is wiped (so search, previews and the saved list have nothing to show),
// the row is left as a "disappeared" tombstone, and any bookmark of it goes.
func (s *Service) sweepEphemeral(now time.Time) int {
	due, err := s.store.DueEphemeral(now.Unix())
	if err != nil {
		return 0
	}
	n := 0
	for _, d := range due {
		if s.ExpireMessage(d.ChannelID, d.ID) == nil {
			_ = s.store.UnbookmarkMessage(d.ID)
			n++
		}
	}
	return n
}
