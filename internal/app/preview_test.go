package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestForbiddenIPRanges(t *testing.T) {
	forbidden := []string{
		"127.0.0.1", "10.0.0.5", "172.16.1.1", "192.168.1.10", "169.254.1.1",
		"0.0.0.0", "224.0.0.1", "::1", "fe80::1", "fc00::1", "fd12::1", "ff02::1", "::",
	}
	for _, s := range forbidden {
		if !isForbiddenIP(net.ParseIP(s)) {
			t.Errorf("%s should be forbidden", s)
		}
	}
	// The IANA special-purpose blocks the first version let through
	// (link-cards review LC-06), and the ones that hide an IPv4 address inside
	// an IPv6 one.
	forbidden = []string{
		"100.64.0.1", "100.100.100.100", "100.127.255.254", // CGNAT / Tailscale
		"0.1.2.3", "192.0.0.8", "192.0.0.170", "192.0.2.1", "192.88.99.1",
		"198.18.0.1", "198.19.255.1", "198.51.100.1", "203.0.113.1",
		"240.0.0.1", "255.255.255.255",
		"::ffff:10.0.0.1", "::ffff:100.64.0.1", // IPv4-mapped
		"64:ff9b::a00:1", "64:ff9b::6440:1", "64:ff9b::7f00:1", // NAT64 wrapping 10/8, 100.64/10, 127/8
		"2002:a00:1::1", "2002:c0a8:101::1", "2002:6440:1::", // 6to4 wrapping private and CGNAT
		"64:ff9b:1::1", "100::1", "2001::1", "2001:db8::1", "3fff::1", "5f00::1",
		"fec0::1", "::ffff:0:a00:1", "::a00:1",
	}
	for _, s := range forbidden {
		if !isForbiddenIP(net.ParseIP(s)) {
			t.Errorf("%s should be forbidden", s)
		}
	}
	allowed := []string{
		"93.184.216.34", "1.1.1.1", "2606:4700::1111", "100.128.0.1", "100.63.255.255", "198.20.0.1",
		// A public IPv4 address through NAT64 or 6to4 is still that address:
		// an IPv6-only network's DNS64 answers every IPv4-only site this way.
		"64:ff9b::5db8:d822", "2002:5db8:d822::1",
	}
	for _, s := range allowed {
		if isForbiddenIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func TestValidatePreviewURL(t *testing.T) {
	bad := []string{
		"ftp://x.com/a", "file:///etc/passwd", "javascript:alert(1)",
		"http://user:pass@x.com/", "http://x.com:8080/", "http://x.com:22/",
	}
	for _, s := range bad {
		u, err := url.Parse(s)
		if err != nil {
			continue
		}
		if validatePreviewURL(u) == nil {
			t.Errorf("%s should be rejected", s)
		}
	}
	for _, s := range []string{"http://x.com/a", "https://x.com:443/b?q=1"} {
		u, _ := url.Parse(s)
		if err := validatePreviewURL(u); err != nil {
			t.Errorf("%s should be accepted: %v", s, err)
		}
	}
}

// TestLinkPreviewRefusesLocal proves the whole pipeline refuses to touch a
// loopback server even via a hostname that resolves there.
func TestLinkPreviewRefusesLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("SSRF: the local server was reached")
	}))
	defer srv.Close()

	pc := newPreviewCache()
	if _, err := pc.fetch(srv.URL); err == nil {
		t.Fatal("fetch of a loopback URL succeeded")
	}
	// "localhost" hostname form too.
	u, _ := url.Parse(srv.URL)
	if _, err := pc.fetch("http://localhost:" + u.Port() + "/"); err == nil {
		t.Fatal("fetch of localhost URL succeeded")
	}
}

func TestParsePreviewHTML(t *testing.T) {
	base, _ := url.Parse("https://example.com/post/1")
	page := `<html><head>
	  <title>Fallback Title</title>
	  <meta property="og:title" content="OG Title"/>
	  <meta property="og:description" content="A description."/>
	  <meta property="og:image" content="/img/cover.png"/>
	  <meta property="og:site_name" content="Example"/>
	</head><body><p>ignored</p></body></html>`
	p := parsePreviewHTML(strings.NewReader(page), base)
	if p.Title != "OG Title" || p.Description != "A description." || p.SiteName != "Example" {
		t.Fatalf("bad parse: %+v", p)
	}
	if p.ImageURL != "https://example.com/img/cover.png" {
		t.Fatalf("og:image not resolved: %q", p.ImageURL)
	}

	// Title fallback + hostile javascript: image rejected.
	page2 := `<html><head><title>Only Title</title>
	  <meta property="og:image" content="javascript:alert(1)"/></head><body></body></html>`
	p2 := parsePreviewHTML(strings.NewReader(page2), base)
	if p2.Title != "Only Title" {
		t.Fatalf("title fallback failed: %+v", p2)
	}
	if p2.ImageURL != "" {
		t.Fatalf("hostile image URL kept: %q", p2.ImageURL)
	}

	// Truncation.
	long := strings.Repeat("x", 500)
	p3 := parsePreviewHTML(strings.NewReader(`<head><meta property="og:title" content="`+long+`"/></head>`), base)
	if len([]rune(p3.Title)) > 201 {
		t.Fatalf("title not truncated: %d runes", len([]rune(p3.Title)))
	}
}
