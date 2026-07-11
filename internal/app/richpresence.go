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

// maxArtURLBytes bounds the album-art URL a peer may broadcast.
const maxArtURLBytes = 512

// Activity is the structured now-playing payload that rides along with the
// status string, so clients can render a real activity card (art, progress).
// PositionMs is a snapshot taken at AtMs (unix ms); clients extrapolate
// progress locally instead of us rebroadcasting every tick.
type Activity struct {
	Artist     string `json:"artist,omitempty"`
	Title      string `json:"title"`
	ArtURL     string `json:"artUrl,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	PositionMs int64  `json:"positionMs,omitempty"`
	AtMs       int64  `json:"atMs,omitempty"`
}

// activityEqual compares the descriptive fields (not the position snapshot —
// that advances every poll without meaning the activity changed).
func activityEqual(a, b *Activity) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.Artist == b.Artist && a.Title == b.Title && a.ArtURL == b.ArtURL && a.DurationMs == b.DurationMs
}

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

// positionDrifted reports whether the freshly-sampled position disagrees with
// what clients would have extrapolated from the last announce (a seek, or a
// pause/resume) by more than a few seconds — worth a re-announce.
func positionDrifted(prev, cur *Activity) bool {
	if prev == nil || cur == nil {
		return false
	}
	expected := prev.PositionMs + (cur.AtMs - prev.AtMs)
	d := cur.PositionMs - expected
	if d < 0 {
		d = -d
	}
	return d > 5000
}

func (s *Service) stopRichPresence() {
	s.activityMu.Lock()
	cancel := s.richPresenceStop
	s.richPresenceStop = nil
	s.activityMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.updateActivity("", nil) // drop the overlay; manual status returns
}

// updateActivity sets the runtime activity overlay and re-announces the profile
// only when it actually changed (track change, stop, or a seek big enough that
// clients' extrapolated progress is wrong) — not on every position tick.
func (s *Service) updateActivity(a string, act *Activity) {
	if len(a) > maxActivityBytes {
		a = a[:maxActivityBytes]
	}
	s.activityMu.Lock()
	changed := s.activity != a || !activityEqual(s.activityInfo, act) || positionDrifted(s.activityInfo, act)
	s.activity = a
	if changed { // keep the last snapshot stable so drift compares against what peers saw
		s.activityInfo = act
	}
	s.activityMu.Unlock()
	if changed {
		s.announceProfileAll()
		// The local UI learns profile changes the same way remote peers do —
		// except its own. Poke it directly so your own card updates live.
		s.emitGuildUpdate()
	}
}
