package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	cnet "github.com/ZahakJ/concord/internal/net"
)

// The GIF-search proxy.
//
// A member's client asks this node; this node asks the GIF vendor. Nobody's
// browser touches the vendor, so the vendor learns one IP — this node's — for
// the whole guild, and learns nothing about who searched for what. The operator,
// in exchange, sees the search terms. That is a real cost, and it is the trade
// the user asked for: they run this node themselves, and they already route
// their discovery, relaying and offline mail through it.
//
// WHY THE VENDOR IS AN INTERFACE. This file used to say "Tenor" everywhere. On
// 30 June 2026 Google decommissioned the public Tenor API: existing keys stopped
// returning data, signups closed, and every part of this feature died at once —
// including the "unavailable" message, which went on telling operators to go get
// a CONCORD_TENOR_KEY that could no longer be obtained. Hardcoding one vendor
// into the proxy was the mistake, not the choice of vendor. The upstream was
// already isolated behind exactly one boundary (this node), so the vendor is now
// a small interface: it knows a vendor's URL shape, JSON and pagination
// convention, and nothing else. Everything that makes this safe — the signed
// handles, the SSRF allowlist, the byte caps, the rate limits — stays outside
// it, in gifProxy, where a new provider cannot weaken it by accident.
//
// Giphy is the default because it is alive and its signups are open. The Tenor
// implementation is kept, and only kept: someone running a Tenor-compatible
// mirror of their own can still select it, but nothing defaults to it, because
// the public API it was written against no longer exists.
//
// This is the one part of the rendezvous that makes OUTBOUND requests to the
// open web on a peer's behalf, so it is written as if peers were hostile:
//
//   - a peer never supplies a URL. It supplies an opaque handle that this node
//     minted from an address the vendor itself returned, HMAC'd under a
//     per-process secret. Anything else is refused, so this cannot be turned
//     into an open proxy for scanning the operator's LAN or laundering traffic.
//   - the handle is checked against a host allowlist on the way back OUT as
//     well, and again on every redirect, so a signature alone is not authority.
//     The allowlist is the CONFIGURED provider's, never the union of all of
//     them: a node proxying for Giphy has no business fetching from tenor.com.
//   - responses are size-capped with a LimitReader (a Content-Length header is
//     a claim, not a fact) and the whole exchange is under a timeout.
//   - requests are token-bucketed per peer, because the expensive resource here
//     is the operator's API quota and bandwidth, not this node's CPU.
//
// With no API key configured the proxy answers "unavailable" and says so. That
// is a supported state, not a failure: most operators will never set a key, and
// the client is required to explain the difference — truthfully, which is why
// the reason string names the provider actually configured and what to set.

const (
	// gifSearchRate/Burst: searches cost the operator API quota, so they are the
	// tighter of the two. A burst covers someone retyping a query a few times.
	gifSearchRate  = 0.5 // sustained searches per second per peer
	gifSearchBurst = 10.0
	// gifMediaRate/Burst: one search result page is ~20 thumbnails, all fetched
	// at once, and then one full GIF when something is picked. The burst has to
	// cover a page without stalling; the sustained rate is what stops a peer
	// using this node as a general-purpose downloader.
	gifMediaRate  = 4.0
	gifMediaBurst = 60.0

	// The identity-INDEPENDENT ceiling, and the one that actually bounds the
	// operator's bill.
	//
	// The buckets above are keyed on libp2p peer id. A peer id is a keypair, and
	// a keypair costs a millisecond to generate: a script that makes a fresh
	// identity per search is handed a fresh full burst every time, so per-peer
	// limits alone bound nothing at all. There is no fix for that at the
	// identity layer — this node deliberately has no idea who is a member, since
	// guilds are end-to-end encrypted and it is in none of them.
	//
	// So the second dimension is keyed on nothing. It is the operator's API
	// quota, spent at one rate no matter how many identities spend it. The
	// sustained rate is the hourly ceiling; the burst lets a whole guild search
	// at once after someone drops a link, which is the busiest honest minute
	// this node ever has.
	gifGlobalSearchRate  = 600.0 / 3600 // 600 searches/hour across ALL peers
	gifGlobalSearchBurst = 120.0

	// gifUpstreamTimeout bounds one outbound HTTPS exchange. It must leave room
	// inside the peer's stream deadline (30s) for the reply to be written back.
	gifUpstreamTimeout = 12 * time.Second

	// gifMaxQueryRunes caps the search terms. Every vendor's own limit is far
	// higher; this is here so a peer cannot push a megabyte of text through the
	// operator's API account.
	gifMaxQueryRunes = 100
	gifMaxResults    = 30

	// gifMaxCursor bounds a pagination cursor. It is echoed back from a previous
	// reply of ours, but it still arrives from a peer.
	gifMaxCursor = 64
)

// tenorGone is the one sentence every Tenor-related message carries, so the
// operator hears the same fact from the startup log, the peer-facing reason
// string and the comments. Kept as a constant precisely because a half-updated
// version of it is how the old lie survived.
const tenorGone = "the public Tenor API was decommissioned on 30 June 2026"

// gifQuery is one search, already sanitized, as handed to a provider.
type gifQuery struct {
	Terms    string // cleanQuery'd search terms, never empty
	Limit    int    // clamped to gifMaxResults
	Pos      string // cursor from a previous reply of ours, length-bounded
	Filter   string // the provider's own safety level, from contentFilter
	Key      string // the operator's API key
	ClientID string // an app-level analytics bucket, if the vendor has one
}

// gifCandidate is one upstream result BEFORE it is made safe for a peer: it
// still carries real URLs, which is why it never leaves this file. gifProxy
// re-checks both addresses against the allowlist and replaces them with signed
// handles before anything goes down the wire.
type gifCandidate struct {
	ID            string
	Title         string
	Preview       string // URL of the small thumbnail
	Full          string // URL of the full-size image
	Width, Height int    // of Full; a layout hint only
}

// gifProvider is one upstream GIF vendor.
//
// Implementations are deliberately dumb: a URL shape, a JSON schema, a cursor
// convention, and a list of hostnames their media lives on. They hold no
// credentials, do no I/O, and enforce no policy — the proxy does that, so that
// adding a vendor cannot loosen the SSRF gate or the caps.
type gifProvider interface {
	// name is shown to peers as provenance ("Results from Giphy…").
	name() string
	// apiBase is the vendor's public API root, used when the operator sets none.
	apiBase() string
	// mediaHosts lists the hostnames media may be fetched from. Each matches
	// exactly or as a parent domain, https only. This is the ONE security input
	// a provider supplies, and it is per provider on purpose.
	mediaHosts() []string
	// contentFilter validates a safety level and returns the vendor's own value
	// for it. The empty string must map to the STRICTEST setting the vendor
	// offers: an operator running a proxy for their friends is not signing up to
	// moderate what it returns, and the permissive default is the surprising one.
	contentFilter(level string) (string, error)
	// searchURL builds one search request against base.
	searchURL(base *url.URL, q gifQuery) *url.URL
	// decode maps a vendor response into candidates plus the cursor for the next
	// page ("" when exhausted). Formats the vendor already says are larger than
	// maxBytes must be skipped: a handle that is guaranteed to fail when clicked
	// is worse than one fewer result.
	decode(body []byte, q gifQuery, maxBytes int64) ([]gifCandidate, string, error)
}

// ---- Giphy (the default) ----

// giphyProvider speaks Giphy's v1 API. Chosen as the default in July 2026
// because it is the surviving public GIF API of the two: it answers, it returns
// a proper 401 for a bad key rather than a vague 400, and signups are open at
// developers.giphy.com.
type giphyProvider struct{}

func (giphyProvider) name() string    { return "Giphy" }
func (giphyProvider) apiBase() string { return "https://api.giphy.com" }

// Giphy serves its media from media.giphy.com, media0-4.giphy.com and
// i.giphy.com. All of them are under giphy.com, so one entry covers the CDN
// without enumerating shards that come and go.
func (giphyProvider) mediaHosts() []string { return []string{"giphy.com"} }

// Giphy calls this "rating" and spells the levels y/g/pg/pg-13/r. The
// Tenor-shaped words are accepted as aliases so an operator migrating off the
// legacy CONCORD_TENOR_CONTENTFILTER does not have to learn a second vocabulary
// in the same upgrade that already changed their variable names.
func (giphyProvider) contentFilter(level string) (string, error) {
	switch level {
	case "":
		return "g", nil // strictest useful default
	case "y", "g", "pg", "pg-13", "r":
		return level, nil
	case "high":
		return "g", nil
	case "medium":
		return "pg", nil
	case "low":
		return "pg-13", nil
	case "off":
		return "r", nil
	default:
		return "", fmt.Errorf("for provider giphy the content filter must be y|g|pg|pg-13|r (or high|medium|low|off), got %q", level)
	}
}

// giphyMaxWindow is Giphy's documented ceiling on offset+limit. Asking past it
// gets a 4xx, so the cursor stops there rather than handing the peer a "More
// results" button that always fails.
const giphyMaxWindow = 4999

func (giphyProvider) searchURL(base *url.URL, q gifQuery) *url.URL {
	u := *base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/gifs/search"
	qs := url.Values{
		"api_key": {q.Key},
		"q":       {q.Terms},
		"limit":   {strconv.Itoa(q.Limit)},
		"rating":  {q.Filter},
		"lang":    {"en"},
	}
	if off := giphyOffset(q.Pos); off > 0 {
		qs.Set("offset", strconv.Itoa(off))
	}
	u.RawQuery = qs.Encode()
	return &u
}

// giphyOffset parses Giphy's cursor, which is a plain result offset. Anything
// that is not a small non-negative integer is treated as "start from the top":
// the cursor came back from a peer, and refusing the whole search over a
// mangled one would be unhelpful.
func giphyOffset(pos string) int {
	if pos == "" || len(pos) > 9 {
		return 0
	}
	n, err := strconv.Atoi(pos)
	if err != nil || n < 0 {
		return 0
	}
	if n > giphyMaxWindow {
		return giphyMaxWindow
	}
	return n
}

// giphySearch is the slice of Giphy's response this proxy uses. Everything else
// in their JSON is ignored on purpose: the less of a third party's schema that
// reaches a peer, the less there is to go wrong.
//
// Note the string-typed numbers. Giphy really does serialize width, height and
// size as JSON strings; decoding them as ints fails on the real API, which is
// the sort of thing only a fixture built from a real response catches.
type giphySearch struct {
	Data []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		AltText string `json:"alt_text"`
		Images  map[string]struct {
			URL    string `json:"url"`
			Width  string `json:"width"`
			Height string `json:"height"`
			Size   string `json:"size"`
		} `json:"images"`
	} `json:"data"`
	Pagination struct {
		TotalCount int `json:"total_count"`
		Count      int `json:"count"`
		Offset     int `json:"offset"`
	} `json:"pagination"`
}

func (giphyProvider) decode(body []byte, q gifQuery, maxBytes int64) ([]gifCandidate, string, error) {
	var gs giphySearch
	if err := json.Unmarshal(body, &gs); err != nil {
		return nil, "", err
	}
	out := make([]gifCandidate, 0, len(gs.Data))
	for _, d := range gs.Data {
		pick := func(names ...string) (string, int, int, bool) {
			for _, n := range names {
				im, ok := d.Images[n]
				if !ok || im.URL == "" {
					continue
				}
				if sz, err := strconv.ParseInt(im.Size, 10, 64); err == nil && sz > maxBytes {
					continue
				}
				w, _ := strconv.Atoi(im.Width)
				h, _ := strconv.Atoi(im.Height)
				return im.URL, w, h, true
			}
			return "", 0, 0, false
		}
		// Thumbnail smallest-first; full biggest-that-fits-first. "downsized" and
		// friends are Giphy's own size-bounded renditions, so preferring them
		// keeps the common case inside our relay ceiling.
		prev, _, _, okP := pick("fixed_width_small", "preview_gif", "fixed_width_downsampled", "fixed_width", "downsized")
		full, w, h, okF := pick("downsized_medium", "downsized", "fixed_width", "original")
		if !okP || !okF {
			continue
		}
		title := d.AltText
		if title == "" {
			title = d.Title
		}
		out = append(out, gifCandidate{ID: d.ID, Title: title, Preview: prev, Full: full, Width: w, Height: h})
	}

	// Giphy has no opaque cursor: the next page is the next offset. Emitted only
	// when there is reason to believe another page exists, so the UI's "More
	// results" button is not offered into a guaranteed empty reply.
	next := ""
	if n := len(gs.Data); n > 0 {
		at := giphyOffset(q.Pos) + n
		if gs.Pagination.Offset > 0 || gs.Pagination.Count > 0 {
			at = gs.Pagination.Offset + gs.Pagination.Count
		}
		if at < giphyMaxWindow && (gs.Pagination.TotalCount == 0 || at < gs.Pagination.TotalCount) {
			next = strconv.Itoa(at)
		}
	}
	return out, next, nil
}

// ---- Tenor (kept, not default) ----

// tenorProvider speaks Tenor's v2 API. KEPT DELIBERATELY, DEFAULT DELIBERATELY
// NOT: as of 30 June 2026 tenor.googleapis.com is decommissioned, so this is
// useful only against a Tenor-compatible mirror the operator points
// CONCORD_GIF_BASE at. Selecting it prints that fact at startup and says it in
// the reason string peers see, so nobody spends an afternoon debugging a
// service that no longer exists.
type tenorProvider struct{}

func (tenorProvider) name() string         { return "Tenor" }
func (tenorProvider) apiBase() string      { return "https://tenor.googleapis.com" }
func (tenorProvider) mediaHosts() []string { return []string{"tenor.com"} }

func (tenorProvider) contentFilter(level string) (string, error) {
	switch level {
	case "off", "low", "medium", "high":
		return level, nil
	case "":
		return "high", nil // strictest
	default:
		return "", fmt.Errorf("for provider tenor the content filter must be off|low|medium|high, got %q", level)
	}
}

func (tenorProvider) searchURL(base *url.URL, q gifQuery) *url.URL {
	u := *base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v2/search"
	qs := url.Values{
		"q":             {q.Terms},
		"key":           {q.Key},
		"limit":         {strconv.Itoa(q.Limit)},
		"media_filter":  {"tinygif,mediumgif,gif"},
		"contentfilter": {q.Filter},
	}
	if q.ClientID != "" {
		qs.Set("client_key", q.ClientID)
	}
	if q.Pos != "" {
		qs.Set("pos", q.Pos)
	}
	u.RawQuery = qs.Encode()
	return &u
}

// tenorSearch is the slice of Tenor's v2 response this proxy uses.
type tenorSearch struct {
	Results []struct {
		ID                 string `json:"id"`
		Title              string `json:"title"`
		ContentDescription string `json:"content_description"`
		MediaFormats       map[string]struct {
			URL  string `json:"url"`
			Dims []int  `json:"dims"`
			Size int64  `json:"size"`
		} `json:"media_formats"`
	} `json:"results"`
	Next string `json:"next"`
}

func (tenorProvider) decode(body []byte, _ gifQuery, maxBytes int64) ([]gifCandidate, string, error) {
	var ts tenorSearch
	if err := json.Unmarshal(body, &ts); err != nil {
		return nil, "", err
	}
	out := make([]gifCandidate, 0, len(ts.Results))
	for _, r := range ts.Results {
		pick := func(names ...string) (string, int, int, bool) {
			for _, n := range names {
				f, ok := r.MediaFormats[n]
				if !ok || f.URL == "" {
					continue
				}
				if f.Size > maxBytes {
					continue
				}
				w, h := 0, 0
				if len(f.Dims) == 2 {
					w, h = f.Dims[0], f.Dims[1]
				}
				return f.URL, w, h, true
			}
			return "", 0, 0, false
		}
		prev, _, _, okP := pick("tinygif", "mediumgif", "gif")
		full, w, h, okF := pick("mediumgif", "gif", "tinygif")
		if !okP || !okF {
			continue
		}
		title := r.ContentDescription
		if title == "" {
			title = r.Title
		}
		out = append(out, gifCandidate{ID: r.ID, Title: title, Preview: prev, Full: full, Width: w, Height: h})
	}
	// Tenor's cursor is opaque; it is handed straight back next time.
	return out, ts.Next, nil
}

// isTenorProvider reports whether the configured vendor is Tenor. Used only to
// decide whether to say out loud that the public API behind it is gone; a type
// assertion rather than a name comparison so a renamed label cannot silence it.
func isTenorProvider(p gifProvider) bool {
	_, ok := p.(tenorProvider)
	return ok
}

// gifProviderByName resolves CONCORD_GIF_PROVIDER. Unknown names are an error
// rather than a silent fallback: an operator who misspells their provider should
// be told, not quietly proxied somewhere they did not choose.
func gifProviderByName(name string) (gifProvider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "giphy":
		return giphyProvider{}, nil
	case "tenor":
		return tenorProvider{}, nil
	default:
		return nil, fmt.Errorf("CONCORD_GIF_PROVIDER must be giphy|tenor, got %q", name)
	}
}

// ---- the proxy ----

// gifProxy is the node's GIF-search service. A zero key means "not configured",
// which is answered honestly rather than treated as an error.
type gifProxy struct {
	provider gifProvider
	key      string   // API key, from CONCORD_GIF_KEY (or legacy CONCORD_TENOR_KEY)
	base     *url.URL // API base, from CONCORD_GIF_BASE or the provider's default
	filter   string   // the provider's own safety level
	client   *http.Client
	secret   []byte // per-process HMAC key for media handles
	clientID string // app-level analytics bucket, for vendors that have one

	// legacyVars: this deployment was configured entirely through the old
	// CONCORD_TENOR_* variables. It keeps working — an upgrade must not break a
	// running node — but the operator is told to migrate, because the service
	// those variables point at is gone.
	legacyVars bool

	mu      sync.Mutex
	buckets map[peer.ID]*gifBuckets
	// globalSearch is the whole node's search allowance, drawn on by every peer
	// together. See gifGlobalSearchRate.
	globalSearch gifBucket
}

type gifBuckets struct {
	search, media gifBucket
}

type gifBucket struct {
	tokens float64
	last   time.Time
}

// take consumes one token, refilling first; reports whether the caller may go.
func (b *gifBucket) take(rate, burst float64) bool {
	now := time.Now()
	if b.last.IsZero() {
		b.tokens, b.last = burst, now
	}
	b.tokens += now.Sub(b.last).Seconds() * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// serveGifSearch wires the proxy onto the host. It always registers the
// handler, key or no key: a client must be able to tell "this node has no key"
// apart from "this node is too old to know what GIF search is", and the only
// way to say the former is to answer.
func serveGifSearch(ctx context.Context, h host.Host) {
	p, err := newGifProxy()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gif search disabled:", err)
		// Still answerable, so peers get "unavailable" with a reason rather than
		// a stream that opens and says nothing. Giphy stands in as the named
		// provider because it is the default the operator would be fixing towards.
		p = &gifProxy{provider: giphyProvider{}, buckets: map[peer.ID]*gifBuckets{}}
	}
	cnet.ServeGifSearch(ctx, h, p.handle)

	// Startup output names the provider AND the host it will actually reach, so
	// "GIF search is on" can never mean a different upstream than the operator
	// thinks. The Tenor notes are printed because a silent misconfiguration
	// against a dead API is exactly what this rewrite exists to prevent.
	if p.legacyVars {
		fmt.Printf("GIF search proxy: configured from the legacy CONCORD_TENOR_* variables. %s — "+
			"migrate to CONCORD_GIF_PROVIDER=giphy with CONCORD_GIF_KEY (developers.giphy.com).\n",
			strings.ToUpper(tenorGone[:1])+tenorGone[1:])
	}
	if p.key == "" {
		fmt.Printf("GIF search proxy: no API key set — peers are told it is unavailable. "+
			"Set CONCORD_GIF_KEY (provider %s, default giphy) to turn it on.\n", p.provider.name())
		return
	}
	fmt.Printf("GIF search proxy enabled: %s via %s.\n", p.provider.name(), p.base.Host)
	if isTenorProvider(p.provider) {
		// Two different pieces of bad news, so two different sentences. Pointing
		// at the dead public host is a node that WILL NOT work; pointing at a
		// mirror is a node that will, as long as the mirror stays up.
		if def, err := url.Parse(p.provider.apiBase()); err == nil && strings.EqualFold(p.base.Host, def.Host) {
			fmt.Printf("  WARNING: %s. %s no longer answers, so searches from this node will fail — "+
				"set CONCORD_GIF_PROVIDER=giphy with CONCORD_GIF_KEY, or point CONCORD_GIF_BASE at a Tenor-compatible mirror.\n",
				tenorGone, p.base.Host)
		} else {
			fmt.Printf("  NOTE: %s; this works only for as long as the mirror at %s does.\n", tenorGone, p.base.Host)
		}
	}

	// Idle peers' buckets are dropped so the map cannot grow forever as
	// distinct peers come and go.
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.pruneBuckets()
			}
		}
	}()
}

// gifConfig is the resolved answer to "which vendor, which key, which host".
type gifConfig struct {
	provider   gifProvider
	key        string
	base       *url.URL
	filter     string
	legacyVars bool
}

// resolveGifConfig reads the environment. It takes a getenv function rather than
// calling os.Getenv directly so the compatibility matrix can be tested as a
// table, without a process environment and without a real API anywhere near it.
//
// The rule, in order of precedence:
//
//	CONCORD_GIF_PROVIDER    giphy | tenor. Default giphy — UNLESS the only key
//	                        present is the legacy CONCORD_TENOR_KEY, in which
//	                        case a working deployment is left as it was.
//	CONCORD_GIF_KEY         falls back to CONCORD_TENOR_KEY when the provider is
//	                        tenor, so an existing node keeps running on upgrade.
//	CONCORD_GIF_BASE        falls back to CONCORD_TENOR_BASE likewise, then to
//	                        the provider's own public base.
//	CONCORD_GIF_CONTENTFILTER  same fallback, then the provider's strictest.
func resolveGifConfig(getenv func(string) string) (gifConfig, error) {
	var cfg gifConfig

	name := strings.TrimSpace(getenv("CONCORD_GIF_PROVIDER"))
	legacyKey := getenv("CONCORD_TENOR_KEY")
	newKey := getenv("CONCORD_GIF_KEY")
	if name == "" {
		// The one place the dead vendor is still chosen automatically: an
		// operator whose node was working yesterday must not wake up to a node
		// that refuses to start. They get a migration notice, not a breakage.
		if newKey == "" && legacyKey != "" {
			name = "tenor"
		} else {
			name = "giphy"
		}
	}
	prov, err := gifProviderByName(name)
	if err != nil {
		return cfg, err
	}
	cfg.provider = prov
	_, isTenor := prov.(tenorProvider)

	cfg.key = newKey
	if cfg.key == "" && isTenor && legacyKey != "" {
		cfg.key = legacyKey
		cfg.legacyVars = true
	}

	baseRaw := getenv("CONCORD_GIF_BASE")
	baseVar := "CONCORD_GIF_BASE"
	if baseRaw == "" && isTenor {
		if v := getenv("CONCORD_TENOR_BASE"); v != "" {
			baseRaw, baseVar = v, "CONCORD_TENOR_BASE"
		}
	}
	if baseRaw == "" {
		baseRaw = prov.apiBase()
	}
	u, err := url.Parse(baseRaw)
	if err != nil || u.Host == "" {
		return cfg, fmt.Errorf("%s %q is not a URL", baseVar, baseRaw)
	}
	cfg.base = u

	level := getenv("CONCORD_GIF_CONTENTFILTER")
	if level == "" && isTenor {
		level = getenv("CONCORD_TENOR_CONTENTFILTER")
	}
	if cfg.filter, err = prov.contentFilter(level); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func newGifProxy() (*gifProxy, error) {
	cfg, err := resolveGifConfig(os.Getenv)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	p := &gifProxy{
		provider:   cfg.provider,
		key:        cfg.key,
		base:       cfg.base,
		filter:     cfg.filter,
		legacyVars: cfg.legacyVars,
		secret:     secret,
		clientID:   "concord",
		buckets:    map[peer.ID]*gifBuckets{},
	}
	p.client = &http.Client{
		Timeout: gifUpstreamTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// A signature on the original URL says nothing about where a
			// redirect points. Re-check every hop, and keep the chain short.
			if len(via) >= 4 {
				return fmt.Errorf("too many redirects")
			}
			if !p.allowURL(req.URL) {
				return fmt.Errorf("redirect to disallowed host %q", req.URL.Host)
			}
			return nil
		},
	}
	return p, nil
}

func (p *gifProxy) pruneBuckets() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for id, b := range p.buckets {
		if now.Sub(b.search.last) > time.Hour && now.Sub(b.media.last) > time.Hour {
			delete(p.buckets, id)
		}
	}
}

func (p *gifProxy) allow(id peer.ID, media bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.buckets[id]
	if b == nil {
		b = &gifBuckets{}
		p.buckets[id] = b
	}
	if media {
		// No global ceiling on media: a media handle can only be obtained from a
		// search reply this node minted, so the search ceiling below already
		// bounds how many of them can exist.
		return b.media.take(gifMediaRate, gifMediaBurst)
	}
	if !b.search.take(gifSearchRate, gifSearchBurst) {
		return false
	}
	// Drawn only AFTER the per-peer bucket agreed, so one loud peer cannot spend
	// the node's whole allowance on requests it was going to be refused anyway.
	return p.globalSearch.take(gifGlobalSearchRate, gifGlobalSearchBurst)
}

// allowURL is the SSRF gate: the only addresses this node will ever fetch.
//
// The operator's own API base is allowed whatever its scheme, because they
// chose it (a self-hosted mirror on the same box, or a test fixture on
// loopback). Everything else must be https and under one of the CONFIGURED
// provider's media hosts. Per provider, not a union of all of them: a node
// proxying Giphy that would also fetch tenor.com is a node with a wider attack
// surface than its operator asked for, and a signed handle from an earlier
// configuration must stop being redeemable when the provider changes.
//
// Note what this does NOT defend against: if the vendor's own DNS were made to
// answer with a private address, this check would still pass. Pinning
// resolution would be the fix, and it is not worth the machinery here — an
// attacker who controls giphy.com's DNS has far better targets than one hobby
// node's LAN.
func (p *gifProxy) allowURL(u *url.URL) bool {
	if u == nil || u.Host == "" {
		return false
	}
	if p.base != nil && strings.EqualFold(u.Host, p.base.Host) {
		return true
	}
	if u.Scheme != "https" || p.provider == nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, allowed := range p.provider.mediaHosts() {
		if h == allowed || strings.HasSuffix(h, "."+allowed) {
			return true
		}
	}
	return false
}

// mint turns a URL the vendor gave us into a handle safe to hand a peer. The
// peer gets the address back verbatim — it is not a secret, and pretending
// otherwise would be theatre — but cannot change so much as one byte of it
// without the MAC failing.
func (p *gifProxy) mint(raw string) string {
	mac := hmac.New(sha256.New, p.secret)
	mac.Write([]byte(raw))
	return base64.RawURLEncoding.EncodeToString([]byte(raw)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// redeem verifies a handle and returns the URL inside it.
func (p *gifProxy) redeem(handle string) (*url.URL, bool) {
	rawB64, sigB64, ok := strings.Cut(handle, ".")
	if !ok {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(rawB64)
	if err != nil {
		return nil, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, p.secret)
	mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, false
	}
	u, err := url.Parse(string(raw))
	if err != nil {
		return nil, false
	}
	// Checked again on redemption, not just at mint time: the allowlist is the
	// invariant, and a signature only proves this node wrote the string.
	if !p.allowURL(u) {
		return nil, false
	}
	return u, true
}

func gifReply(r cnet.GifResponse) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// unavailableDetail is the sentence a peer's UI shows when this node has no key.
// It has to be TRUE and it has to be actionable, which is the whole reason this
// file was rewritten: the old one said "set CONCORD_TENOR_KEY", advice that
// became impossible to follow the day Google closed Tenor's signups.
func (p *gifProxy) unavailableDetail() string {
	if isTenorProvider(p.provider) {
		return "this rendezvous is set to the Tenor provider, but " + tenorGone +
			" — whoever runs it should set CONCORD_GIF_PROVIDER=giphy and CONCORD_GIF_KEY"
	}
	return "this rendezvous has no GIF API key configured — whoever runs it can set CONCORD_GIF_KEY"
}

// providerName is nil-safe, because the disabled proxy built on a config error
// still has to answer.
func (p *gifProxy) providerName() string {
	if p.provider == nil {
		return ""
	}
	return p.provider.name()
}

// handle answers one request from a peer.
//
// There is no membership gate here, unlike the release protocol. Anyone who can
// open a stream to this node can search, because the node has no way to know
// which guilds exist — they are end-to-end encrypted and it is not in any of
// them. The rate limiter is what bounds the damage; an operator who wants a
// private proxy runs a rendezvous that strangers do not know the address of,
// which is already how the rest of this node is protected.
func (p *gifProxy) handle(ctx context.Context, from peer.ID, request []byte) ([]byte, error) {
	var req cnet.GifRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return gifReply(cnet.GifResponse{Status: cnet.GifStatusBadRequest, Detail: "unreadable request"})
	}
	if p.key == "" {
		// Source is set even here: the picker names the provider in its
		// provenance line, and "Tenor" hardcoded in a frontend is how the last
		// lie got shipped.
		return gifReply(cnet.GifResponse{
			Status: cnet.GifStatusUnavailable,
			Detail: p.unavailableDetail(),
			Source: p.providerName(),
		})
	}
	// Only a real search spends API quota, so only a real search draws on the
	// tight bucket; media fetches and status probes share the loose one.
	if !p.allow(from, req.Op != "search") {
		return gifReply(cnet.GifResponse{
			Status: cnet.GifStatusRateLimited,
			Detail: "too many GIF requests — wait a moment",
		})
	}

	switch req.Op {
	case "status":
		// Reached only when a key IS configured (the check above returns
		// unavailable otherwise), so this is the client's way to find out
		// whether the Search tab is usable before the user types anything.
		return gifReply(cnet.GifResponse{Status: cnet.GifStatusOK, Source: p.providerName()})
	case "search":
		return gifReply(p.search(ctx, req))
	case "media":
		return gifReply(p.media(ctx, req))
	default:
		return gifReply(cnet.GifResponse{Status: cnet.GifStatusBadRequest, Detail: "unknown operation"})
	}
}

// cleanQuery bounds and sanitizes the search terms. Control characters are
// dropped rather than rejected: they cannot appear in anything a person typed
// into the box, so their presence means a mangled client, not a hostile one,
// and refusing the whole search would be unhelpful.
func cleanQuery(q string) string {
	q = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, q)
	q = strings.TrimSpace(q)
	rs := []rune(q)
	if len(rs) > gifMaxQueryRunes {
		q = string(rs[:gifMaxQueryRunes])
	}
	return q
}

func (p *gifProxy) search(ctx context.Context, req cnet.GifRequest) cnet.GifResponse {
	q := cleanQuery(req.Query)
	if q == "" {
		return cnet.GifResponse{Status: cnet.GifStatusBadRequest, Detail: "empty search"}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > gifMaxResults {
		limit = gifMaxResults
	}
	pos := req.Pos
	if len(pos) > gifMaxCursor {
		// Over-long cursors are dropped rather than refused: the peer gets page
		// one, which is a worse answer than they wanted but still an answer.
		pos = ""
	}

	gq := gifQuery{Terms: q, Limit: limit, Pos: pos, Filter: p.filter, Key: p.key, ClientID: p.clientID}
	u := p.provider.searchURL(p.base, gq)

	body, _, err := p.fetch(ctx, u, 1<<20)
	if err != nil {
		// Deliberately not err.Error(): that string can contain the API key
		// (net/http puts the full URL in its errors) and the peer must never
		// see it. The provider name is safe and useful, so it is named instead.
		return cnet.GifResponse{
			Status: cnet.GifStatusUpstream,
			Detail: "the GIF API did not answer",
			Source: p.providerName(),
		}
	}
	cands, next, err := p.provider.decode(body, gq, cnet.MaxGifMediaBytes)
	if err != nil {
		return cnet.GifResponse{
			Status: cnet.GifStatusUpstream,
			Detail: "the GIF API sent something unreadable",
			Source: p.providerName(),
		}
	}

	out := cnet.GifResponse{Status: cnet.GifStatusOK, Source: p.providerName(), Next: next, Results: []cnet.GifHit{}}
	for _, c := range cands {
		// The allowlist is applied HERE, outside the provider, over every address
		// it proposed. A provider that returned an off-allowlist URL — because the
		// vendor is compromised, or because a mirror is misconfigured — gets its
		// result dropped, never minted into a handle.
		pu, err1 := url.Parse(c.Preview)
		fu, err2 := url.Parse(c.Full)
		if err1 != nil || err2 != nil || !p.allowURL(pu) || !p.allowURL(fu) {
			continue
		}
		out.Results = append(out.Results, cnet.GifHit{
			ID:      c.ID,
			Title:   cleanQuery(c.Title),
			Preview: p.mint(c.Preview),
			Full:    p.mint(c.Full),
			Width:   c.Width, Height: c.Height,
		})
	}
	return out
}

func (p *gifProxy) media(ctx context.Context, req cnet.GifRequest) cnet.GifResponse {
	u, ok := p.redeem(req.Ref)
	if !ok {
		// Almost always a handle from before this node restarted: the signing
		// secret is generated per process on purpose (nothing to persist, and
		// nothing to leak), so old handles stop verifying. The client's cure is
		// to search again, which is what this status tells it.
		//
		// A forged handle and an off-allowlist URL land here too, deliberately
		// sharing the innocent answer: whoever is probing learns only "no",
		// never which of the checks they tripped.
		return cnet.GifResponse{Status: cnet.GifStatusExpired, Detail: "that result is stale — search again"}
	}
	body, ctype, err := p.fetch(ctx, u, cnet.MaxGifMediaBytes)
	if err != nil {
		return cnet.GifResponse{Status: cnet.GifStatusUpstream, Detail: "could not fetch that image"}
	}
	sub, ok := gifSubtype(ctype)
	if !ok {
		// The proxy relays images. Anything else — HTML, a redirect page, a
		// zip — is not what a peer asked for and is not going down the wire.
		return cnet.GifResponse{Status: cnet.GifStatusUpstream, Detail: "that address did not return an image"}
	}
	return cnet.GifResponse{Status: cnet.GifStatusOK, Source: p.providerName(), Media: body, Subtype: sub}
}

// gifSubtype maps a Content-Type to the four image subtypes Concord's
// attachment format understands.
func gifSubtype(ctype string) (string, bool) {
	mt, _, _ := strings.Cut(ctype, ";")
	switch strings.ToLower(strings.TrimSpace(mt)) {
	case "image/gif":
		return "gif", true
	case "image/webp":
		return "webp", true
	case "image/png":
		return "png", true
	case "image/jpeg", "image/jpg":
		return "jpeg", true
	}
	return "", false
}

// fetch performs one outbound request, capped at max bytes.
func (p *gifProxy) fetch(ctx context.Context, u *url.URL, max int64) ([]byte, string, error) {
	if !p.allowURL(u) {
		return nil, "", fmt.Errorf("disallowed host")
	}
	ctx, cancel := context.WithTimeout(ctx, gifUpstreamTimeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	// A fresh request with headers WE choose, not the peer's. This is the other
	// half of the privacy property: the vendor sees one constant User-Agent and
	// this node's IP for every member of every guild, with nothing to tell them
	// apart. No cookies (this client has no jar), no Referer, no Accept-Language.
	r.Header.Set("User-Agent", "concord-rendezvous")
	r.Header.Set("Accept", "*/*")

	resp, err := p.client.Do(r)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	// max+1 so an over-cap body is detected rather than silently truncated into
	// a corrupt image. Content-Length is not consulted: it is a claim.
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > max {
		return nil, "", fmt.Errorf("upstream body exceeds %d bytes", max)
	}
	return body, resp.Header.Get("Content-Type"), nil
}
