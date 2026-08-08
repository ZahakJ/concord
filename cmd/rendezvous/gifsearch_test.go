package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"

	cnet "github.com/ZahakJ/concord/internal/net"
)

// The proxy is the only part of the rendezvous that makes outbound requests on
// a peer's behalf, so these tests are mostly about what it REFUSES to fetch.
// None of them touch a real API — not Giphy, not a Tenor mirror: a fake upstream
// on loopback stands in, reached through CONCORD_GIF_BASE, which is the same
// knob an operator would use for a self-hosted mirror.
//
// There is a second thing under test here that did not exist before: the
// provider abstraction. Tenor's public API was decommissioned on 30 June 2026
// and the whole feature died with it, so the matrix of "which vendor, which key,
// which host" and the per-provider allowlist are now load-bearing, and are
// pinned as tightly as the SSRF gate.

// fakeGiphy serves a Giphy-v1-shaped search page and the images it names.
//
// The numbers are JSON STRINGS on purpose: Giphy really does serialize width,
// height and size that way, and a fixture that used real ints would let a
// decoder that cannot parse the live API pass its tests.
func fakeGiphy(t *testing.T, gif []byte) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/gifs/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") == "" {
			t.Errorf("search request carried no API key")
		}
		if r.URL.Query().Get("rating") == "" {
			t.Errorf("search request carried no rating (content filter)")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("q") {
		case "nothing":
			// A search that genuinely matched nothing. Must be "ok with zero
			// results", never an error: there is nothing wrong with the node.
			fmt.Fprint(w, `{"data":[],"pagination":{"total_count":0,"count":0,"offset":0},"meta":{"status":200}}`)
		case "garbage":
			fmt.Fprint(w, `{"data":[{"id":`)
		default:
			fmt.Fprintf(w, `{"data":[{"id":"abc","title":"cat GIF","alt_text":"cat vibing",
			  "images":{
			    "fixed_width_small":{"url":%q,"width":"100","height":"80","size":"10"},
			    "downsized":{"url":%q,"width":"320","height":"240","size":"%d"},
			    "original":{"url":%q,"width":"480","height":"360","size":""}}}],
			  "pagination":{"total_count":100,"count":1,"offset":%d},"meta":{"status":200}}`,
				srv.URL+"/tiny.gif", srv.URL+"/full.gif", len(gif), srv.URL+"/full.gif",
				giphyOffset(r.URL.Query().Get("offset")))
		}
	})
	fakeGifMedia(mux, gif)
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// fakeTenor serves a Tenor-v2-shaped search page. Kept because the Tenor
// provider is kept: an operator with a Tenor-compatible mirror is still a
// supported deployment, and the only way to know it still decodes is to decode
// it.
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
	fakeGifMedia(mux, gif)
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// fakeGifMedia hangs the media endpoints both fake upstreams share, including
// the two the proxy has to refuse.
func fakeGifMedia(mux *http.ServeMux, gif []byte) {
	img := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write(gif)
	}
	mux.HandleFunc("/tiny.gif", img)
	mux.HandleFunc("/full.gif", img)
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
}

// clearGifEnv blanks every variable the proxy reads, so a test never inherits
// an operator's real configuration — least of all a real API key.
func clearGifEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"CONCORD_GIF_PROVIDER", "CONCORD_GIF_KEY", "CONCORD_GIF_BASE", "CONCORD_GIF_CONTENTFILTER",
		"CONCORD_TENOR_KEY", "CONCORD_TENOR_BASE", "CONCORD_TENOR_CONTENTFILTER",
	} {
		t.Setenv(k, "")
	}
}

func newTestProxy(t *testing.T, provider, key, base string) *gifProxy {
	t.Helper()
	clearGifEnv(t)
	t.Setenv("CONCORD_GIF_PROVIDER", provider)
	t.Setenv("CONCORD_GIF_KEY", key)
	t.Setenv("CONCORD_GIF_BASE", base)
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
	p := newTestProxy(t, "", "", "")
	for _, op := range []string{"status", "search", "media"} {
		resp := ask(t, p, "peer-a", cnet.GifRequest{Op: op, Query: "cat", Ref: "x.y"})
		if resp.Status != cnet.GifStatusUnavailable {
			t.Fatalf("op %q: status = %q, want unavailable", op, resp.Status)
		}
		if resp.Detail == "" {
			t.Fatalf("op %q: unavailable with no reason to show", op)
		}
		// The provider is named even when unusable, so the picker's provenance
		// line never has to guess (it used to guess "Tenor", and was wrong).
		if resp.Source != "Giphy" {
			t.Fatalf("op %q: Source = %q, want the default provider named", op, resp.Source)
		}
	}
}

// TestGifUnavailableIsTrue is the regression test for the failure this whole
// change exists to fix. The old message told operators to set CONCORD_TENOR_KEY.
// Google closed Tenor's signups on 30 June 2026, so that advice became
// impossible to follow — a failure state that lied about its own cure.
func TestGifUnavailableIsTrue(t *testing.T) {
	giphy := newTestProxy(t, "giphy", "", "")
	d := giphy.unavailableDetail()
	if strings.Contains(d, "CONCORD_TENOR_KEY") {
		t.Fatalf("unavailable detail still asks for a Tenor key: %q", d)
	}
	if !strings.Contains(d, "CONCORD_GIF_KEY") {
		t.Fatalf("unavailable detail names nothing to configure: %q", d)
	}

	// A node deliberately set to Tenor and left keyless has to hear the reason
	// its vendor cannot be signed up for.
	tenor := newTestProxy(t, "tenor", "", "")
	d = tenor.unavailableDetail()
	if !strings.Contains(d, "decommissioned") || !strings.Contains(d, "CONCORD_GIF_PROVIDER=giphy") {
		t.Fatalf("Tenor-configured detail does not explain the shutdown: %q", d)
	}
}

// TestGiphySearchAndMedia is the happy path, and the assertion that matters
// most: a result carries handles, and the BYTES come back through the proxy.
func TestGiphySearchAndMedia(t *testing.T) {
	gif := []byte("GIF89a" + strings.Repeat("x", 200))
	srv := fakeGiphy(t, gif)
	p := newTestProxy(t, "giphy", "test-key", srv.URL)

	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "cat", Limit: 5})
	if resp.Status != cnet.GifStatusOK {
		t.Fatalf("status = %q (%s), want ok", resp.Status, resp.Detail)
	}
	if resp.Source != "Giphy" {
		t.Fatalf("Source = %q, want Giphy", resp.Source)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %+v, want one", resp.Results)
	}
	hit := resp.Results[0]
	// alt_text wins over title, and the dimensions come from the FULL rendition.
	if hit.Title != "cat vibing" || hit.Width != 320 || hit.Height != 240 {
		t.Fatalf("hit = %+v, want the downsized rendition's metadata", hit)
	}
	// Giphy paginates by offset, so the cursor is "where the next page starts".
	if resp.Next != "1" {
		t.Fatalf("Next = %q, want the next offset", resp.Next)
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

	// Second page: the cursor we handed out has to be the one that comes back.
	page2 := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "cat", Pos: resp.Next})
	if page2.Status != cnet.GifStatusOK || page2.Next != "2" {
		t.Fatalf("page 2 = %q next %q, want ok next 2", page2.Status, page2.Next)
	}
}

// TestGiphyEmptyResults: a search that matched nothing is a successful search.
// The picker says "found nothing" for it, and would say "broken" for an error.
func TestGiphyEmptyResults(t *testing.T) {
	srv := fakeGiphy(t, []byte("GIF89a"))
	p := newTestProxy(t, "giphy", "test-key", srv.URL)
	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "nothing"})
	if resp.Status != cnet.GifStatusOK {
		t.Fatalf("status = %q (%s), want ok", resp.Status, resp.Detail)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("results = %+v, want none", resp.Results)
	}
	if resp.Next != "" {
		t.Fatalf("Next = %q, want no next page offered", resp.Next)
	}
}

// TestGiphyMalformedJSON: an upstream that answers with truncated JSON is an
// upstream problem, reported as one, with nothing of its body echoed back.
func TestGiphyMalformedJSON(t *testing.T) {
	srv := fakeGiphy(t, []byte("GIF89a"))
	p := newTestProxy(t, "giphy", "test-key", srv.URL)
	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "garbage"})
	if resp.Status != cnet.GifStatusUpstream {
		t.Fatalf("status = %q, want upstream", resp.Status)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("results = %+v, want none", resp.Results)
	}
}

// TestTenorProviderStillDecodes: the Tenor implementation is kept for mirrors,
// so it has to keep working when explicitly selected — just never by default.
func TestTenorProviderStillDecodes(t *testing.T) {
	gif := []byte("GIF89a" + strings.Repeat("x", 50))
	srv := fakeTenor(t, gif)
	p := newTestProxy(t, "tenor", "test-key", srv.URL)

	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "cat"})
	if resp.Status != cnet.GifStatusOK || resp.Source != "Tenor" {
		t.Fatalf("status = %q source = %q, want ok/Tenor", resp.Status, resp.Source)
	}
	if len(resp.Results) != 1 || resp.Next != "20" {
		t.Fatalf("results = %+v next = %q, want one result and Tenor's opaque cursor", resp.Results, resp.Next)
	}
	media := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: resp.Results[0].Full})
	if media.Status != cnet.GifStatusOK || string(media.Media) != string(gif) {
		t.Fatalf("media status = %q, %d bytes", media.Status, len(media.Media))
	}
}

// TestGifProxyRefusesForgedHandles: a peer must not be able to name a URL. This
// is the SSRF gate — without it the node is an open proxy into the operator's
// network.
func TestGifProxyRefusesForgedHandles(t *testing.T) {
	srv := fakeGiphy(t, []byte("GIF89a"))
	p := newTestProxy(t, "giphy", "test-key", srv.URL)

	// A well-formed handle for a URL we never minted.
	forged := func(raw string) string {
		return handleFor(raw, []byte("not-the-secret"))
	}
	for _, raw := range []string{
		"http://127.0.0.1:1/secret",
		"http://169.254.169.254/latest/meta-data/",
		"file:///etc/passwd",
		"https://evil.example/x.gif",
		"https://media.giphy.com/real.gif", // right host, wrong signature
	} {
		resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: forged(raw)})
		if resp.Status != cnet.GifStatusExpired {
			t.Fatalf("forged handle for %q: status = %q, want it refused", raw, resp.Status)
		}
	}

	// A genuine signature over a URL outside the allowlist is refused too: the
	// MAC proves this node wrote the string, not that the address is allowed.
	for _, raw := range []string{
		"http://127.0.0.1:1/secret",
		// Signed by THIS node, on a host that belongs to a DIFFERENT provider.
		// A Giphy proxy has no business fetching from Tenor's CDN, even over a
		// handle it minted itself.
		"https://media.tenor.com/real.gif",
	} {
		resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: p.mint(raw)})
		if resp.Status != cnet.GifStatusExpired {
			t.Fatalf("signed off-allowlist URL %q: status = %q, want it refused", raw, resp.Status)
		}
	}
}

// handleFor builds a handle signed with an arbitrary secret, for the forgery
// test above.
func handleFor(raw string, secret []byte) string {
	p := &gifProxy{secret: secret}
	return p.mint(raw)
}

// TestGifProxyAllowURL pins the allowlist directly, PER PROVIDER. The cross
// entries are the point: after the Tenor shutdown the proxy speaks to whichever
// vendor it was configured for, and the allowlist must narrow with it rather
// than becoming the union of every vendor Concord has ever supported.
func TestGifProxyAllowURL(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:9999")
	cases := []struct {
		provider gifProvider
		url      string
		want     bool
		why      string
	}{
		{giphyProvider{}, "https://media.giphy.com/media/x/200w.gif", true, "Giphy's CDN"},
		{giphyProvider{}, "https://media3.giphy.com/media/x/200w.gif", true, "a Giphy CDN shard"},
		{giphyProvider{}, "https://i.giphy.com/x.gif", true, "Giphy's short host"},
		{giphyProvider{}, "https://giphy.com/x.gif", true, "the apex"},
		{giphyProvider{}, "https://api.giphy.com/v1/gifs/search", true, "under giphy.com like the rest"},
		{giphyProvider{}, "https://media.tenor.com/a.gif", false, "another provider's CDN"},
		{giphyProvider{}, "https://tenor.com/a.gif", false, "another provider's apex"},
		{giphyProvider{}, "http://media.giphy.com/a.gif", false, "plaintext to a host we didn't configure"},
		{giphyProvider{}, "https://giphy.com.evil.example/a", false, "suffix-confusion domain"},
		{giphyProvider{}, "https://evilgiphy.com/a.gif", false, "no dot before the allowed suffix"},
		{giphyProvider{}, "http://127.0.0.1:9999/tiny.gif", true, "the operator's own base"},
		{giphyProvider{}, "https://127.0.0.1/a.gif", false, "loopback that is not the base"},

		{tenorProvider{}, "https://media.tenor.com/a.gif", true, "Tenor's CDN"},
		{tenorProvider{}, "https://media1.tenor.com/a.gif", true, "a Tenor CDN shard"},
		{tenorProvider{}, "https://tenor.com/a.gif", true, "the apex"},
		{tenorProvider{}, "https://tenor.googleapis.com/v2/s", false, "only reachable as the configured base"},
		{tenorProvider{}, "https://media.giphy.com/a.gif", false, "another provider's CDN"},
		{tenorProvider{}, "https://tenor.com.evil.example/a", false, "suffix-confusion domain"},
		{tenorProvider{}, "https://eviltenor.com/a.gif", false, "no dot before the allowed suffix"},
		{tenorProvider{}, "http://127.0.0.1:9999/tiny.gif", true, "the operator's own base"},
	}
	for _, c := range cases {
		p := &gifProxy{provider: c.provider, base: base}
		u, err := url.Parse(c.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.allowURL(u); got != c.want {
			t.Errorf("%s: allowURL(%q) = %v, want %v (%s)", c.provider.name(), c.url, got, c.want, c.why)
		}
	}

	// And the same URL flipping verdict as the provider changes, stated as one
	// assertion because it is the property, not a side effect of the table.
	giphyMedia, _ := url.Parse("https://media.giphy.com/a.gif")
	tenorMedia, _ := url.Parse("https://media.tenor.com/a.gif")
	g := &gifProxy{provider: giphyProvider{}}
	tn := &gifProxy{provider: tenorProvider{}}
	if !g.allowURL(giphyMedia) || g.allowURL(tenorMedia) {
		t.Error("a Giphy proxy must allow only Giphy media")
	}
	if !tn.allowURL(tenorMedia) || tn.allowURL(giphyMedia) {
		t.Error("a Tenor proxy must allow only Tenor media")
	}
}

// TestGifConfigMatrix pins the whole configuration surface, including the
// promise that an existing Tenor deployment keeps running on upgrade. Written
// against resolveGifConfig with an injected getenv so it is a pure table: no
// process environment, no network, no ordering between cases.
func TestGifConfigMatrix(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		provider string
		key      string
		host     string
		filter   string
		legacy   bool
		wantErr  bool
	}{{
		name:     "nothing set at all",
		env:      map[string]string{},
		provider: "Giphy", key: "", host: "api.giphy.com", filter: "g",
	}, {
		name:     "the new vars, provider defaulted",
		env:      map[string]string{"CONCORD_GIF_KEY": "k"},
		provider: "Giphy", key: "k", host: "api.giphy.com", filter: "g",
	}, {
		// The upgrade promise: a node running on the old variables keeps
		// running, as Tenor, rather than silently switching vendor or refusing
		// to start. It is told to migrate; it is not broken.
		name:     "legacy Tenor deployment on upgrade",
		env:      map[string]string{"CONCORD_TENOR_KEY": "old", "CONCORD_TENOR_CONTENTFILTER": "medium"},
		provider: "Tenor", key: "old", host: "tenor.googleapis.com", filter: "medium", legacy: true,
	}, {
		name: "legacy base honoured for the legacy provider",
		env: map[string]string{
			"CONCORD_TENOR_KEY":  "old",
			"CONCORD_TENOR_BASE": "http://mirror.local:8080",
		},
		provider: "Tenor", key: "old", host: "mirror.local:8080", filter: "high", legacy: true,
	}, {
		// New vars win, and once CONCORD_GIF_KEY exists the legacy key is not
		// consulted at all — so migrating does not require deleting the old one
		// in the same deploy.
		name: "new vars win over legacy",
		env: map[string]string{
			"CONCORD_GIF_KEY":   "new",
			"CONCORD_TENOR_KEY": "old",
		},
		provider: "Giphy", key: "new", host: "api.giphy.com", filter: "g",
	}, {
		name: "explicit tenor with new vars is not legacy",
		env: map[string]string{
			"CONCORD_GIF_PROVIDER": "tenor",
			"CONCORD_GIF_KEY":      "new",
			"CONCORD_GIF_BASE":     "https://mirror.example",
		},
		provider: "Tenor", key: "new", host: "mirror.example", filter: "high",
	}, {
		// Selecting tenor without a CONCORD_GIF_KEY still picks up the old key:
		// half-migrated is a real state and it should work.
		name: "explicit tenor falls back to the legacy key",
		env: map[string]string{
			"CONCORD_GIF_PROVIDER": "tenor",
			"CONCORD_TENOR_KEY":    "old",
		},
		provider: "Tenor", key: "old", host: "tenor.googleapis.com", filter: "high", legacy: true,
	}, {
		// Giphy never reads the Tenor variables: a key for a different vendor is
		// not a key, and inheriting one would send the wrong secret upstream.
		name: "giphy ignores the legacy vars entirely",
		env: map[string]string{
			"CONCORD_GIF_PROVIDER": "giphy",
			"CONCORD_TENOR_KEY":    "old",
			"CONCORD_TENOR_BASE":   "http://mirror.local:8080",
		},
		provider: "Giphy", key: "", host: "api.giphy.com", filter: "g",
	}, {
		name:     "tenor-shaped filter words map onto Giphy's ratings",
		env:      map[string]string{"CONCORD_GIF_KEY": "k", "CONCORD_GIF_CONTENTFILTER": "medium"},
		provider: "Giphy", key: "k", host: "api.giphy.com", filter: "pg",
	}, {
		name:     "Giphy's own rating names pass through",
		env:      map[string]string{"CONCORD_GIF_KEY": "k", "CONCORD_GIF_CONTENTFILTER": "pg-13"},
		provider: "Giphy", key: "k", host: "api.giphy.com", filter: "pg-13",
	}, {
		name:    "unknown provider is an error, not a silent default",
		env:     map[string]string{"CONCORD_GIF_PROVIDER": "tenorr"},
		wantErr: true,
	}, {
		name:    "nonsense filter is an error",
		env:     map[string]string{"CONCORD_GIF_KEY": "k", "CONCORD_GIF_CONTENTFILTER": "banana"},
		wantErr: true,
	}, {
		name:    "nonsense base is an error",
		env:     map[string]string{"CONCORD_GIF_KEY": "k", "CONCORD_GIF_BASE": "not a url"},
		wantErr: true,
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := resolveGifConfig(func(k string) string { return c.env[k] })
			if c.wantErr {
				if err == nil {
					t.Fatalf("want an error, got provider %s host %s", cfg.provider.name(), cfg.base.Host)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveGifConfig: %v", err)
			}
			if got := cfg.provider.name(); got != c.provider {
				t.Errorf("provider = %q, want %q", got, c.provider)
			}
			if cfg.key != c.key {
				t.Errorf("key = %q, want %q", cfg.key, c.key)
			}
			if cfg.base.Host != c.host {
				t.Errorf("host = %q, want %q", cfg.base.Host, c.host)
			}
			if cfg.filter != c.filter {
				t.Errorf("filter = %q, want %q", cfg.filter, c.filter)
			}
			if cfg.legacyVars != c.legacy {
				t.Errorf("legacyVars = %v, want %v", cfg.legacyVars, c.legacy)
			}
		})
	}
}

// TestGiphyCursorBounds: the cursor comes back from a peer, so a mangled one
// must degrade to "page one" rather than reaching the upstream as garbage or
// walking past Giphy's offset window.
func TestGiphyCursorBounds(t *testing.T) {
	cases := map[string]int{
		"":                    0,
		"0":                   0,
		"40":                  40,
		"-1":                  0,
		"abc":                 0,
		"9999999999999999999": 0, // longer than any real offset
		"999999":              giphyMaxWindow,
	}
	for in, want := range cases {
		if got := giphyOffset(in); got != want {
			t.Errorf("giphyOffset(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestGifProxyRefusesNonImages: the proxy relays pictures. An address that
// answers with HTML is not relayed, whatever it was signed as.
func TestGifProxyRefusesNonImages(t *testing.T) {
	srv := fakeGiphy(t, []byte("GIF89a"))
	p := newTestProxy(t, "giphy", "test-key", srv.URL)
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
	srv := fakeGiphy(t, []byte("GIF89a"))
	p := newTestProxy(t, "giphy", "test-key", srv.URL)
	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "media", Ref: p.mint(srv.URL + "/huge.gif")})
	if resp.Status != cnet.GifStatusUpstream {
		t.Fatalf("status = %q, want the oversized body refused", resp.Status)
	}
	if len(resp.Media) != 0 {
		t.Fatalf("relayed %d bytes of an over-cap image", len(resp.Media))
	}
}

// TestGifProxySkipsOversizedRenditions: a rendition the vendor itself says is
// over the ceiling is never minted into a handle, so a peer cannot be handed a
// result that is guaranteed to fail when clicked.
func TestGifProxySkipsOversizedRenditions(t *testing.T) {
	body := fmt.Sprintf(`{"data":[
	  {"id":"toobig","images":{
	    "fixed_width_small":{"url":"https://media.giphy.com/t.gif","width":"1","height":"1","size":"10"},
	    "downsized":{"url":"https://media.giphy.com/f.gif","width":"2","height":"2","size":"%d"},
	    "original":{"url":"https://media.giphy.com/o.gif","width":"3","height":"3","size":"%d"}}}],
	  "pagination":{"total_count":1,"count":1,"offset":0}}`,
		cnet.MaxGifMediaBytes+1, cnet.MaxGifMediaBytes+9)
	cands, _, err := (giphyProvider{}).decode([]byte(body), gifQuery{}, cnet.MaxGifMediaBytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("candidates = %+v, want the over-cap result dropped", cands)
	}
}

// TestGifProxyRateLimits: the scarce resource is the operator's API quota, so a
// peer that hammers search gets told to wait rather than spending it.
func TestGifProxyRateLimits(t *testing.T) {
	srv := fakeGiphy(t, []byte("GIF89a"))
	p := newTestProxy(t, "giphy", "test-key", srv.URL)

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

// TestGifProxyGlobalSearchCeiling: the per-peer bucket above is keyed on a
// libp2p peer id, and a peer id is a keypair — a millisecond of work. An
// attacker who never reuses one is handed a fresh full burst every request, so
// the per-peer limit is a limit they simply opt out of, and the operator's API
// quota is what pays. The ceiling that catches this is keyed on nothing at all.
func TestGifProxyGlobalSearchCeiling(t *testing.T) {
	srv := fakeGiphy(t, []byte("GIF89a"))
	p := newTestProxy(t, "giphy", "test-key", srv.URL)

	served, limited := 0, false
	for i := 0; i < int(gifGlobalSearchBurst)+40; i++ {
		// A never-repeated identity, which is exactly what the evasion costs.
		from := peer.ID("throwaway-" + strconv.Itoa(i))
		if ask(t, p, from, cnet.GifRequest{Op: "search", Query: "cat"}).Status == cnet.GifStatusRateLimited {
			limited = true
			break
		}
		served++
	}
	if !limited {
		t.Fatalf("%d searches from %d never-repeated identities were all served — "+
			"the limit is still keyed on peer id", served, served)
	}
	// The ceiling must not be so tight that it fires before the per-peer buckets
	// do; a guild all searching at once after someone drops a link is honest.
	if served < int(gifSearchBurst) {
		t.Fatalf("the global ceiling fired after only %d searches, inside a single "+
			"peer's own burst of %d", served, int(gifSearchBurst))
	}
	// It bites searches, which spend quota — not the rest. A client must still
	// be able to ask whether the feature exists, or it cannot tell a busy node
	// from one that was never configured.
	if got := ask(t, p, "somebody", cnet.GifRequest{Op: "status"}).Status; got != cnet.GifStatusOK {
		t.Fatalf("status op returned %q while the search ceiling was spent", got)
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
	srv := fakeGiphy(t, []byte("GIF89a"))
	p := newTestProxy(t, "giphy", "test-key", srv.URL)
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
	p := newTestProxy(t, "giphy", key, "http://127.0.0.1:1")
	resp := ask(t, p, "peer-a", cnet.GifRequest{Op: "search", Query: "cat"})
	if resp.Status != cnet.GifStatusUpstream {
		t.Fatalf("status = %q, want upstream", resp.Status)
	}
	if strings.Contains(resp.Detail, key) || strings.Contains(resp.Detail, "127.0.0.1") {
		t.Fatalf("reply leaked the request URL: %q", resp.Detail)
	}
}
