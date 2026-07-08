package app

import (
	"context"
	"time"
)

// Rich presence surfaces what you're doing on this device — currently the
// now-playing media track (via the OS media session) — as your status line, the
// way Discord shows "Listening to Spotify". It is:
//   - opt-in (off by default; a purely local, on-device read),
//   - an overlay: while something is playing it stands in for your manual
//     status, and your manual status returns when playback stops,
//   - platform-specific under the hood (see nowPlaying in mpris_linux.go /
//     mpris_other.go); unsupported platforms simply report nothing.
// Nothing leaves the device except the resulting status string, which already
// travels to your guilds/DMs like any status.

const richPresenceInterval = 8 * time.Second

// maxActivityBytes bounds the auto status so a pathological track title can't
// bloat the profile broadcast.
const maxActivityBytes = 128

// SetRichPresence enables or disables rich presence and persists the choice.
func (s *Service) SetRichPresence(enabled bool) error {
	val := ""
	if enabled {
		val = "1"
	}
	if err := s.store.SetSetting("rich_presence", val); err != nil {
		return err
	}
	s.activityMu.Lock()
	running := s.richPresenceStop != nil
	s.activityMu.Unlock()
	if enabled && !running {
		s.startRichPresence()
	} else if !enabled && running {
		s.stopRichPresence()
	}
	return nil
}

// RichPresenceEnabled reports whether the user has turned rich presence on.
func (s *Service) RichPresenceEnabled() bool {
	v, _ := s.store.GetSetting("rich_presence")
	return v == "1"
}

func (s *Service) startRichPresence() {
	ctx, cancel := context.WithCancel(s.ctx)
	s.activityMu.Lock()
	if s.richPresenceStop != nil {
		s.activityMu.Unlock()
		cancel()
		return // already running
	}
	s.richPresenceStop = cancel
	s.activityMu.Unlock()

	go func() {
		t := time.NewTicker(richPresenceInterval)
		defer t.Stop()
		s.updateActivity(nowPlaying()) // reflect immediately
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.updateActivity(nowPlaying())
			}
		}
	}()
}

func (s *Service) stopRichPresence() {
	s.activityMu.Lock()
	cancel := s.richPresenceStop
	s.richPresenceStop = nil
	s.activityMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.updateActivity("") // drop the overlay; manual status returns
}

// updateActivity sets the runtime activity overlay and re-announces the profile
// only when it actually changed (so we're not broadcasting every tick).
func (s *Service) updateActivity(a string) {
	if len(a) > maxActivityBytes {
		a = a[:maxActivityBytes]
	}
	s.activityMu.Lock()
	changed := s.activity != a
	s.activity = a
	s.activityMu.Unlock()
	if changed {
		s.announceProfileAll()
	}
}
