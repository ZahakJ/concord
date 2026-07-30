package app

import "testing"

// The guild banner reaches a CSS url() in the client, and for a long time both
// the setter and the receive path checked only that it STARTED with
// "data:image/". A value like
//
//	data:image/png;base64,AAAA);background-image:url(http://tracker.example/x.png
//
// satisfies that, escapes the declaration it is interpolated into, and makes
// every member who opens the guild fetch a remote asset — an IP disclosure to
// whoever set the banner, in an app that ships link previews OFF by default for
// exactly that reason. Both paths now demand the whole string be a base64
// raster data URI, or a preset id from a narrow charset.
func TestGuildBannerRejectsCSSBreakout(t *testing.T) {
	const cap = 512 << 10
	breakouts := []string{
		`data:image/png;base64,AAAA);background-image:url(http://tracker.example/x.png`,
		`data:image/png;base64,AAAA"),url("http://tracker.example/x.png`,
		`data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=`, // svg is scriptable
		`data:image/png;base64,AA AA`,                // whitespace splits the value
		`data:image/png;utf8,<svg/>`,                 // not base64 at all
		`data:image/png;base64,`,                     // empty payload
	}
	for _, v := range breakouts {
		if validImageDataURI(v, cap) {
			t.Errorf("accepted a banner that can escape a CSS url(): %.60q", v)
		}
	}
	for _, v := range []string{
		`data:image/png;base64,iVBORw0KGgo=`,
		`data:image/jpeg;base64,/9j/4AAQSkZJRg==`,
		`data:image/gif;base64,R0lGODlhAQABAA==`,
		`data:image/webp;base64,UklGRhIAAABXRUJQ`,
	} {
		if !validImageDataURI(v, cap) {
			t.Errorf("rejected a legitimate image data URI: %q", v)
		}
	}
}

// A preset id is interpolated into the client's markup too, so it is bounded to
// a charset that cannot carry a quote, a paren or a scheme.
func TestGuildBannerPresetIDCharset(t *testing.T) {
	for _, bad := range []string{
		"", "Neon", "neon coliseum", "neon\"", "url(x)", "a/../b", "a:b",
		"toolongtoolongtoolongtoolongtoolong",
	} {
		if validPresetID(bad) {
			t.Errorf("accepted preset id %q", bad)
		}
	}
	for _, good := range []string{"neon-coliseum", "vinyl-night", "yule", "a", "x0-9"} {
		if !validPresetID(good) {
			t.Errorf("rejected preset id %q", good)
		}
	}
}
