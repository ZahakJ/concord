package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func launchServer(t *testing.T) *Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return New(ctx, false, WithAuthToken("the-real-token"))
}

// page is the Origin the page's own fetch carries.
const page = "http://127.0.0.1:8787"

func exchange(s *Server, code, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/launch", strings.NewReader(`{"code":"`+code+`"}`))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.HandleLaunch(w, r)
	return w
}

// A launch code opens the page once: it buys the token, and the same code
// never buys it again. That is what lets the code ride places the token must
// not (a browser's command line, a terminal, a launcher's output): a copy
// found there later is spent.
func TestLaunchCodeIsSingleUse(t *testing.T) {
	s := launchServer(t)
	code := s.MintLaunchCode()
	if code == "" || strings.Contains(code, "the-real-token") {
		t.Fatalf("code %q", code)
	}
	w := exchange(s, code, page)
	if w.Code != http.StatusOK {
		t.Fatalf("first use: %d %s", w.Code, w.Body)
	}
	var got struct{ Token string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Token != "the-real-token" {
		t.Fatalf("first use returned %q (%v)", w.Body, err)
	}
	if w := exchange(s, code, page); w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "the-real-token") {
		t.Fatalf("second use: %d %s", w.Code, w.Body)
	}
}

func TestLaunchCodeExpires(t *testing.T) {
	s := launchServer(t)
	now := time.Now()
	s.launch.now = func() time.Time { return now }
	code := s.MintLaunchCode()
	now = now.Add(launchCodeTTL + time.Second)
	if w := exchange(s, code, page); w.Code != http.StatusForbidden {
		t.Fatalf("expired code: %d %s", w.Code, w.Body)
	}
}

func TestLaunchRefusesWrongCodesAndForeignPages(t *testing.T) {
	s := launchServer(t)
	for _, c := range []string{"", "nope", strings.Repeat("0", 32)} {
		if w := exchange(s, c, page); w.Code != http.StatusForbidden {
			t.Errorf("code %q: %d", c, w.Code)
		}
	}
	code := s.MintLaunchCode()
	if w := exchange(s, code, "https://evil.example"); w.Code != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", w.Code)
	}
	// The refused attempt did not spend it.
	if w := exchange(s, code, "http://127.0.0.1:8787"); w.Code != http.StatusOK {
		t.Fatalf("local origin after a refused foreign one: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/launch?code="+s.MintLaunchCode(), nil)
	w := httptest.NewRecorder()
	s.HandleLaunch(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", w.Code)
	}
}

// Unspent codes are bounded: a caller minting in a loop cannot grow memory.
func TestLaunchCodesAreBounded(t *testing.T) {
	s := launchServer(t)
	first := s.MintLaunchCode()
	for i := 0; i < maxLaunchCodes; i++ {
		s.MintLaunchCode()
	}
	if n := s.launch.len(); n > maxLaunchCodes {
		t.Fatalf("%d codes held", n)
	}
	if w := exchange(s, first, page); w.Code != http.StatusForbidden {
		t.Fatalf("the oldest code survived the cap: %d", w.Code)
	}
}

// The page's fetch always carries an Origin, so a request without one is
// refused, and spends nothing. That stops only clients that send no Origin:
// a program can set a loopback Origin itself (the next test's first
// redemption is such a request), and against that the defence is the 409
// and the rotation.
func TestLaunchRefusesARequestWithNoOrigin(t *testing.T) {
	s := launchServer(t)
	code := s.MintLaunchCode()
	if w := exchange(s, code, ""); w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "the-real-token") {
		t.Fatalf("no Origin: %d %s", w.Code, w.Body)
	}
	if w := exchange(s, code, page); w.Code != http.StatusOK {
		t.Fatalf("the page after a refused originless try: %d %s", w.Code, w.Body)
	}
}

// When the page finds its code already spent, someone else redeemed it first
// and holds the token. The page is told so (409, not the 403 of a bad code),
// and the server rotates the token: what the first redeemer took stops
// working, including an event stream it opened, the revoke hooks run (the
// browser build cancels any device-link offer there), the new token reaches
// the rotation hook and only fresh codes, and the page is told how to get a
// fresh code.
func TestLaunchCodeRedeemedTwiceRotatesTheToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var told []string
	revoked := 0
	s := New(ctx, false, WithAuthToken("the-real-token"),
		WithTokenRotated(func(tok string) { told = append(told, tok) }),
		WithRevoke(func() { revoked++ }))
	s.SetLaunchCommand("curl --unix-socket /run/x.sock http://launch/code")
	code := s.MintLaunchCode()
	before := s.MintLaunchCode() // minted before the rotation, never used
	bearer := func(tok string) bool {
		r := httptest.NewRequest(http.MethodPost, "/rpc", nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		return s.authorized(r)
	}

	if w := exchange(s, code, page); w.Code != http.StatusOK {
		t.Fatalf("first redemption: %d %s", w.Code, w.Body)
	}
	if !bearer("the-real-token") {
		t.Fatal("the token does not work before any rotation")
	}
	events := httptest.NewServer(http.HandlerFunc(s.HandleEvents))
	defer events.Close()
	resp, err := http.Get(events.URL + "/?token=the-real-token")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("event stream: %v %v", resp, err)
	}
	defer resp.Body.Close()

	if w := exchange(s, "never-minted", page); w.Code != http.StatusForbidden || !bearer("the-real-token") || revoked != 0 {
		t.Fatalf("an unknown code rotated or was not refused: %d (revoked %d)", w.Code, revoked)
	}
	w2 := exchange(s, code, page)
	var conflict struct{ Fresh string }
	if w2.Code != http.StatusConflict || json.Unmarshal(w2.Body.Bytes(), &conflict) != nil ||
		conflict.Fresh != "curl --unix-socket /run/x.sock http://launch/code" || strings.Contains(w2.Body.String(), "the-real-token") {
		t.Fatalf("second redemption: %d %s", w2.Code, w2.Body)
	}
	if revoked != 1 {
		t.Fatalf("revoke hooks ran %d times on the rotation, want 1", revoked)
	}
	if bearer("the-real-token") {
		t.Fatal("the token the first redeemer took still works")
	}
	if len(told) != 1 || told[0] == "" || told[0] == "the-real-token" || !bearer(told[0]) {
		t.Fatalf("rotation hook told %q", told)
	}
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(resp.Body); done <- err }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the event stream opened with the old token is still open")
	}
	if w := exchange(s, before, page); w.Code != http.StatusForbidden {
		t.Fatalf("a code minted before the rotation still buys a token: %d %s", w.Code, w.Body)
	}
	w := exchange(s, s.MintLaunchCode(), page)
	var got struct{ Token string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK || got.Token != told[0] {
		t.Fatalf("a fresh code bought %d %q, want the new token", w.Code, w.Body)
	}
}
