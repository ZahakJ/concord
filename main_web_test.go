//go:build !wails

package main

import "testing"

// TestLocalOriginCSRFGuard locks in the /rpc CSRF guard: requests with no Origin
// (native clients, same-origin no-Origin posts) and loopback Origins are allowed;
// a request carrying any real website's Origin — the CSRF vector — is rejected.
func TestLocalOriginCSRFGuard(t *testing.T) {
	allowed := []string{
		"",
		"http://127.0.0.1:8787",
		"http://localhost:8787",
		"https://localhost",
		"http://[::1]:8787",
	}
	for _, o := range allowed {
		if !localOrigin(o) {
			t.Errorf("origin %q should be allowed", o)
		}
	}

	forbidden := []string{
		"https://evil.example",
		"http://attacker.com",             // DNS-rebind: Origin stays the attacker's domain
		"http://127.0.0.1.evil.com",       // loopback as a subdomain label
		"http://localhost.evil.com",
		"null",                            // opaque origin (some sandboxed contexts)
		"http://169.254.169.254",
	}
	for _, o := range forbidden {
		if localOrigin(o) {
			t.Errorf("cross-origin %q must be rejected", o)
		}
	}
}
