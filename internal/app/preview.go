package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/sync/singleflight"
)

// Link previews: the app's ONLY outbound HTTP, and it is made only by the
// SENDER's device when it builds a preview to attach to its own message
// (linkembed.go). A receiver never calls this: it renders what the message
// carries and nothing else. A preview fetch reveals the
// user's IP to the linked site — the same exposure as clicking the link — but
// nothing else (no cookies, no auth, tight size/time caps). Because a chat
// message can name any URL, the fetcher must be SSRF-safe: it will not talk
// to loopback, private, or link-local addresses, and it dials the exact IP it
// vetted (so a DNS rebind between check and dial buys an attacker nothing).

// Preview is the scraped OpenGraph/title summary of a link.
type Preview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"imageUrl"`
	SiteName    string `json:"siteName"`
}

const (
	previewTimeout   = 5 * time.Second
	previewBodyCap   = 512 << 10 // bytes of HTML we're willing to read
	previewCacheCap  = 256
	previewTTL       = time.Hour
	previewErrTTL    = 10 * time.Minute
	previewParallel  = 4
	previewUserAgent = "concord/1.0 (link preview)"
)

type previewCache struct {
	mu      sync.Mutex
	entries map[string]previewEntry
	flight  singleflight.Group
	sem     chan struct{}
	client  *http.Client
	// validate vets a URL before we fetch it and on every redirect hop. It is
	// validatePreviewURL in production; only tests replace it (to reach an
	// httptest server on a random loopback port).
	validate func(*url.URL) error
	// embeds caches finished sender-side embeds (see linkembed.go).
	embeds map[string]embedEntry
}

type previewEntry struct {
	p       Preview
	err     error
	expires time.Time
}

func newPreviewCache() *previewCache {
	pc := &previewCache{
		entries:  map[string]previewEntry{},
		sem:      make(chan struct{}, previewParallel),
		validate: validatePreviewURL,
		embeds:   map[string]embedEntry{},
	}
	pc.client = &http.Client{
		Timeout: previewTimeout,
		Transport: &http.Transport{
			DialContext:       safeDialContext,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return pc.validate(req.URL)
		},
	}
	return pc
}

// safeDialContext resolves the host itself, rejects any forbidden address,
// and then dials the vetted IP literal — pinning the address that was checked.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := previewLookup(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if previewForbidden(ip.IP) {
			continue
		}
		conn, err := previewDial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("no permitted address for %s", host)
}

// previewLookup, previewForbidden and previewDial are the resolver, the
// address check and the dialer every preview and link-card connection goes
// through. They are variables only so a test in this package can point a fake
// host name at a private address (to prove the check runs AFTER resolution)
// and route a vetted public-looking address to its own local test server;
// nothing outside the tests assigns them.
var (
	previewLookup    = net.DefaultResolver.LookupIPAddr
	previewForbidden = isForbiddenIP
	previewDial      = (&net.Dialer{}).DialContext
)

// The address blocks a link fetch must never reach: every block in IANA's
// IPv4 and IPv6 Special-Purpose Address Registries that is not globally
// reachable, plus multicast and the reserved 240/4. A card fetch runs on the
// sender's device by default on every send, and the title it reads is posted
// to the channel, so a page that redirects to http://100.100.100.100/ (a
// tailnet) or to a NAT64-wrapped 10.x address would otherwise publish an
// internal page's title.
//
// Three prefixes carry an IPv4 address inside them and are judged by that
// address instead: IPv4-mapped ::ffff:0:0/96 (net.IP.To4 already unwraps it),
// the well-known NAT64 prefix 64:ff9b::/96 (RFC 6052; an IPv6-only phone
// network's DNS64 answers every IPv4-only site with one of these, so the
// prefix as such must stay usable) and 6to4 2002::/16 (RFC 3056).
var forbiddenPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		// IPv4 (iana-ipv4-special-registry, and multicast/reserved).
		"0.0.0.0/8",       // "this network" (RFC 791)
		"10.0.0.0/8",      // private (RFC 1918)
		"100.64.0.0/10",   // shared address space / CGNAT, Tailscale (RFC 6598)
		"127.0.0.0/8",     // loopback
		"169.254.0.0/16",  // link-local
		"172.16.0.0/12",   // private
		"192.0.0.0/24",    // IETF protocol assignments (RFC 6890)
		"192.0.2.0/24",    // TEST-NET-1
		"192.88.99.0/24",  // deprecated 6to4 relay anycast (RFC 7526)
		"192.168.0.0/16",  // private
		"198.18.0.0/15",   // benchmarking (RFC 2544)
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"224.0.0.0/4",     // multicast
		"240.0.0.0/4",     // reserved, and 255.255.255.255 limited broadcast
		// IPv6 (iana-ipv6-special-registry).
		"::/96",           // unspecified, loopback, deprecated IPv4-compatible
		"::ffff:0:0:0/96", // IPv4-translated (RFC 2765)
		"64:ff9b:1::/48",  // local-use IPv4/IPv6 translation (RFC 8215)
		"100::/64",        // discard-only (RFC 6666)
		"100:0:0:1::/64",  // dummy prefix (RFC 9780)
		"2001::/23",       // IETF protocol assignments, Teredo included
		"2001:db8::/32",   // documentation
		"3fff::/20",       // documentation (RFC 9637)
		"5f00::/16",       // segment routing SIDs (RFC 9602)
		"fc00::/7",        // unique local
		"fe80::/10",       // link-local
		"fec0::/10",       // deprecated site-local
		"ff00::/8",        // multicast
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

var (
	nat64WellKnown = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour      = netip.MustParsePrefix("2002::/16")
)

// isForbiddenIP rejects every address range an SSRF probe would target.
func isForbiddenIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	a = a.Unmap()
	b := a.As16()
	switch {
	case nat64WellKnown.Contains(a):
		a = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
	case sixToFour.Contains(a):
		a = netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]})
	}
	for _, p := range forbiddenPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func validatePreviewURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("userinfo not allowed")
	}
	switch u.Port() {
	case "", "80", "443":
	default:
		return fmt.Errorf("port %q not allowed", u.Port())
	}
	return nil
}

// LinkPreview fetches (or serves from cache) a link's OpenGraph summary.
func (s *Service) LinkPreview(rawURL string) (Preview, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return Preview{}, fmt.Errorf("app: bad url: %w", err)
	}
	u.Fragment = ""
	pc := s.previews
	if err := pc.validate(u); err != nil {
		return Preview{}, fmt.Errorf("app: %w", err)
	}
	key := u.String()

	pc.mu.Lock()
	if e, ok := pc.entries[key]; ok && time.Now().Before(e.expires) {
		pc.mu.Unlock()
		return e.p, e.err
	}
	pc.mu.Unlock()

	v, err, _ := pc.flight.Do(key, func() (any, error) {
		pc.sem <- struct{}{}
		defer func() { <-pc.sem }()
		p, err := pc.fetch(key)
		ttl := previewTTL
		if err != nil {
			ttl = previewErrTTL
		}
		pc.mu.Lock()
		if len(pc.entries) >= previewCacheCap {
			// Crude overflow relief: drop a handful of arbitrary entries.
			n := 0
			for k := range pc.entries {
				delete(pc.entries, k)
				if n++; n >= 16 {
					break
				}
			}
		}
		pc.entries[key] = previewEntry{p: p, err: err, expires: time.Now().Add(ttl)}
		pc.mu.Unlock()
		return p, err
	})
	if err != nil {
		return Preview{}, err
	}
	return v.(Preview), nil
}

func (pc *previewCache) fetch(rawURL string) (Preview, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return Preview{}, err
	}
	req.Header.Set("User-Agent", previewUserAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := pc.client.Do(req)
	if err != nil {
		return Preview{}, fmt.Errorf("app: preview fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Preview{}, fmt.Errorf("app: preview fetch: HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		return Preview{}, fmt.Errorf("app: not an html page")
	}

	p := parsePreviewHTML(io.LimitReader(resp.Body, previewBodyCap), resp.Request.URL)
	p.URL = rawURL
	if p.Title == "" && p.Description == "" {
		return Preview{}, fmt.Errorf("app: page has no preview metadata")
	}
	return p, nil
}

// parsePreviewHTML walks the document head for OpenGraph / twitter-card /
// <title> metadata. Values are truncated; og:image is resolved against the
// final URL and kept only when http(s).
func parsePreviewHTML(r io.Reader, base *url.URL) Preview {
	var p Preview
	var title string
	z := html.NewTokenizer(r)
	depth := 0
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			finishPreview(&p, title, base)
			return p
		case html.TextToken:
			if inTitle {
				title += string(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == "title" {
				inTitle = true
				continue
			}
			if tag == "body" || depth > 512 {
				finishPreview(&p, title, base)
				return p
			}
			depth++
			if tag != "meta" || !hasAttr {
				continue
			}
			var prop, content string
			for {
				k, v, more := z.TagAttr()
				switch string(k) {
				case "property", "name":
					prop = string(v)
				case "content":
					content = string(v)
				}
				if !more {
					break
				}
			}
			switch prop {
			case "og:title":
				p.Title = content
			case "og:description":
				p.Description = content
			case "og:image", "og:image:url":
				if p.ImageURL == "" {
					p.ImageURL = content
				}
			case "og:site_name":
				p.SiteName = content
			case "twitter:title":
				if p.Title == "" {
					p.Title = content
				}
			case "twitter:description":
				if p.Description == "" {
					p.Description = content
				}
			case "twitter:image":
				if p.ImageURL == "" {
					p.ImageURL = content
				}
			case "description":
				if p.Description == "" {
					p.Description = content
				}
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "title" {
				inTitle = false
			}
		}
	}
}

func finishPreview(p *Preview, title string, base *url.URL) {
	if p.Title == "" {
		p.Title = strings.TrimSpace(title)
	}
	p.Title = truncateRunes(strings.TrimSpace(p.Title), 200)
	p.Description = truncateRunes(strings.TrimSpace(p.Description), 400)
	p.SiteName = truncateRunes(strings.TrimSpace(p.SiteName), 100)
	if p.ImageURL != "" && base != nil {
		if img, err := base.Parse(p.ImageURL); err == nil && (img.Scheme == "http" || img.Scheme == "https") {
			p.ImageURL = img.String()
		} else {
			p.ImageURL = ""
		}
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
