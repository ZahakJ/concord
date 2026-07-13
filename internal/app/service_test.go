package app

import "testing"

// TestValidImageDataURIRejectsCSSBreakout pins the banner CSS-injection fix: a
// data URI that tries to break out of the CSS url("…") wrapper (quotes, parens,
// semicolons, an external url()) must be rejected, while a real base64 image the
// app itself produces is accepted.
func TestValidImageDataURIRejectsCSSBreakout(t *testing.T) {
	good := []string{
		"data:image/jpeg;base64,/9j/4AAQSkZJRg==",
		"data:image/png;base64,iVBORw0KGgoAAAANSUhEUg==",
		"data:image/webp;base64,UklGRh4AAABXRUJQ",
	}
	for _, u := range good {
		if !validImageDataURI(u, 1<<20) {
			t.Errorf("rejected a legitimate image data URI: %q", u)
		}
	}
	bad := []string{
		`data:image/svg+xml,x");background:url(//attacker.example/p.gif?probe`, // CSS breakout
		`data:image/png;base64,ABC");background-image:url(//evil`,              // quote + paren breakout
		"data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=",                           // svg not allowed
		"data:image/png,notbase64",                                             // missing ;base64
		"data:text/html;base64,PGh0bWw+",                                       // not an image type
		"data:image/png;base64,",                                               // empty payload
		`data:image/png;base64,AAAA AAAA`,                                      // space is not base64
	}
	for _, u := range bad {
		if validImageDataURI(u, 1<<20) {
			t.Errorf("accepted a dangerous / malformed data URI: %q", u)
		}
	}
	// validBanner must route data: URIs through the strict check and still accept presets.
	if validBanner(`data:image/svg+xml,x");background:url(//evil`) {
		t.Error("validBanner accepted a CSS-breakout banner")
	}
	if !validBanner("preset:galaxy") || !validBanner("") {
		t.Error("validBanner rejected a legitimate preset / empty banner")
	}
}
