//go:build linux && !android

package app

import (
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// nowPlaying reads the currently-playing track from any MPRIS-compatible media
// player on the session bus (Spotify, the browser, most native players) and
// returns a status line like "🎵 Artist — Title", or "" when nothing is
// playing. Best-effort: any D-Bus error yields "" (no status). Uses the shared
// session bus connection, so it must not be closed here.
func nowPlaying() string {
	conn, err := dbus.SessionBus()
	if err != nil {
		return ""
	}
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return ""
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
		if artist != "" {
			return fmt.Sprintf("🎵 %s — %s", artist, title)
		}
		return "🎵 " + title
	}
	return ""
}
