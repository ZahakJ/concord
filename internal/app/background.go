package app

import "time"

// Background mode (phones). Measured on Android with the app backgrounded and
// the screen off, the core kept up the exact same cadence as foregrounded:
// ~4 packets/second combined (DHT discovery every 15s, guild reconcile every
// 20s, mailbox sweep every 60s, own-device dials every 30s, plus gossip
// heartbeats), ~45 KB/min through the radio. The CPU cost is nothing — the
// battery cost is the radio: a packet every few hundred milliseconds means the
// Wi-Fi chip never enters power-save and a cellular modem never leaves the
// high-power RRC state, which reads as "Concord eats the battery" even though
// the app is doing almost no work.
//
// So the shells tell the core when the app leaves the screen, and every
// periodic loop stretches to one slow shared beat. What stays untouched, on
// purpose, is everything that makes a backgrounded phone reachable: the open
// connections (rendezvous, relay reservation, gossip mesh) are kept, gossipsub
// keeps delivering messages instantly, and the foreground-return path (Nudge +
// the loops waking on bgWake) catches up the moment the user comes back.
const backgroundBeat = 3 * time.Minute

// SetBackground switches the periodic loops between their foreground cadence
// and the slow background beat. Idempotent; safe from any goroutine. On the
// background→foreground edge every throttled loop is woken immediately, so
// returning to the app never waits out a beat that was scheduled while asleep.
func (s *Service) SetBackground(bg bool) {
	s.bgMu.Lock()
	changed := s.bg != bg
	s.bg = bg
	var wake chan struct{}
	if changed && !bg && s.bgWake != nil {
		wake = s.bgWake
		s.bgWake = nil
	}
	s.bgMu.Unlock()
	if !changed {
		return
	}
	s.host.SetBackground(bg)
	if wake != nil {
		close(wake) // foreground again: run the throttled loops now
	}
}

func (s *Service) backgrounded() bool {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	return s.bg
}

// bgPace stretches a foreground interval to the background beat while the app
// is backgrounded. Intervals already slower than the beat are left alone.
func (s *Service) bgPace(fg time.Duration) time.Duration {
	if s.backgrounded() && fg < backgroundBeat {
		return backgroundBeat
	}
	return fg
}

// bgWakeCh returns a channel closed on the next background→foreground switch,
// so paced loops can park on it alongside their timer.
func (s *Service) bgWakeCh() <-chan struct{} {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	if s.bgWake == nil {
		s.bgWake = make(chan struct{})
	}
	return s.bgWake
}
