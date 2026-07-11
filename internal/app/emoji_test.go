package app

import "testing"

// TestEmojiImageValidationRejectsXSS locks in the fix for the stored-XSS via
// custom emoji: the image string is rendered into an <img src="…"> in the
// client, so anything that could break out of that attribute (a quote, an event
// handler, a script URL, or a scriptable SVG) must be rejected at ingest.
func TestEmojiImageValidationRejectsXSS(t *testing.T) {
	good := []string{
		"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
		"data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==",
		"data:image/jpeg;base64,/9j/4AAQSkZJRg==",
		"data:image/webp;base64,UklGR==",
	}
	for _, g := range good {
		if !validEmojiImage(g) {
			t.Errorf("valid image rejected: %q", g)
		}
	}

	bad := []string{
		`data:image/png;base64,x" onerror="alert(1)`,         // attribute breakout → XSS
		`data:image/png;base64,x"><script>alert(1)</script>`, // tag breakout
		`data:image/svg+xml,<svg onload=alert(1)>`,           // scriptable svg
		`data:image/png;base64,valid=='"`,                    // trailing quote
		`javascript:alert(1)`,                                // script URL
		`data:image/png;base64,` + "a b",                     // space (not base64)
		`data:text/html;base64,PHNjcmlwdD4=`,                 // wrong mime
		`https://evil.example/x.png`,                         // remote URL
		"",                                                   // empty
	}
	for _, b := range bad {
		if validEmojiImage(b) {
			t.Errorf("dangerous image accepted: %q", b)
		}
	}
}
