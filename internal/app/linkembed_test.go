package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/ZahakJ/concord/internal/store"
)

// previewTestService is a store-backed Service with no network, whose preview
// fetcher is allowed to reach a 127.0.0.1 httptest server. The SSRF guard is
// loosened HERE only (validate + default transport); production wiring is
// newPreviewCache() untouched.
func previewTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "concord.db"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	pc := newPreviewCache()
	pc.client = &http.Client{Timeout: 5 * time.Second}
	pc.validate = func(u *url.URL) error {
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("scheme")
		}
		return nil
	}
	return &Service{store: st, previews: pc}
}

// jpegWithMetadata is a 1000x500 JPEG with a fake EXIF APP1 segment spliced in
// right after SOI, carrying a marker string that must not survive cleaning.
func jpegWithMetadata(t *testing.T, marker string) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 1000, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{uint8(x / 4), uint8(y / 2), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	payload := append([]byte("Exif\x00\x00"), []byte(marker)...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	seg = append(seg, payload...)
	return append(append(append([]byte{}, raw[:2]...), seg...), raw[2:]...)
}

func pageServer(t *testing.T, hits *atomic.Int32, img []byte, longTitle string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/post", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>x</title>
<meta property="og:title" content="%s"/>
<meta property="og:description" content="A <b>fine</b> page"/>
<meta property="og:site_name" content="Example Site"/>
<meta property="og:image" content="/cover.jpg"/></head><body></body></html>`, longTitle)
	})
	mux.HandleFunc("/cover.jpg", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func openEmbedBlob(t *testing.T, s *Service, img *LinkEmbedImage) []byte {
	t.Helper()
	ct, ok, err := s.store.GetAttachment(img.Blob)
	if err != nil || !ok {
		t.Fatalf("blob not stored: %v ok=%v", err, ok)
	}
	k, err := b64url.DecodeString(img.Keys)
	if err != nil || len(k) != attachKeysLen {
		t.Fatalf("bad keys: %v", err)
	}
	var key [32]byte
	var nonce [24]byte
	copy(key[:], k[:32])
	copy(nonce[:], k[32:])
	plain, ok := secretbox.Open(nil, ct, &nonce, &key)
	if !ok {
		t.Fatal("blob does not open with the token's keys")
	}
	return plain
}

func TestBuildLinkEmbedFromLocalServer(t *testing.T) {
	var hits atomic.Int32
	srv := pageServer(t, &hits, jpegWithMetadata(t, "SECRET-GPS-48.85N"), strings.Repeat("T", 900))
	s := previewTestService(t)
	link := srv.URL + "/post"

	e, err := s.buildLinkEmbed(link)
	if err != nil {
		t.Fatal(err)
	}
	if e.URL != link {
		t.Fatalf("url = %q, want the link as written", e.URL)
	}
	if n := len([]rune(e.Title)); n != linkEmbedTitleMax {
		t.Fatalf("title not capped: %d runes", n)
	}
	if e.Desc != "A <b>fine</b> page" {
		t.Fatalf("desc = %q (plain text expected, taken as-is)", e.Desc)
	}
	if e.Site != "Example Site" {
		t.Fatalf("site = %q", e.Site)
	}
	if e.Image == nil {
		t.Fatal("no image embedded")
	}
	if e.Image.W != 400 || e.Image.H != 200 || e.Image.Type != "jpeg" {
		t.Fatalf("image not scaled to fit 400px: %+v", e.Image)
	}
	plain := openEmbedBlob(t, s, e.Image)
	if bytes.Contains(plain, []byte("SECRET-GPS")) || bytes.Contains(plain, []byte("Exif")) {
		t.Fatal("image metadata survived cleaning")
	}
	cfg, format, err := stdimage.DecodeConfig(bytes.NewReader(plain))
	if err != nil || format != "jpeg" || cfg.Width != 400 || cfg.Height != 200 {
		t.Fatalf("cleaned image: %v %s %+v", err, format, cfg)
	}
	if len(plain) > 100<<10 {
		t.Fatalf("embedded image is %d bytes; should be small", len(plain))
	}
	// The token carries a blob reference, never a remote image URL.
	tok := encodeLinkEmbeds([]LinkEmbed{e})
	raw, _ := base64.RawURLEncoding.DecodeString(linkEmbedTokenRe.FindStringSubmatch(tok)[1])
	if strings.Contains(string(raw), "cover.jpg") || strings.Contains(string(raw), srv.URL+"/cover") {
		t.Fatalf("token leaks the remote image URL: %s", raw)
	}
}

func TestBuildLinkEmbedRefusesOversizeAndNonImage(t *testing.T) {
	s := previewTestService(t)
	// Huge pixel count: refused from the header, before decoding.
	big := stdimage.NewGray(stdimage.Rect(0, 0, 5000, 5000))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, big, &jpeg.Options{Quality: 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := cleanPreviewImage(buf.Bytes()); err == nil {
		t.Fatal("25 MP image accepted")
	}
	if _, _, _, err := cleanPreviewImage([]byte("<html>not an image</html>")); err == nil {
		t.Fatal("non-image accepted")
	}
	_ = s
}

func TestPrepareLinkPreviewsRoundTripAndCaps(t *testing.T) {
	var hits atomic.Int32
	srv := pageServer(t, &hits, jpegWithMetadata(t, "x"), "Hello")
	s := previewTestService(t)
	a, b := srv.URL+"/post?n=1", srv.URL+"/post?n=2"
	c, d := srv.URL+"/post?n=3", srv.URL+"/post?n=4"
	text := fmt.Sprintf("look %s and %s, also %s %s", a, b, c, d)
	tok := s.PrepareLinkPreviews(text)
	if tok == "" {
		t.Fatal("no token")
	}
	got := ParseLinkEmbeds(text + "\n" + tok)
	if len(got) != 3 {
		t.Fatalf("want exactly 3 previews (cap), got %d", len(got))
	}
	for _, e := range got {
		if e.Image == nil || e.Title != "Hello" {
			t.Fatalf("lost data on the way: %+v", e)
		}
	}
	// A second prepare for the same links is served from cache: no new fetches.
	before := hits.Load()
	_ = s.PrepareLinkPreviews(text)
	if hits.Load() != before {
		t.Fatal("debounced re-prepare refetched")
	}
	if s.PrepareLinkPreviews("no links here") != "" {
		t.Fatal("token for text without links")
	}
}

func TestParseLinkEmbedsRejectsMismatchAndHostile(t *testing.T) {
	img := &LinkEmbedImage{Blob: strings.Repeat("a", 64), Keys: strings.Repeat("k", 75), Type: "jpeg", W: 400, H: 100}
	mk := func(e LinkEmbed) string { return encodeLinkEmbeds([]LinkEmbed{e}) }
	good := LinkEmbed{URL: "https://example.com/a", Title: "T", Image: img}

	if got := ParseLinkEmbeds("see https://example.com/a " + mk(good)); len(got) != 1 {
		t.Fatalf("valid preview rejected: %v", got)
	}
	cases := map[string]string{
		"host mismatch":     "see https://evil.test/a " + mk(good),
		"path mismatch":     "see https://example.com/other " + mk(good),
		"no link in text":   "hello " + mk(good),
		"javascript scheme": "javascript:alert(1) " + mk(LinkEmbed{URL: "javascript:alert(1)", Title: "T"}),
		"userinfo":          "https://u:p@example.com/a " + mk(LinkEmbed{URL: "https://u:p@example.com/a", Title: "T"}),
		"ftp":               "ftp://example.com/a " + mk(LinkEmbed{URL: "ftp://example.com/a", Title: "T"}),
	}
	for name, body := range cases {
		if got := ParseLinkEmbeds(body); len(got) != 0 {
			t.Errorf("%s: accepted %v", name, got)
		}
	}
	// Hostile text is flattened and capped; a bad image ref is dropped, card kept.
	hostile := LinkEmbed{URL: "https://example.com/a", Title: "ti‮tle\x07\n" + strings.Repeat("x", 900),
		Desc: strings.Repeat("d", 2000), Site: strings.Repeat("s", 500),
		Image: &LinkEmbedImage{Blob: "nothex", Keys: "short", Type: "jpeg", W: 4000, H: 4000}}
	raw := mk(hostile)
	if raw == "" {
		t.Skip("oversize token refused at send time — also fine")
	}
	got := ParseLinkEmbeds("https://example.com/a " + raw)
	if len(got) != 1 {
		t.Fatalf("hostile card: %v", got)
	}
	if strings.ContainsAny(got[0].Title, "‮\x07\n") || len([]rune(got[0].Title)) > linkEmbedTitleMax ||
		len([]rune(got[0].Desc)) > linkEmbedDescMax || len([]rune(got[0].Site)) > linkEmbedSiteMax || got[0].Image != nil {
		t.Fatalf("not sanitised: %+v", got[0])
	}
	// Oversize payload and junk are ignored.
	if got := ParseLinkEmbeds("[lp](concord://lp/v1/" + strings.Repeat("A", linkEmbedTokenMax+1) + ")"); got != nil {
		t.Fatal("oversize token accepted")
	}
	if got := ParseLinkEmbeds("[lp](concord://lp/v1/!!!)"); got != nil {
		t.Fatal("junk accepted")
	}
}

// embedCountingTransport fails the test on any attempt to use it.
type embedCountingTransport struct{ n atomic.Int32 }

func (c *embedCountingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, fmt.Errorf("receiver attempted an outbound request")
}

func TestReceivePathMakesNoHTTPRequest(t *testing.T) {
	var hits atomic.Int32
	srv := pageServer(t, &hits, jpegWithMetadata(t, "x"), "Hello")
	sender := previewTestService(t)
	link := srv.URL + "/post"
	body := "check " + link + "\n" + sender.PrepareLinkPreviews("check "+link)
	hits.Store(0)

	recv := previewTestService(t)
	ct := &embedCountingTransport{}
	recv.previews.client = &http.Client{Transport: ct}
	for i := 0; i < 3; i++ {
		if got := ParseLinkEmbeds(body); len(got) != 1 {
			t.Fatalf("receiver could not read the embedded preview: %v", got)
		}
	}
	if ct.n.Load() != 0 || hits.Load() != 0 {
		t.Fatalf("receive path made requests: client=%d server=%d", ct.n.Load(), hits.Load())
	}
}

// Networked: the preview rides the message from A to B, B shows it from the
// message alone, fetches the picture peer to peer from A, and never touches
// its HTTP client.
func TestEmbeddedPreviewArrivesWithMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	b := startService(t, ctx)
	a.previews.client = &http.Client{Timeout: 5 * time.Second}
	a.previews.validate = func(*url.URL) error { return nil }
	ct := &embedCountingTransport{}
	b.previews.client = &http.Client{Transport: ct}

	var hits atomic.Int32
	srv := pageServer(t, &hits, jpegWithMetadata(t, "SECRET"), "Embedded Title")

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	link := srv.URL + "/post"
	text := "worth a read " + link
	tok := a.PrepareLinkPreviews(text)
	if tok == "" {
		t.Fatal("sender built no preview")
	}
	hits.Store(0)
	sent, err := a.send(channel, text+"\n"+tok, "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		for _, m := range mustMessages(t, b, channel) {
			if m.ID == sent.ID {
				return true
			}
		}
		return false
	}, "B never received the message")

	var content string
	for _, m := range mustMessages(t, b, channel) {
		if m.ID == sent.ID {
			content = m.Content
		}
	}
	got := ParseLinkEmbeds(content)
	if len(got) != 1 || got[0].Title != "Embedded Title" || got[0].Image == nil {
		t.Fatalf("B did not get the preview with the message: %+v", got)
	}
	jpg, err := b.FetchAttachment(channel, got[0].Image.Blob, got[0].Image.Keys, "jpeg")
	if err != nil || !strings.HasPrefix(jpg, "data:image/jpeg;base64,") {
		t.Fatalf("B could not fetch the embedded picture from peers: %v", err)
	}
	if ct.n.Load() != 0 || hits.Load() != 0 {
		t.Fatalf("receiver made HTTP requests: client=%d server=%d", ct.n.Load(), hits.Load())
	}
}
