package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPublicPeersOnly pins the TURN SSRF guard: the relay must refuse to forward
// toward loopback, private, link-local (incl. cloud metadata), and CGNAT
// addresses — anything an attacker could use to pivot from this public relay
// into a private network — while still relaying between real public peers.
func TestPublicPeersOnly(t *testing.T) {
	if allowPrivatePeers {
		t.Skip("CONCORD_TURN_ALLOW_PRIVATE is set in this environment")
	}
	deny := []string{
		"127.0.0.1",       // loopback
		"::1",             // loopback v6
		"169.254.169.254", // link-local — cloud metadata
		"169.254.1.1",     // link-local
		"10.0.0.5",        // RFC1918
		"172.16.9.9",      // RFC1918
		"192.168.1.1",     // RFC1918
		"100.64.0.1",      // RFC6598 CGNAT
		"100.127.255.254", // RFC6598 CGNAT
		"0.0.0.0",         // unspecified
		"fc00::1",         // ULA (fly 6PN lives in here)
		"fe80::1",         // link-local v6
		"224.0.0.1",       // multicast
	}
	for _, ip := range deny {
		if publicPeersOnly(nil, net.ParseIP(ip)) {
			t.Errorf("relay MUST refuse peer %s (SSRF/pivot risk)", ip)
		}
	}
	allow := []string{"8.8.8.8", "1.1.1.1", "203.0.113.7", "2606:4700:4700::1111"}
	for _, ip := range allow {
		if !publicPeersOnly(nil, net.ParseIP(ip)) {
			t.Errorf("relay should allow public peer %s", ip)
		}
	}
	if publicPeersOnly(nil, nil) {
		t.Error("nil peer IP must be denied")
	}
}

// TestTURNCredsRateLimit: /turn has to stay open — a guest joining a meeting
// link has no account to authenticate with — but every pair it hands out is an
// hour's licence to push media through a relay the operator pays for. Open is
// not unlimited.
func TestTURNCredsRateLimit(t *testing.T) {
	ts := &turnServer{secret: "test-secret", public: "concord.example", port: "3478"}

	ask := func(ip string) int {
		r := httptest.NewRequest(http.MethodGet, "/turn", nil)
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		ts.handleTURNCreds(w, r)
		return w.Code
	}

	for i := 0; i < int(turnCredsBurst); i++ {
		if got := ask("198.51.100.4"); got != http.StatusOK {
			t.Fatalf("request %d inside the burst answered %d; a client renegotiating "+
				"a call must not be refused its ICE config", i, got)
		}
	}
	if got := ask("198.51.100.4"); got != http.StatusTooManyRequests {
		t.Fatalf("past the burst /turn answered %d, want %d", got, http.StatusTooManyRequests)
	}
	// Per IP: one harvester must not stop everyone else's calls from connecting.
	if got := ask("203.0.113.11"); got != http.StatusOK {
		t.Fatalf("a different client IP answered %d — the limiter is not per IP", got)
	}
}
