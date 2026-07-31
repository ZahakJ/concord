//go:build linux && !android

package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// nowPlaying reads the currently-playing track from any MPRIS-compatible media
// player on the session bus (Spotify, the browser, most native players) and
// returns a status line like "🎵 Artist — Title" plus the structured Activity
// (art, duration, position snapshot), or "", nil when nothing is playing.
// Best-effort: any D-Bus error yields no status. Uses the shared session bus
// connection, so it must not be closed here.
func nowPlaying() (string, *Activity) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return "", nil
	}
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return "", nil
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "org.mpris.MediaPlayer2.") {
			continue
		}
		obj := conn.Object(name, "/org/mpris/MediaPlayer2")

		st, err := obj.GetProperty("org.mpris.MediaPlayer2.Player.PlaybackStatus")
		if err != nil {
			continue
		}
		if s, _ := st.Value().(string); s != "Playing" {
			continue
		}

		metaV, err := obj.GetProperty("org.mpris.MediaPlayer2.Player.Metadata")
		if err != nil {
			continue
		}
		meta, ok := metaV.Value().(map[string]dbus.Variant)
		if !ok {
			continue
		}
		title, _ := meta["xesam:title"].Value().(string)
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		var artist string
		if a, ok := meta["xesam:artist"].Value().([]string); ok && len(a) > 0 {
			artist = strings.TrimSpace(a[0])
		}

		act := &Activity{Artist: artist, Title: title, AtMs: time.Now().UnixMilli()}
		if art, ok := meta["mpris:artUrl"].Value().(string); ok {
			// Only broadcast web-fetchable art (players also emit file:// paths,
			// useless — and leaky — off-device).
			if strings.HasPrefix(art, "https://") || strings.HasPrefix(art, "http://") {
				if len(art) <= maxArtURLBytes {
					act.ArtURL = art
				}
			}
		}
		// mpris:length and Position are in microseconds; either may be missing
		// or typed int64/uint64 depending on the player.
		if l := asInt64(meta["mpris:length"].Value()); l > 0 {
			act.DurationMs = l / 1000
		}
		if pos, err := obj.GetProperty("org.mpris.MediaPlayer2.Player.Position"); err == nil {
			if p := asInt64(pos.Value()); p > 0 {
				act.PositionMs = p / 1000
			}
		}

		if artist != "" {
			return fmt.Sprintf("🎵 %s — %s", artist, title), act
		}
		return "🎵 " + title, act
	}
	return "", nil
}

// asInt64 normalizes the numeric types D-Bus variants show up as.
func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case uint64:
		return int64(n)
	case int32:
		return int64(n)
	case uint32:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

// richPresenceSupported: MPRIS exists here, so the poll loop has something to
// ask. See mpris_other.go for why the unsupported platforms skip it entirely.
const richPresenceSupported = true
