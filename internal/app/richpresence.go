package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// Rich presence surfaces what you're doing on this device — currently the
// now-playing media track (via the OS media session) — as your status line, the
// way a status line reads "Listening to <track>". It is:
//   - opt-in (off by default; a purely local, on-device read),
//   - an overlay: while something is playing it stands in for your manual
//     status on this device's own card, and your manual status is what returns
//     when playback stops,
//   - platform-specific under the hood (see nowPlaying in mpris_linux.go /
//     mpris_other.go); unsupported platforms simply report nothing.
// Nothing leaves the device except the track (title, artist, album-art URL,
// length and a position snapshot), on its own small frame — see
// announceActivityAll for why it is not folded into the profile announce.

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

// sanitizeActivity bounds a now-playing payload that came from a peer. Every
// lane a peer's activity can arrive by funnels through here — the full profile
// announce, the slim activity announce, and history sync — because a title is
// rendered and an art URL is FETCHED by the client, and neither is ours.
// Returns a copy, so bounding one arrival never edits a struct another caller
// still holds.
func sanitizeActivity(a *Activity) *Activity {
	if a == nil {
		return nil
	}
	if len(a.Title) > maxActivityBytes || len(a.Artist) > maxActivityBytes {
		return nil
	}
	out := *a
	if !validArtURL(out.ArtURL) {
		// Only web art travels: no file:///javascript: junk a client might render.
		out.ArtURL = ""
	}
	return &out
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
	if !richPresenceSupported {
		return // nothing to poll here; don't run an 8s timer to learn nothing
	}
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

// activitySeekCalm is the floor between two POSITION-ONLY re-announces. A new
// track is news and travels the moment it is noticed; a seek is the same track
// at a different place, and someone dragging a scrubber must not turn every
// poll into a broadcast to every guild. A calmed seek is not lost: the snapshot
// peers were last told about is left in place, so the next poll measures the
// same drift and says it once the floor has passed.
const activitySeekCalm = 30 * time.Second

// updateActivity records what this device is playing and tells the guilds,
// when there is something to tell: a track change, a stop, or a seek far
// enough that the progress clients extrapolate has gone wrong. Not on every
// position tick, and — for a seek — not more often than activitySeekCalm.
func (s *Service) updateActivity(a string, act *Activity) { s.updateActivityAt(a, act, time.Now()) }

// updateActivityAt is updateActivity with the clock passed in, so the calm can
// be driven directly by a test rather than waited out.
func (s *Service) updateActivityAt(a string, act *Activity, now time.Time) {
	a = clampBytes(a, maxActivityBytes)
	s.activityMu.Lock()
	moved := s.activity != a || !activityEqual(s.activityInfo, act)
	say := moved || (positionDrifted(s.activityInfo, act) && now.Sub(s.lastActivitySay) >= activitySeekCalm)
	if say {
		// Only what we actually broadcast is recorded, so drift always compares
		// against what peers were told — a calmed seek stays visible to the
		// next poll instead of being silently adopted and forgotten.
		s.activity = a
		s.activityInfo = act
		s.lastActivitySay = now
	}
	s.activityMu.Unlock()
	if say {
		s.announceActivityAll()
		// The local UI learns profile changes the same way remote peers do —
		// except its own. Poke it directly so your own card updates live.
		s.emitGuildUpdate()
	}
}

// announceActivityAll broadcasts the now-playing line to every guild.
//
// This is deliberately NOT announceProfileAll. A profile announce carries the
// whole profile — an avatar up to 64 KiB, a profile banner up to 256 KiB, the
// bio, the game collection — MLS-encrypted and flooded to every guild's mesh.
// Hanging a change of song on it meant a listener re-published a third of a
// megabyte of pictures to every guild they are in, several times a song, to
// report a string. The activity frame is the string.
func (s *Service) announceActivityAll() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	if len(ids) == 0 {
		return
	}
	frame := s.activityFrame() // one read of the profile, not one per guild
	for _, id := range ids {
		s.publishActivity(id, frame)
	}
}

// activityFrame is the slim announcement: who is speaking and what they are
// playing, and nothing else. It carries no name, no images and no status line,
// which is what makes it safe for a receiver to apply verbatim — there is no
// field in it that could arrive empty and blank something already cached.
func (s *Service) activityFrame() guildMeta {
	s.activityMu.Lock()
	var act *Activity
	if s.activity != "" {
		act = s.activityInfo
	}
	s.activityMu.Unlock()
	return guildMeta{Type: "activity", Fingerprint: s.id.Fingerprint(), Activity: act}
}

// publishActivity sends one guild its copy, reporting whether it went out.
func (s *Service) publishActivity(guildID string, frame guildMeta) bool {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return false
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return false
	}
	ct, err := s.mls.Encrypt(s.ctx, groupID, payload)
	if err != nil {
		return false
	}
	return s.ps.Publish(s.ctx, domain.GuildMetaTopicID(groupID), ct) == nil
}

// applyActivityMeta folds an inbound activity frame into the sender's cached
// profile. Same actor binding as a profile announce: a frame speaks only for
// the member MLS says wrote it, or anyone could put a song on anyone's card.
func (s *Service) applyActivityMeta(actor string, m guildMeta) {
	if m.Fingerprint != "" && m.Fingerprint != actor {
		return
	}
	s.learnActivity(actor, m.Activity)
}

// learnActivity replaces the now-playing overlay on a member we already know,
// and touches nothing else about them.
//
// That restriction is the whole compatibility story on the receive side. The
// profile applier protects a cached name and mailbox key from being blanked by
// a partial update but protects nothing else, so a slim announce routed through
// it would have wiped the avatar, banner, bio and colours of everyone whose
// music we could hear. This path cannot: the only field it can write is the
// one field the frame carries. It also writes nothing to disk — an activity is
// ephemeral by definition, and a song that outlived the process would be a lie.
func (s *Service) learnActivity(fingerprint string, act *Activity) {
	if fingerprint == "" || fingerprint == s.id.Fingerprint() {
		return
	}
	act = sanitizeActivity(act)
	s.mu.Lock()
	p, known := s.profiles[fingerprint]
	if !known {
		// Nobody we have met, so there is no card to put this on. Their profile
		// announce is what introduces them, and it carries the activity too.
		s.mu.Unlock()
		return
	}
	if activityEqual(p.Activity, act) && activityPosEqual(p.Activity, act) {
		s.mu.Unlock()
		return // nothing new; don't wake the UI
	}
	p.Activity = act
	s.profiles[fingerprint] = p
	s.mu.Unlock()
	s.emitGuildUpdate()
}
