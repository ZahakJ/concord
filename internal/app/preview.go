package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/sync/singleflight"
)

// Link previews: the app's ONLY outbound HTTP. A preview fetch reveals the
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
}

type previewEntry struct {
	p       Preview
	err     error
	expires time.Time
}

func newPreviewCache() *previewCache {
	pc := &previewCache{
		entries: map[string]previewEntry{},
		sem:     make(chan struct{}, previewParallel),
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
			return validatePreviewURL(req.URL)
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
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	for _, ip := range ips {
		if isForbiddenIP(ip.IP) {
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("no permitted address for %s", host)
}

// isForbiddenIP rejects every address range an SSRF probe would target.
func isForbiddenIP(ip net.IP) bool {
	if ip == nil ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	// IPv6 unique-local fc00::/7 (not covered by IsPrivate).
	if v6 := ip.To16(); v6 != nil && ip.To4() == nil && (v6[0]&0xfe) == 0xfc {
		return true
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
	if err := validatePreviewURL(u); err != nil {
		return Preview{}, fmt.Errorf("app: %w", err)
	}
	key := u.String()

	pc := s.previews
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
