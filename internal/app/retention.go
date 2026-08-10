package app

import (
	"time"
)

// retentionTick paces the pruning sweep. Coarse deliberately: the shortest
// policy the governance layer will fold to is an hour, so nothing is gained by
// looking more often than that, and a sweep walks every channel of every guild.
const retentionTick = time.Hour

// runRetentionLoop prunes messages that have outlived their guild's policy.
//
// Distinct from the per-message disappearing timer (ExpireMessage): that one is
// chosen by an author for one message, travels inside the MLS-authenticated
// content, and erases the body while leaving a tombstone, so every device
// vanishes it at the same wall-clock instant with no coordination at all. This
// is a guild-wide housekeeping rule that applies to messages already sent,
// carried by the governance log, and it removes the rows outright to reclaim
// the space. Neither replaces the other.
//
// Local enforcement is all there is. Nothing here reaches another device: each
// peer prunes its own copy when it comes around to it, so two members with
// different uptimes forget at different moments, and a member running a
// modified client need not forget at all. That is a property of having no
// server, not a bug to fix, and the UI says so beside the switch.
func (s *Service) runRetentionLoop() {
	s.sweepRetention(time.Now())
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.bgWakeCh():
			// Foregrounded: catch up on whatever aged out while asleep.
		case <-time.After(s.bgPace(retentionTick)):
		}
		s.sweepRetention(time.Now())
	}
}

// sweepRetention runs one pass. Channels are grouped by the cutoff that applies
// to them so a guild with no per-channel overrides — the ordinary case — costs
// exactly one statement rather than one per channel.
func (s *Service) sweepRetention(now time.Time) {
	type work struct {
		cutoff   int64
		channels []string
	}

	s.mu.RLock()
	byCutoff := map[int64]*work{}
	for _, g := range s.guilds {
		// DMs and disposable meeting rooms have no governance log to carry a
		// policy, so there is nothing to apply to them.
		if g.Kind != "" {
			continue
		}
		st, ok := s.govState[g.ID]
		if !ok {
			continue
		}
		for _, c := range g.Channels {
			secs := s.retentionFor(st, c.ID)
			if secs <= 0 {
				continue
			}
			cutoff := now.Add(-time.Duration(secs) * time.Second).UnixNano()
			w := byCutoff[cutoff]
			if w == nil {
				w = &work{cutoff: cutoff}
				byCutoff[cutoff] = w
			}
			w.channels = append(w.channels, c.ID)
		}
	}
	s.mu.RUnlock()

	pruned := 0
	for _, w := range byCutoff {
		n, err := s.store.PruneMessagesBefore(w.channels, w.cutoff)
		if err != nil {
			continue // transient; the next sweep tries again
		}
		pruned += n
	}
	// Something vanished underneath whatever is on screen, so tell the UI to
	// re-read rather than leave it showing messages that are no longer there.
	if pruned > 0 {
		s.emitGuildUpdate()
	}
}
