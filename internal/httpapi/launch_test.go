package httpapi

import (
	"context"
	"encoding/json"
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
	w := exchange(s, code, "")
	if w.Code != http.StatusOK {
		t.Fatalf("first use: %d %s", w.Code, w.Body)
	}
	var got struct{ Token string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Token != "the-real-token" {
		t.Fatalf("first use returned %q (%v)", w.Body, err)
	}
	if w := exchange(s, code, ""); w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "the-real-token") {
		t.Fatalf("second use: %d %s", w.Code, w.Body)
	}
}

func TestLaunchCodeExpires(t *testing.T) {
	s := launchServer(t)
	now := time.Now()
	s.launch.now = func() time.Time { return now }
	code := s.MintLaunchCode()
	now = now.Add(launchCodeTTL + time.Second)
	if w := exchange(s, code, ""); w.Code != http.StatusForbidden {
		t.Fatalf("expired code: %d %s", w.Code, w.Body)
	}
}

func TestLaunchRefusesWrongCodesAndForeignPages(t *testing.T) {
	s := launchServer(t)
	for _, c := range []string{"", "nope", strings.Repeat("0", 32)} {
		if w := exchange(s, c, ""); w.Code != http.StatusForbidden {
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
	if w := exchange(s, first, ""); w.Code != http.StatusForbidden {
		t.Fatalf("the oldest code survived the cap: %d", w.Code)
	}
}
