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

	cnet "github.com/zahak/concord/internal/net"
)

// The GIF-search proxy.
//
// A member's client asks this node; this node asks Tenor. Nobody's browser
// touches Google, so Google learns one IP — this node's — for the whole guild,
// and learns nothing about who searched for what. The operator, in exchange,
// sees the search terms. That is a real cost, and it is the trade the user
// asked for: they run this node themselves, and they already route their
// discovery, relaying and offline mail through it.
//
// This is the one part of the rendezvous that makes OUTBOUND requests to the
// open web on a peer's behalf, so it is written as if peers were hostile:
//
//   - a peer never supplies a URL. It supplies an opaque handle that this node
//     minted from an address Tenor itself returned, HMAC'd under a per-process
//     secret. Anything else is refused, so this cannot be turned into an open
//     proxy for scanning the operator's LAN or laundering traffic.
//   - the handle is checked against a host allowlist on the way back OUT as
//     well, and again on every redirect, so a signature alone is not authority.
//   - responses are size-capped with a LimitReader (a Content-Length header is
//     a claim, not a fact) and the whole exchange is under a timeout.
//   - requests are token-bucketed per peer, because the expensive resource here
//     is the operator's API quota and bandwidth, not this node's CPU.
//
// With no API key configured the proxy answers "unavailable" and says so. That
// is a supported state, not a failure: most operators will never set a key, and
// the client is required to explain the difference.

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

	// gifUpstreamTimeout bounds one outbound HTTPS exchange. It must leave room
	// inside the peer's stream deadline (30s) for the reply to be written back.
	gifUpstreamTimeout = 12 * time.Second

	// gifMaxQueryRunes caps the search terms. Tenor's own limit is far higher;
	// this is here so a peer cannot push a megabyte of text through the
	// operator's API account.
	gifMaxQueryRunes = 100
	gifMaxResults    = 30
)

// gifProxy is the node's GIF-search service. A zero key means "not configured",
// which is answered honestly rather than treated as an error.
type gifProxy struct {
	key      string   // Tenor API key, from CONCORD_TENOR_KEY
	base     *url.URL // API base, from CONCORD_TENOR_BASE
	filter   string   // Tenor contentfilter level
	client   *http.Client
	secret   []byte // per-process HMAC key for media handles
	clientID string // Tenor "client_key", an app-level bucket for their analytics

	mu      sync.Mutex
	buckets map[peer.ID]*gifBuckets
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
		p = &gifProxy{buckets: map[peer.ID]*gifBuckets{}}
	}
	cnet.ServeGifSearch(ctx, h, p.handle)
	if p.key == "" {
		fmt.Println("GIF search proxy: no CONCORD_TENOR_KEY set — peers are told it is unavailable.")
		return
	}
	fmt.Println("GIF search proxy enabled (Tenor via", p.base.Host+").")

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

func newGifProxy() (*gifProxy, error) {
	base := os.Getenv("CONCORD_TENOR_BASE")
	if base == "" {
		base = "https://tenor.googleapis.com"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("CONCORD_TENOR_BASE %q is not a URL", base)
	}
	filter := os.Getenv("CONCORD_TENOR_CONTENTFILTER")
	switch filter {
	case "off", "low", "medium", "high":
	case "":
		// Default to Tenor's strictest setting. An operator running a proxy for
		// their friends is not signing up to moderate what it returns, and the
		// permissive default is the one that would surprise them.
		filter = "high"
	default:
		return nil, fmt.Errorf("CONCORD_TENOR_CONTENTFILTER must be off|low|medium|high, got %q", filter)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	p := &gifProxy{
		key:      os.Getenv("CONCORD_TENOR_KEY"),
		base:     u,
		filter:   filter,
		secret:   secret,
		clientID: "concord",
		buckets:  map[peer.ID]*gifBuckets{},
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
		return b.media.take(gifMediaRate, gifMediaBurst)
	}
	return b.search.take(gifSearchRate, gifSearchBurst)
}

// allowURL is the SSRF gate: the only addresses this node will ever fetch.
//
// The operator's own API base is allowed whatever its scheme, because they
// chose it (a self-hosted mirror on the same box, or a test fixture on
// loopback). Everything else must be https and under tenor.com — which is where
// Tenor's CDN lives, and the only place a media URL can legitimately have come
// from. Note what this does NOT defend against: if Tenor's own DNS were made to
// answer with a private address, this check would still pass. Pinning
// resolution would be the fix, and it is not worth the machinery here — an
// attacker who controls tenor.com's DNS has far better targets than one hobby
// node's LAN.
func (p *gifProxy) allowURL(u *url.URL) bool {
	if u == nil || u.Host == "" {
		return false
	}
	if p.base != nil && strings.EqualFold(u.Host, p.base.Host) {
		return true
	}
	if u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "tenor.com" || strings.HasSuffix(h, ".tenor.com")
}

// mint turns a URL Tenor gave us into a handle safe to hand a peer. The peer
// gets the address back verbatim — it is not a secret, and pretending otherwise
// would be theatre — but cannot change so much as one byte of it without the
// MAC failing.
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
		return gifReply(cnet.GifResponse{
			Status: cnet.GifStatusUnavailable,
			Detail: "this rendezvous has no GIF API key configured",
		})
	}
	if !p.allow(from, req.Op == "media") {
		return gifReply(cnet.GifResponse{
			Status: cnet.GifStatusRateLimited,
			Detail: "too many GIF requests — wait a moment",
		})
	}

	switch req.Op {
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

// tenorSearch is the slice of Tenor's v2 response this proxy uses. Everything
// else in their JSON is ignored on purpose: the less of a third party's schema
// that reaches a peer, the less there is to go wrong.
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

	u := *p.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v2/search"
	qs := url.Values{
		"q":             {q},
		"key":           {p.key},
		"limit":         {strconv.Itoa(limit)},
		"media_filter":  {"tinygif,mediumgif,gif"},
		"contentfilter": {p.filter},
	}
	if p.clientID != "" {
		qs.Set("client_key", p.clientID)
	}
	// The cursor is echoed back from a previous reply of ours, but it still
	// arrives from a peer, so it is length-bounded like anything else.
	if pos := req.Pos; pos != "" && len(pos) <= 64 {
		qs.Set("pos", pos)
	}
	u.RawQuery = qs.Encode()

	body, _, err := p.fetch(ctx, &u, 1<<20)
	if err != nil {
		// Deliberately not err.Error(): that string can contain the API key
		// (net/http puts the full URL in its errors) and the peer must never
		// see it.
		return cnet.GifResponse{Status: cnet.GifStatusUpstream, Detail: "the GIF API did not answer"}
	}
	var ts tenorSearch
	if err := json.Unmarshal(body, &ts); err != nil {
		return cnet.GifResponse{Status: cnet.GifStatusUpstream, Detail: "the GIF API sent something unreadable"}
	}

	out := cnet.GifResponse{Status: cnet.GifStatusOK, Source: "Tenor", Next: ts.Next, Results: []cnet.GifHit{}}
	for _, r := range ts.Results {
		pick := func(names ...string) (string, int, int, bool) {
			for _, n := range names {
				f, ok := r.MediaFormats[n]
				if !ok || f.URL == "" {
					continue
				}
				// Skip a format Tenor already says is bigger than we would
				// relay, so a peer never gets a handle that is guaranteed to
				// fail when they click it.
				if f.Size > cnet.MaxGifMediaBytes {
					continue
				}
				fu, err := url.Parse(f.URL)
				if err != nil || !p.allowURL(fu) {
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
		// Thumbnail smallest-first, full biggest-that-fits-first.
		prev, _, _, okP := pick("tinygif", "mediumgif", "gif")
		full, w, h, okF := pick("mediumgif", "gif", "tinygif")
		if !okP || !okF {
			continue
		}
		title := r.ContentDescription
		if title == "" {
			title = r.Title
		}
		out.Results = append(out.Results, cnet.GifHit{
			ID:      r.ID,
			Title:   cleanQuery(title),
			Preview: p.mint(prev),
			Full:    p.mint(full),
			Width:   w, Height: h,
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
	return cnet.GifResponse{Status: cnet.GifStatusOK, Source: "Tenor", Media: body, Subtype: sub}
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
	// half of the privacy property: Tenor sees one constant User-Agent and this
	// node's IP for every member of every guild, with nothing to tell them
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
