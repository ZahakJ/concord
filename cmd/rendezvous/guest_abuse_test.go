package main

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// /guest/ws is the node's most exposed door: no account, no token this node can
// check, and it makes the node DIAL a peer id the caller chose. The framing
// contract lives in guest_smoke_test.go; this file is about what the door
// refuses. Nothing here touches a network — the gate is checked before the
// WebSocket upgrade, on purpose, so a recorder is enough to see the answer.

// somePeerID returns a syntactically valid peer id nothing is listening on.
func somePeerID(t *testing.T) string {
	t.Helper()
	_, pub, err := p2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// attempt makes one /guest/ws request and returns the status. The request is a
// plain GET, not an upgrade, so anything that gets PAST the gate fails in
// gorilla's upgrade check instead — a status the gate never produces, which is
// what makes "was it refused, and why" readable.
func attempt(t *testing.T, gate *guestGate, hostID, ip string) int {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/guest/ws?h="+hostID, nil)
	r.Header.Set("X-Forwarded-For", ip)
	w := httptest.NewRecorder()
	relayGuest(context.Background(), nil, gate, w, r)
	return w.Code
}

// TestGuestGatewayPerIPLimit: a visitor has no identity, so their IP is the only
// handle there is — and without it a loop can make this node dial forever.
func TestGuestGatewayPerIPLimit(t *testing.T) {
	gate := &guestGate{}
	id := somePeerID(t)

	for i := 0; i < int(guestConnBurst); i++ {
		if got := attempt(t, gate, id, "198.51.100.7"); got == http.StatusTooManyRequests {
			t.Fatalf("attempt %d inside the burst was refused; a guest reconnecting "+
				"after a network flap must not be locked out", i)
		}
	}
	if got := attempt(t, gate, id, "198.51.100.7"); got != http.StatusTooManyRequests {
		t.Fatalf("past the burst the gateway answered %d, want %d", got, http.StatusTooManyRequests)
	}
	// Per IP, not global: one loop must not close the door on everyone else.
	if got := attempt(t, gate, id, "203.0.113.9"); got == http.StatusTooManyRequests {
		t.Fatal("a different client IP was limited by another's usage")
	}
}

// TestGuestGatewaySessionCap: the per-IP budget is worth nothing to a botnet, so
// the number of sockets alive at once is capped too. Each live session holds a
// libp2p stream, a browser socket and a goroutine pair.
func TestGuestGatewaySessionCap(t *testing.T) {
	gate := &guestGate{}
	gate.sessions.Store(maxGuestSessions)

	// A fresh IP with a full bucket: the only thing that can refuse this is the
	// global cap.
	if got := attempt(t, gate, somePeerID(t), "192.0.2.55"); got != http.StatusServiceUnavailable {
		t.Fatalf("at capacity the gateway answered %d, want %d", got, http.StatusServiceUnavailable)
	}
	if n := gate.sessions.Load(); n != maxGuestSessions {
		t.Fatalf("a refused request left the session count at %d — the slot it "+
			"claimed was never given back, so the gateway would ratchet shut", n)
	}
	// Once a session ends, the door opens again.
	gate.sessions.Store(maxGuestSessions - 1)
	if got := attempt(t, gate, somePeerID(t), "192.0.2.55"); got == http.StatusServiceUnavailable {
		t.Fatal("the gateway stayed at capacity after a session was released")
	}
}

// TestGuestOriginAllowed pins the CSRF check. Origin is not authentication —
// anyone with a socket can claim any value — so this is only about stopping
// OTHER PAGES from driving a visitor's meeting in their browser.
func TestGuestOriginAllowed(t *testing.T) {
	const pub = "concord.example"
	cases := []struct {
		origin, host string
		want         bool
		why          string
	}{
		{"", "", true, "unconfigured: a dev run has no one name to compare against"},
		{"https://anything.example", "", true, "unconfigured: still permissive"},
		{"https://concord.example", pub, true, "the configured host"},
		{"https://CONCORD.example", pub, true, "hostnames are case-insensitive"},
		{"http://concord.example", pub, true, "scheme is whatever terminates TLS in front"},
		{"https://evil.example", pub, false, "another site driving the meeting"},
		{"https://concord.example.evil.test", pub, false, "suffix, not the host"},
		{"https://evil.test/concord.example", pub, false, "path, not the host"},
		{"", pub, false, "every real guest is a browser, and browsers send Origin"},
		{"null", pub, false, "a sandboxed frame's opaque origin"},
		{"file://", pub, false, "not an http origin"},
	}
	for _, c := range cases {
		if got := guestOriginAllowed(c.origin, c.host); got != c.want {
			t.Errorf("guestOriginAllowed(%q, %q) = %v, want %v — %s", c.origin, c.host, got, c.want, c.why)
		}
	}
}
