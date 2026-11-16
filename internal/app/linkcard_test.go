package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	goimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Every test here talks to a local httptest server only. Names under .test
// resolve (through the previewLookup hook) to a public-looking TEST-NET
// address, which passes the SSRF check exactly as a real site would, and the
// previewDial hook routes that address to the local server. Anything the hook
// does not know falls through to the REAL resolver and the REAL check \u2014 which
// is how "redirect to localhost" is proved to be refused.

// A public address. (TEST-NET-3 used to stand in for one; isForbiddenIP now
// refuses every documentation block, as the IANA registry says it should.)
const cardPublicIP = "93.184.216.34"

type cardRig struct {
	srv      *httptest.Server
	dials    atomic.Int32
	privDial atomic.Int32
}

func newCardRig(t *testing.T, h http.Handler) *cardRig {
	t.Helper()
	r := &cardRig{srv: httptest.NewServer(h)}
	addr := r.srv.Listener.Addr().String()
	oldLookup, oldDial := previewLookup, previewDial
	previewLookup = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		switch {
		case host == "private.test":
			// A name whose DNS answer is a private address: the check must run
			// on what the name RESOLVED to.
			return []net.IPAddr{{IP: net.ParseIP("10.1.2.3")}}, nil
		case host == "tailnet.test":
			// CGNAT 100.64/10: Tailscale's own address and every tailnet node.
			return []net.IPAddr{{IP: net.ParseIP("100.100.100.100")}}, nil
		case host == "nat64.test":
			// The well-known NAT64 prefix wrapping 10.0.0.1.
			return []net.IPAddr{{IP: net.ParseIP("64:ff9b::a00:1")}}, nil
		case host == "sixtofour.test":
			// 6to4 wrapping 192.168.1.1.
			return []net.IPAddr{{IP: net.ParseIP("2002:c0a8:101::1")}}, nil
		case strings.HasSuffix(host, ".test"):
			return []net.IPAddr{{IP: net.ParseIP(cardPublicIP)}}, nil
		}
		return oldLookup(ctx, host)
	}
	previewDial = func(ctx context.Context, network, a string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(a)
		if host != cardPublicIP {
			r.privDial.Add(1)
			return nil, fmt.Errorf("test: refusing to dial %s", a)
		}
		r.dials.Add(1)
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	oldYT, oldThumb, oldVimeo := ytOEmbedEndpoint, ytThumbBase, vimeoOEmbedEndpoint
	ytOEmbedEndpoint = "http://oembed.test/youtube"
	ytThumbBase = "http://img.test/vi/"
	vimeoOEmbedEndpoint = "http://oembed.test/vimeo"
	t.Cleanup(func() {
		r.srv.Close()
		previewLookup, previewDial = oldLookup, oldDial
		ytOEmbedEndpoint, ytThumbBase, vimeoOEmbedEndpoint = oldYT, oldThumb, oldVimeo
	})
	return r
}

func cardService() *Service { return &Service{previews: newPreviewCache()} }

func testJPEG(w, h int) []byte {
	img := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

func page(title, extra string) string {
	return `<html><head><meta property="og:title" content="` + title + `">` + extra + `</head><body>x</body></html>`
}

func build(t *testing.T, raw string) (LinkCard, error) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cardBudget)
	defer cancel()
	return cardService().previews.buildCard(ctx, u, true)
}

func TestLinkCardHostilePages(t *testing.T) {
	huge := strings.Repeat("A", 100_000)
	mux := http.NewServeMux()
	serveHTML := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, body)
		}
	}
	mux.HandleFunc("/huge", serveHTML(page(huge, "")))
	mux.HandleFunc("/script", serveHTML(page(`&lt;script&gt;alert(1)&lt;/script&gt; *bold* [x] @everyone :smile:`, "")))
	mux.HandleFunc("/bidi", serveHTML(page("Invoice \u202egnp.exe\u202c done\u200b\u2066x\u2069", "<meta property=\"og:site_name\" content=\"\u202etube\u202c\">")))
	mux.HandleFunc("/phish", serveHTML(page("paypal.com", "")))
	mux.HandleFunc("/addr", serveHTML(page("https://www.paypal.com/signin", "")))
	mux.HandleFunc("/nodejs", serveHTML(page("Node.js", `<meta property="og:video:duration" content="213">`)))
	mux.HandleFunc("/json-as-html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"title":"nope"}`)
	})
	mux.HandleFunc("/to-localhost", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost/", http.StatusFound)
	})
	mux.HandleFunc("/to-loopback-port", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:8080/", http.StatusFound)
	})
	mux.HandleFunc("/to-private-name", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://private.test/", http.StatusFound)
	})
	for _, h := range []string{"tailnet", "nat64", "sixtofour"} {
		target := "http://" + h + ".test/"
		mux.HandleFunc("/to-"+h, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target, http.StatusFound)
		})
	}
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(20 * time.Second):
		case <-r.Context().Done():
		}
	})
	rig := newCardRig(t, mux)

	c, err := build(t, "http://site.test/huge")
	if err != nil || len([]rune(c.Title)) > cardTitleMax+1 {
		t.Fatalf("huge title: err=%v len=%d", err, len([]rune(c.Title)))
	}

	c, err = build(t, "http://site.test/script")
	if err != nil {
		t.Fatal(err)
	}
	// Kept as inert text; the escapes make every client print it literally.
	block, _ := cardService().finishCard(c, nil, "http://site.test/script")
	if !strings.Contains(c.Title, "<script>") {
		t.Fatalf("title text lost: %q", c.Title)
	}
	for _, want := range []string{`\*bold\*`, `\[x\]`, `\@everyone`, `\:smile:`} {
		if !strings.Contains(block.Block, want) {
			t.Errorf("block %q does not escape %s", block.Block, want)
		}
	}

	c, err = build(t, "http://site.test/bidi")
	if err != nil {
		t.Fatal(err)
	}
	if c.Site != "tube" || strings.ContainsAny(c.Title+c.Site, "\u202e\u202c\u200b\u2066\u2069") {
		t.Fatalf("bidi controls survived: %q / %q", c.Title, c.Site)
	}

	for _, p := range []string{"/phish", "/addr"} {
		c, err = build(t, "http://site.test"+p)
		if err != nil || c.Title != "" {
			t.Errorf("%s: a title naming a site must be refused: %q err=%v", p, c.Title, err)
		}
	}
	c, err = build(t, "http://site.test/nodejs")
	if err != nil || c.Title != "Node.js" || c.Duration != "3:33" {
		t.Errorf("Node.js is a name, and the duration is read: %+v err=%v", c, err)
	}

	if _, err = build(t, "http://site.test/json-as-html"); err == nil {
		t.Error("a JSON body was read as a page")
	}

	before := rig.privDial.Load()
	for _, p := range []string{"/to-localhost", "/to-loopback-port", "/to-private-name", "/to-tailnet", "/to-nat64", "/to-sixtofour"} {
		if _, err = build(t, "http://site.test"+p); err == nil {
			t.Errorf("%s: redirect target was fetched", p)
		}
	}
	if rig.privDial.Load() != before {
		t.Errorf("a forbidden address reached the dialer %d time(s)", rig.privDial.Load()-before)
	}
	// A private DNS answer is refused directly, not only after a redirect.
	if _, err = build(t, "http://private.test/"); err == nil {
		t.Error("a name resolving to 10.1.2.3 was fetched")
	}
	before = rig.privDial.Load()
	for _, h := range []string{"tailnet", "nat64", "sixtofour"} {
		if _, err = build(t, "http://"+h+".test/"); err == nil {
			t.Errorf("%s.test was fetched", h)
		}
	}
	if rig.privDial.Load() != before {
		t.Errorf("a CGNAT, NAT64 or 6to4 address reached the dialer %d time(s)", rig.privDial.Load()-before)
	}

	start := time.Now()
	if _, err = build(t, "http://site.test/slow"); err == nil {
		t.Error("a server that never answers produced a card")
	}
	if d := time.Since(start); d > cardBudget+2*time.Second {
		t.Errorf("slow server held the card for %v", d)
	}
}

// pngClaiming is a valid PNG — signature, IHDR with a correct CRC — whose
// header claims w×h while its pixel data is one pixel's worth. DecodeConfig
// reads it happily, so it reaches the size check; a decoder that got past the
// check would allocate the whole claimed frame before finding the data short.
// (The first version of this fixture patched the size without the CRC, and
// DecodeConfig refused it as corrupt before any size check ran.)
func pngClaiming(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, goimage.NewGray(goimage.Rect(0, 0, 1, 1)))
	p := b.Bytes()
	binary.BigEndian.PutUint32(p[16:], uint32(w))
	binary.BigEndian.PutUint32(p[20:], uint32(h))
	binary.BigEndian.PutUint32(p[29:], crc32.ChecksumIEEE(p[12:29])) // over type + data
	return p
}

func TestLinkCardImages(t *testing.T) {
	big := testJPEG(1280, 720)
	bombBytes := pngClaiming(30000, 30000)

	mux := http.NewServeMux()
	img := func(ct string, b []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", ct)
			_, _ = w.Write(b)
		}
	}
	mux.HandleFunc("/big.jpg", img("image/jpeg", big))
	mux.HandleFunc("/bomb.png", img("image/png", bombBytes))
	mux.HandleFunc("/oversize.jpg", img("image/jpeg", append(testJPEG(64, 64), make([]byte, cardImageCap+10)...)))
	mux.HandleFunc("/html.jpg", img("text/html", big))
	mux.HandleFunc("/svg", img("image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)))
	for _, p := range []string{"big.jpg", "bomb.png", "oversize.jpg", "html.jpg", "svg"} {
		p := p
		mux.HandleFunc("/page-"+p, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page("A page", `<meta property="og:image" content="http://img.test/`+p+`">`))
		})
	}
	newCardRig(t, mux)

	c, err := build(t, "http://site.test/page-big.jpg")
	if err != nil || len(c.thumb) == 0 {
		t.Fatalf("a normal picture made no thumbnail: err=%v", err)
	}
	if c.w > cardThumbW || c.h > cardThumbH || len(c.thumb) > cardThumbBytes {
		t.Fatalf("thumbnail not bounded: %dx%d, %d bytes", c.w, c.h, len(c.thumb))
	}
	if cfg, f, err := goimage.DecodeConfig(bytes.NewReader(c.thumb)); err != nil || f != "jpeg" || cfg.Width != c.w {
		t.Fatalf("thumbnail is not the re-encoded JPEG: %v %s %+v", err, f, cfg)
	}
	for _, p := range []string{"bomb.png", "oversize.jpg", "html.jpg", "svg"} {
		c, err := build(t, "http://site.test/page-"+p)
		if err != nil || c.Title != "A page" {
			t.Errorf("%s: the card should survive a bad picture: %+v err=%v", p, c, err)
		}
		if len(c.thumb) != 0 {
			t.Errorf("%s: a bad picture became a thumbnail", p)
		}
	}
}

func TestLinkCardOEmbed(t *testing.T) {
	var gotURL atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube", func(w http.ResponseWriter, r *http.Request) {
		gotURL.Store(r.URL.Query().Get("url"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"title":"Never Gonna Give You Up","author_name":"Rick Astley","thumbnail_url":"http://elsewhere.test/x.jpg"}`)
	})
	var vimeoBody atomic.Value
	vimeoBody.Store(`{"title":"A Vimeo film","author_name":"Someone","duration":3725,"thumbnail_url":"http://img.test/v.jpg"}`)
	mux.HandleFunc("/vimeo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, vimeoBody.Load().(string))
	})
	mux.HandleFunc("/vi/dQw4w9WgXcQ/hqdefault.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(testJPEG(480, 360))
	})
	mux.HandleFunc("/v.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(testJPEG(640, 360))
	})
	newCardRig(t, mux)

	c, err := build(t, "https://youtu.be/dQw4w9WgXcQ?t=42")
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Never Gonna Give You Up" || c.Site != "Rick Astley" {
		t.Fatalf("youtube card: %+v", c)
	}
	if gotURL.Load() != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("oEmbed asked about %v, not the rebuilt watch URL", gotURL.Load())
	}
	// hqdefault (4:3, letterboxed) is cropped to 16:9.
	if c.w != 480 || c.h != 270 {
		t.Fatalf("youtube thumbnail %dx%d, want 480x270", c.w, c.h)
	}

	c, err = build(t, "https://vimeo.com/76979871")
	if err != nil || c.Title != "A Vimeo film" || c.Duration != "1:02:05" || len(c.thumb) == 0 {
		t.Fatalf("vimeo card: %+v err=%v", c, err)
	}

	// A length with a fraction keeps the card (LC2-06).
	vimeoBody.Store(`{"title":"A short film","author_name":"Someone","duration":12.5,"thumbnail_url":"http://img.test/v.jpg"}`)
	c, err = build(t, "https://vimeo.com/76979871")
	if err != nil || c.Title != "A short film" || c.Duration != "0:13" {
		t.Fatalf("vimeo card with a fractional length: %+v err=%v", c, err)
	}

	// Look-alike hosts are not YouTube, however the path reads.
	for _, raw := range []string{
		"https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ",
		"https://evil.example/?x=youtu.be/dQw4w9WgXcQ",
		"https://notyoutube.com/watch?v=dQw4w9WgXcQ",
	} {
		u, _ := url.Parse(raw)
		if youtubeVideoID(u) != "" {
			t.Errorf("%s treated as YouTube", raw)
		}
	}
}

// The block must parse back on the reader's side: the masked line, then a v2
// token with the card bit, whose blob opens to the thumbnail just encoded.
func TestLinkCardBlockRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a service")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	c := LinkCard{Title: "A (good) title_with ]brackets[", Site: "Some Channel", Duration: "3:33", thumb: testJPEG(480, 270), w: 480, h: 270}
	out, err := s.finishCard(c, nil, "https://en.wikipedia.org/wiki/Foo_(bar)")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.Block, "\n")
	if len(lines) != 2 {
		t.Fatalf("block: %q", out.Block)
	}
	if lines[0] != `[A (good) title\_with \]brackets\[](https://en.wikipedia.org/wiki/Foo_%28bar%29) · wikipedia.org` {
		t.Fatalf("masked line: %s", lines[0])
	}
	m := regexp.MustCompile(`^!\[image\]\(concord://attach/v2/([0-9a-f]{64})/([A-Za-z0-9_-]{75})/jpeg/480x270/2//([A-Za-z0-9_-]*)\)$`).FindStringSubmatch(lines[1])
	if m == nil {
		t.Fatalf("token line: %s", lines[1])
	}
	desc, _ := b64url.DecodeString(m[3])
	if string(desc) != "Some Channel · 3:33" {
		t.Fatalf("caption %q", desc)
	}
	dataURL, err := s.FetchAttachment("", m[1], m[2], "jpeg")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, "data:image/jpeg;base64,"))
	if !bytes.Equal(got, c.thumb) {
		t.Fatal("blob does not open to the thumbnail")
	}
}

func TestCardTitleRule(t *testing.T) {
	for title, ok := range map[string]bool{
		"Never Gonna Give You Up": true,
		"Node.js":                 true,
		"ASP.NET":                 false, // .net is a TLD: reads as a place
		"paypal.com":              false,
		"www.example":             false,
		"see https://x.org":       false,
		"github.com/user/repo":    false,
		"":                        false,
		"3.14":                    true,
		"*paypal.com*":            false,
		"(paypal.com).":           false,
		// Invisible and look-alike spellings of an address (review LC-03).
		"pay\u00adpal.com": false, // soft hyphen
		"paypal\u2060.com": false, // word joiner
		"paypal\u200b.com": false, // zero-width space
		"paypal\u3002com":  false, // ideographic full stop
		"paypal\uff61com":  false, // halfwidth ideographic full stop
		"ｐａｙｐａｌ．ｃｏｍ":       false, // fullwidth
		"Ｎｏｄｅ.ｊｓ":          true,
	} {
		if cardTitleOK(title) != ok {
			t.Errorf("cardTitleOK(%q) = %v", title, !ok)
		}
	}
}

// The size guard runs on the header and refuses before anything decodes, and
// the refusal is the size check's own — not a decoder tripping over the data.
func TestLinkCardImageSizeGuard(t *testing.T) {
	cases := map[string][]byte{
		"bomb":      pngClaiming(30000, 30000), // over the side limit
		"wide":      pngClaiming(5000, 600),    // one side over
		"pixels":    pngClaiming(2100, 2100),   // 4.4 MP: sides fine, pixels over
		"3000x2000": pngClaiming(3000, 2000),
	}
	mux := http.NewServeMux()
	for name, b := range cases {
		if cfg, err := png.DecodeConfig(bytes.NewReader(b)); err != nil || cfg.Width == 0 {
			t.Fatalf("%s: the fixture does not reach the size check: %v", name, err)
		}
		mux.HandleFunc("/"+name+".png", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(b)
		})
	}
	ok := testJPEG(1920, 1080) // 2 MP, a large real og:image
	mux.HandleFunc("/ok.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(ok)
	})
	newCardRig(t, mux)
	pc := cardService().previews
	ctx, cancel := context.WithTimeout(context.Background(), cardBudget)
	defer cancel()
	for name := range cases {
		_, _, _, err := pc.thumbnail(ctx, "http://img.test/"+name+".png", false)
		if err == nil || !strings.Contains(err.Error(), "is too large") {
			t.Errorf("%s: want the size check's refusal, got %v", name, err)
		}
	}
	if _, w, h, err := pc.thumbnail(ctx, "http://img.test/ok.jpg", false); err != nil || w != cardThumbW || h != cardThumbH {
		t.Errorf("a 1920x1080 picture was refused: %dx%d %v", w, h, err)
	}
}

// Decodes run one at a time, however many cards are being built.
func TestLinkCardDecodesOneAtATime(t *testing.T) {
	pic := testJPEG(640, 360)
	mux := http.NewServeMux()
	mux.HandleFunc("/p.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(pic)
	})
	newCardRig(t, mux)
	var peak atomic.Int32
	cardDecodeObserve = func(n int32) {
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond) // hold the decode open so overlap would show
	}
	defer func() { cardDecodeObserve = nil }()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), cardBudget)
			defer cancel()
			if _, _, _, err := cardService().previews.thumbnail(ctx, "http://img.test/p.jpg", false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if p := peak.Load(); p != 1 {
		t.Fatalf("%d decodes ran at once", p)
	}
}

// Shrinking reads the decoded frame in place: no second full-size copy.
func TestDownscaleAllocatesNoFrame(t *testing.T) {
	img := goimage.NewNRGBA(goimage.Rect(0, 0, 2000, 2000)) // 16 MB of pixels
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	out := downscale(img, img.Bounds(), cardThumbW, cardThumbH)
	runtime.ReadMemStats(&m1)
	if d := m1.TotalAlloc - m0.TotalAlloc; d > 2<<20 {
		t.Fatalf("downscale allocated %d bytes for a %dx%d thumbnail", d, out.Rect.Dx(), out.Rect.Dy())
	}
	// Transparent pixels land on white, opaque ones keep their colour.
	clear := goimage.NewNRGBA(goimage.Rect(0, 0, 4, 4))
	if o := downscale(clear, clear.Bounds(), 2, 2); o.Pix[0] != 255 || o.Pix[1] != 255 {
		t.Fatalf("transparent did not become white: %v", o.Pix[:4])
	}
	if o := downscale(img, img.Bounds(), 2, 2); o.Pix[0] != 0 {
		t.Fatalf("opaque black became %v", o.Pix[:4])
	}
}

// A page cannot pass itself off as another site through its site name or
// channel: every address in that text that is not the link's own goes,
// however it is spelled.
func TestLinkCardSiteCannotNameAnotherSite(t *testing.T) {
	mux := http.NewServeMux()
	site := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page("Never Gonna Give You Up (Official Video)", `<meta property="og:site_name" content="`+name+`"><meta property="og:video:duration" content="213">`))
		}
	}
	cases := map[string]string{
		"Rick Astley - Topic · youtube.com":       "Rick Astley - Topic",
		"Rick Astley - Topic·youtube.com":         "Rick Astley - Topic",
		"youtube.com | Rick Astley":               "Rick Astley",
		"Rick Astley https://www.youtube.com/@ra": "Rick Astley",
		"Rick Astley ｙｏｕｔｕｂｅ．ｃｏｍ":                 "Rick Astley",
		"Rick Astley you\u00adtube\u3002com":      "Rick Astley",
		"Rick Astley youtube\u200b.com":           "Rick Astley",
		"Rick Astley youtube\u00b7com":            "Rick Astley",
		"Rick Astley youtube\u2027com":            "Rick Astley",
		"Rick Astley youtube[.]com":               "Rick Astley",
		"Topic\u00b7youtube\u00b7com\u00b7x":      "Topic · x",
		"News bbc(.)co(.)uk":                      "News",
		"Col\u00b7lecci\u00f3 d'art":              "Col\u00b7lecci\u00f3 d'art",
		"Node.js Foundation":                      "Node.js Foundation",
		"Example News":                            "Example News",
	}
	i := 0
	paths := map[string]string{}
	for name := range cases {
		i++
		p := fmt.Sprintf("/s%d", i)
		paths[name] = p
		mux.HandleFunc(p, site(name))
	}
	newCardRig(t, mux)
	for name, want := range cases {
		c, err := build(t, "http://evil.test"+paths[name])
		if err != nil || c.Site != want {
			t.Errorf("site %q: got %q, want %q (err %v)", name, c.Site, want, err)
		}
	}
	// The link's own domain may name itself.
	if got := stripForeignDomains("Example · example.org", "example.org"); got != "Example · example.org" {
		t.Errorf("own domain removed: %q", got)
	}
	if got := stripForeignDomains("Rick Astley youtube·com", "youtube.com"); got != "Rick Astley youtube·com" {
		t.Errorf("own domain, written with a middle dot, removed: %q", got)
	}
}

// The masked line carries the registrable domain in plain text, where a v0.60
// reader prints it (v0.60 draws the title as a link and nothing else).
func TestLinkCardBlockCarriesDomain(t *testing.T) {
	s := cardService()
	for raw, want := range map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ": "[A title](https://www.youtube.com/watch?v=dQw4w9WgXcQ) · youtube.com",
		"https://paypa1-secure.evil.example/signin":   "[A title](https://paypa1-secure.evil.example/signin) · evil.example",
		"https://user.github.io/x":                    "[A title](https://user.github.io/x) · user.github.io",
		"https://bbc.co.uk/news":                      "[A title](https://bbc.co.uk/news) · bbc.co.uk",
		"http://93.184.216.34/a":                      "[A title](http://93.184.216.34/a)",
	} {
		c, err := s.finishCard(LinkCard{Title: "A title"}, nil, raw)
		if err != nil || c.Block != want {
			t.Errorf("%s: block %q, want %q (%v)", raw, c.Block, want, err)
		}
	}
}

// One failed fetch must not make the same link go out bare for ten minutes.
func TestLinkCardErrorIsForgottenSoon(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gone", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	newCardRig(t, mux)
	s := cardService()
	if _, err := s.LinkCard("http://site.test/gone", true); err == nil {
		t.Fatal("a 404 made a card")
	}
	e, ok := s.previews.cards["t|http://site.test/gone"]
	if !ok || e.err == nil {
		t.Fatal("the failure was not cached at all")
	}
	if left := time.Until(e.expires); left > cardErrTTL || left > time.Minute {
		t.Fatalf("a failure is remembered for %v", left)
	}
}

func TestCleanCardTextInvisibles(t *testing.T) {
	for in, want := range map[string]string{
		"pay\u00adpal":            "paypal",
		"a\u2060b\u2061c\u2064d":  "abcd",
		"x\u206ay\u180ez":         "xyz",
		"\u3164Name\uffa0":        "Name",
		"tag\U000E0041\U000E007F": "tag",
		"\u202eevil\u202c":        "evil",
		"family \U0001F468\u200d\U0001F469\u200d\U0001F467": "family \U0001F468\u200d\U0001F469\u200d\U0001F467",
		"\u2764\ufe0f":            "\u2764\ufe0f",
		"line\nbreak\tand  space": "line break and space",
	} {
		if got := cleanCardText(in, 200); got != want {
			t.Errorf("cleanCardText(%q) = %q, want %q", in, got, want)
		}
	}
}

// noHTTPTransport counts, and refuses, every request a reader's fetcher makes.
type noHTTPTransport struct{ n atomic.Int32 }

func (c *noHTTPTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, fmt.Errorf("a reader attempted an outbound request")
}

// The card rides the message from A to B. B gets it from the message alone,
// byte for byte, fetches the picture from A like any attachment, and makes no
// HTTP request; the page is not contacted again after A built the card.
// (zyads' TestEmbeddedPreviewArrivesWithMessage, carried over to the card
// block.)
func TestLinkCardArrivesWithMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/post", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page("Embedded Title", `<meta property="og:site_name" content="Example Site"><meta property="og:image" content="http://page.test/cover.jpg">`))
	})
	mux.HandleFunc("/cover.jpg", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(testJPEG(800, 450))
	})
	newCardRig(t, mux)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	b := startService(t, ctx)
	ct := &noHTTPTransport{}
	b.previews.client = &http.Client{Transport: ct}

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

	link := "http://page.test/post"
	c, err := a.LinkCard(link, true)
	if err != nil || !strings.Contains(c.Block, "\n![image](concord://attach/v2/") {
		t.Fatalf("sender built no card with a picture: %+v %v", c, err)
	}
	text := "worth a read " + link + "\n" + c.Block
	hits.Store(0)
	sent, err := a.send(channel, text, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var content string
	waitUntil(t, 20*time.Second, func() bool {
		for _, m := range mustMessages(t, b, channel) {
			if m.ID == sent.ID {
				content = m.Content
				return true
			}
		}
		return false
	}, "B never received the message")
	if content != text {
		t.Fatalf("B's copy differs from what A sent:\n%q\n%q", content, text)
	}
	m := regexp.MustCompile(`concord://attach/v2/([0-9a-f]{64})/([A-Za-z0-9_-]{75})/jpeg/`).FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("no thumbnail token in %q", content)
	}
	// Fetched the way the reader's client does: retried while A, the only
	// holder, is still being found (under -race that can take a moment).
	var jpg string
	var ferr error
	waitUntil(t, 20*time.Second, func() bool {
		jpg, ferr = b.FetchAttachment(channel, m[1], m[2], "jpeg")
		return ferr == nil
	}, "B could not fetch the card's picture from peers")
	if !strings.HasPrefix(jpg, "data:image/jpeg;base64,") {
		t.Fatalf("the card's picture is not a JPEG data URL: %.40q %v", jpg, ferr)
	}
	if ct.n.Load() != 0 || hits.Load() != 0 {
		t.Fatalf("reading made HTTP requests: client=%d server=%d", ct.n.Load(), hits.Load())
	}
}
