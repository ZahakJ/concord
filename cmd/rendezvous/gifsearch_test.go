package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"

	cnet "github.com/zahak/concord/internal/net"
)

// The proxy is the only part of the rendezvous that makes outbound requests on
// a peer's behalf, so these tests are mostly about what it REFUSES to fetch.
// None of them touch the real Tenor: a fake API server on loopback stands in,
// reached through CONCORD_TENOR_BASE — the same knob an operator would use for
// a self-hosted mirror.

// fakeTenor serves a one-result search page and the images it names.
func fakeTenor(t *testing.T, gif []byte) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") == "" {
			t.Errorf("search request carried no API key")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"results":[{"id":"abc","content_description":"cat vibing",
		  "media_formats":{
		    "tinygif":{"url":%q,"dims":[100,80],"size":10},
		    "mediumgif":{"url":%q,"dims":[320,240],"size":%d}}}],"next":"20"}`,
			srv.URL+"/tiny.gif", srv.URL+"/full.gif", len(gif))
	})
	mux.HandleFunc("/tiny.gif", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write(gif)
	})
	mux.HandleFunc("/full.gif", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write(gif)
	})
	// Not an image: the proxy must refuse to relay it whatever the URL says.
	mux.HandleFunc("/page.gif", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>nope</html>"))
	})
	// Bigger than the relay ceiling.
	mux.HandleFunc("/huge.gif", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write(make([]byte, cnet.MaxGifMediaBytes+1))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestProxy(t *testing.T, key, base string) *gifProxy {
	t.Helper()
	t.Setenv("CONCORD_TENOR_KEY", key)
	t.Setenv("CONCORD_TENOR_BASE", base)
	t.Setenv("CONCORD_TENOR_CONTENTFILTER", "")
	p, err := newGifProxy()
	if err != nil {
		t.Fatalf("newGifProxy: %v", err)
	}
	return p
}

func ask(t *testing.T, p *gifProxy, from peer.ID, req cnet.GifRequest) cnet.GifResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.handle(context.Background(), from, body)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	var resp cnet.GifResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	return resp
}

// TestGifProxyNoKey: an operator who never configured a key has not broken
// anything, so the answer must be "unavailable" with a reason — never an error
// that reads like a bug, and never silence.
func TestGifProxyNoKey(t *testing.T) {
	p := newTestProxy(t, "", "https://tenor.googleapis.com")
	for _, op := range []string{"status", "search", "media"} {
		resp := ask(t, p, "peer-a", cnet.GifRequest{Op: op, Query: "cat", Ref: "x.y"})
		if resp.Status != cnet.GifStatusUnavailable {
			t.Fatalf("op %q: status = %q, want unavailable", op, resp.Status)
		}
		if resp.Detail == "" {
			t.Fatalf("op %q: unavailable with no reason to show", op)
		}
	}
}

// TestGifProxySearchAndMedia is the happy path, and the assertion that matters
// most: a result carries handles, and the BYTES come back through the proxy.
func TestGifProxySearchAndMedia(t *testing.T) {
	gif := []byte("GIF89a" + strings.Repeat("x", 200))
	srv := fakeTenor(t, gif)
	p := newTestProxy(t, "test-key", srv.URL)

	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "cat", Limit: 5})
	if resp.Status != cnet.GifStatusOK {
		t.Fatalf("status = %q (%s), want ok", resp.Status, resp.Detail)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %+v, want one", resp.Results)
	}
	hit := resp.Results[0]
	if hit.Title != "cat vibing" || hit.Width != 320 || hit.Height != 240 {
		t.Fatalf("hit = %+v, want the mediumgif's metadata", hit)
	}
	if resp.Next != "20" {
		t.Fatalf("Next = %q, want the upstream cursor", resp.Next)
	}
	// A handle must not be a URL: the client has to be incapable of fetching
	// anything itself, or the whole feature is decorative.
	if strings.Contains(hit.Preview, "://") || strings.Contains(hit.Full, "://") {
		t.Fatalf("handles look like URLs: %q / %q", hit.Preview, hit.Full)
	}

	media := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: hit.Full, Full: true})
	if media.Status != cnet.GifStatusOK {
		t.Fatalf("media status = %q (%s), want ok", media.Status, media.Detail)
	}
	if media.Subtype != "gif" || string(media.Media) != string(gif) {
		t.Fatalf("media = %d bytes subtype %q, want the %d bytes we served", len(media.Media), media.Subtype, len(gif))
	}
}

// TestGifProxyRefusesForgedHandles: a peer must not be able to name a URL. This
// is the SSRF gate — without it the node is an open proxy into the operator's
// network.
func TestGifProxyRefusesForgedHandles(t *testing.T) {
	srv := fakeTenor(t, []byte("GIF89a"))
	p := newTestProxy(t, "test-key", srv.URL)

	// A well-formed handle for a URL we never minted.
	forged := func(raw string) string {
		return handleFor(raw, []byte("not-the-secret"))
	}
	for _, raw := range []string{
		"http://127.0.0.1:1/secret",
		"http://169.254.169.254/latest/meta-data/",
		"file:///etc/passwd",
		"https://evil.example/x.gif",
		"https://media.tenor.com/real.gif", // right host, wrong signature
	} {
		resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: forged(raw)})
		if resp.Status != cnet.GifStatusExpired {
			t.Fatalf("forged handle for %q: status = %q, want it refused", raw, resp.Status)
		}
	}

	// A genuine signature over a URL outside the allowlist is refused too: the
	// MAC proves this node wrote the string, not that the address is allowed.
	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: p.mint("http://127.0.0.1:1/secret")})
	if resp.Status != cnet.GifStatusExpired {
		t.Fatalf("signed off-allowlist URL: status = %q, want it refused", resp.Status)
	}
}

// handleFor builds a handle signed with an arbitrary secret, for the forgery
// test above.
func handleFor(raw string, secret []byte) string {
	p := &gifProxy{secret: secret}
	return p.mint(raw)
}

// TestGifProxyAllowURL pins the allowlist directly, including the reason the
// operator's own base is exempt from the https requirement.
func TestGifProxyAllowURL(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:9999")
	p := &gifProxy{base: base}
	cases := map[string]bool{
		"https://media.tenor.com/a.gif":     true,
		"https://media1.tenor.com/a.gif":    true,
		"https://tenor.com/a.gif":           true,
		"https://tenor.googleapis.com/v2/s": false, // only reachable as the configured base
		"http://127.0.0.1:9999/tiny.gif":    true,  // the operator's own base
		"http://media.tenor.com/a.gif":      false, // plaintext to a host we didn't configure
		"https://tenor.com.evil.example/a":  false,
		"https://eviltenor.com/a.gif":       false,
		"https://127.0.0.1/a.gif":           false,
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.allowURL(u); got != want {
			t.Errorf("allowURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

// TestGifProxyRefusesNonImages: the proxy relays pictures. An address that
// answers with HTML is not relayed, whatever it was signed as.
func TestGifProxyRefusesNonImages(t *testing.T) {
	srv := fakeTenor(t, []byte("GIF89a"))
	p := newTestProxy(t, "test-key", srv.URL)
	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: p.mint(srv.URL + "/page.gif")})
	if resp.Status != cnet.GifStatusUpstream {
		t.Fatalf("status = %q, want upstream refusal", resp.Status)
	}
	if len(resp.Media) != 0 {
		t.Fatal("non-image bytes were relayed")
	}
}

// TestGifProxyCapsMediaSize: Content-Length is a claim, so the cap has to be
// enforced on the bytes actually read.
func TestGifProxyCapsMediaSize(t *testing.T) {
	srv := fakeTenor(t, []byte("GIF89a"))
	p := newTestProxy(t, "test-key", srv.URL)
	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: p.mint(srv.URL + "/huge.gif")})
	if resp.Status != cnet.GifStatusUpstream {
		t.Fatalf("status = %q, want the oversized body refused", resp.Status)
	}
	if len(resp.Media) != 0 {
		t.Fatalf("relayed %d bytes of an over-cap image", len(resp.Media))
	}
}

// TestGifProxyRateLimits: the scarce resource is the operator's API quota, so a
// peer that hammers search gets told to wait rather than spending it.
func TestGifProxyRateLimits(t *testing.T) {
	srv := fakeTenor(t, []byte("GIF89a"))
	p := newTestProxy(t, "test-key", srv.URL)

	limited := false
	for i := 0; i < int(gifSearchBurst)+5; i++ {
		if ask(t, p, "greedy", cnet.GifRequest{Op: "search", Query: "cat"}).Status == cnet.GifStatusRateLimited {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatalf("search was never rate limited after %d requests", int(gifSearchBurst)+5)
	}
	// The bucket is per peer: one greedy client must not lock everyone out.
	if got := ask(t, p, "polite", cnet.GifRequest{Op: "search", Query: "cat"}).Status; got != cnet.GifStatusOK {
		t.Fatalf("a different peer got %q — the limiter is not per peer", got)
	}
}

// TestGifProxyBoundsQuery: a peer must not be able to push unbounded text
// through the operator's API account, and control characters never come from a
// person typing in the box.
func TestGifProxyBoundsQuery(t *testing.T) {
	long := strings.Repeat("a", gifMaxQueryRunes*3)
	if got := cleanQuery(long); len([]rune(got)) != gifMaxQueryRunes {
		t.Fatalf("query length = %d, want %d", len([]rune(got)), gifMaxQueryRunes)
	}
	// A zero-width space (Cf) and a newline: neither can come from the box.
	if got := cleanQuery("c\u200bat\n"); got != "cat" {
		t.Fatalf("cleanQuery stripped to %q, want %q", got, "cat")
	}
	srv := fakeTenor(t, []byte("GIF89a"))
	p := newTestProxy(t, "test-key", srv.URL)
	if got := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "   "}).Status; got != cnet.GifStatusBadRequest {
		t.Fatalf("empty query status = %q, want bad_request", got)
	}
}

// TestGifProxyNeverLeaksTheKey: net/http puts the full URL — API key and all —
// into its error strings, and those errors must not reach a peer.
func TestGifProxyNeverLeaksTheKey(t *testing.T) {
	const key = "SUPER-SECRET-KEY"
	// A base that resolves to nothing, so the fetch fails and takes the error
	// path that would otherwise carry the URL.
	p := newTestProxy(t, key, "http://127.0.0.1:1")
	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "cat"})
	if resp.Status != cnet.GifStatusUpstream {
		t.Fatalf("status = %q, want upstream", resp.Status)
	}
	if strings.Contains(resp.Detail, key) || strings.Contains(resp.Detail, "127.0.0.1") {
		t.Fatalf("reply leaked the request URL: %q", resp.Detail)
	}
}
