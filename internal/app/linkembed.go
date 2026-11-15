package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	stdimage "image"
	"image/color"
	"image/draw"
	_ "image/gif" // decoders for fetched preview images
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Sender-built link previews.
//
// The SENDER's device fetches the page once, while composing, and the message
// carries the result inside its encrypted body as a rider token appended after
// the text:
//
//	[lp](concord://lp/v1/<base64url(JSON array of LinkEmbed)>)
//
// Recipients render that and nothing else: they make no request to the linked
// site, for the page or for its image. The image is not a URL — it is
// downloaded by the sender, decoded, scaled down to fit linkEmbedImageMax and
// re-encoded as a JPEG (which discards every byte of the original's metadata),
// then sealed as an ordinary attachment blob. The token holds only the blob
// reference, so the picture travels exactly like an attachment: encrypted,
// content-addressed, fetched peer to peer from members.
//
// A peer that predates this rider shows it as a bare link labelled "lp" and
// ignores the payload; nothing about the message body itself changes.
//
// The token is inside the signed, encrypted body, so a relaying member cannot
// alter it. Receivers still validate it as hostile input (ParseLinkEmbeds and
// its twin lib/linkembed.js).

const (
	linkEmbedMax        = 3    // previews per message
	linkEmbedURLMax     = 500  // bytes of URL
	linkEmbedTitleMax   = 200  // runes
	linkEmbedDescMax    = 400  // runes
	linkEmbedSiteMax    = 100  // runes
	linkEmbedTokenMax   = 8192 // chars of base64 payload accepted on receipt
	linkEmbedImageMax   = 400  // px: longest side of the embedded picture
	linkEmbedImageFetch = 2 << 20
	linkEmbedPixelsMax  = 16_000_000 // refuse to decode anything bigger
	linkEmbedTTL        = 30 * time.Minute
)

// LinkEmbedImage is the blob reference of the embedded picture. It mirrors an
// attachment token's fields.
type LinkEmbedImage struct {
	Blob string `json:"b"` // blob id, 64 hex
	Keys string `json:"k"` // base64url key||nonce, 75 chars
	Type string `json:"f"` // jpeg
	W    int    `json:"w"`
	H    int    `json:"h"`
}

// LinkEmbed is one preview as it rides in a message.
type LinkEmbed struct {
	URL   string          `json:"u"`
	Title string          `json:"t,omitempty"`
	Desc  string          `json:"d,omitempty"`
	Site  string          `json:"s,omitempty"`
	Image *LinkEmbedImage `json:"i,omitempty"`
}

var (
	linkEmbedTokenRe = regexp.MustCompile(`\[lp\]\(concord://lp/v1/([A-Za-z0-9_-]+)\)`)
	linkRe           = regexp.MustCompile(`https?://[^\s<]+`)
	linkTrailRe      = regexp.MustCompile(`[).,;!?]+$`)
	codeFenceRe      = regexp.MustCompile("`[^`]*`")
)

type embedEntry struct {
	e       LinkEmbed
	err     error
	expires time.Time
}

// linkURLs pulls up to max distinct http(s) links from plain message text,
// ignoring code. It is the Go twin of extractLinks in lib/embeds.js.
func linkURLs(text string, max int) []string {
	parts := strings.Split(text, "```")
	var kept []string
	for i, p := range parts {
		if i%2 == 0 {
			kept = append(kept, p)
		}
	}
	scrubbed := codeFenceRe.ReplaceAllString(strings.Join(kept, " "), " ")
	var out []string
	seen := map[string]bool{}
	for _, m := range linkRe.FindAllString(scrubbed, -1) {
		m = linkTrailRe.ReplaceAllString(m, "")
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
		if len(out) >= max {
			break
		}
	}
	return out
}

// plainText makes a scraped or received string safe to show as text: control
// and bidi-override characters go, whitespace collapses, length is capped.
func plainText(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r):
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F, r == 0xFEFF:
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(out); len(r) > max {
		out = string(r[:max])
	}
	return out
}

// embeddableURL reports whether a URL may be carried or shown: absolute
// http/https with a host, no userinfo, bounded length.
func embeddableURL(raw string) (*url.URL, bool) {
	if raw == "" || len(raw) > linkEmbedURLMax {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, false
	}
	return u, true
}

// PrepareLinkPreviews is what the composer calls, debounced, once a link shows
// up in the draft. It fetches (this device only) up to three links' previews
// and returns the rider token to append to the message body, or "" when
// nothing could be built. It never fails the send: a page that will not answer
// simply has no preview.
func (s *Service) PrepareLinkPreviews(text string) string {
	urls := linkURLs(text, linkEmbedMax)
	if len(urls) == 0 {
		return ""
	}
	embeds := make([]*LinkEmbed, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			if e, err := s.linkEmbedFor(u); err == nil {
				embeds[i] = &e
			}
		}(i, u)
	}
	wg.Wait()
	var out []LinkEmbed
	for _, e := range embeds {
		if e != nil {
			out = append(out, *e)
		}
	}
	return encodeLinkEmbeds(out)
}

func encodeLinkEmbeds(es []LinkEmbed) string {
	if len(es) == 0 {
		return ""
	}
	raw, err := json.Marshal(es)
	if err != nil {
		return ""
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	if len(payload) > linkEmbedTokenMax {
		return "" // never emit what receivers would refuse
	}
	return "[lp](concord://lp/v1/" + payload + ")"
}

// linkEmbedFor builds (or serves from a short cache) one preview.
func (s *Service) linkEmbedFor(rawURL string) (LinkEmbed, error) {
	pc := s.previews
	now := time.Now()
	pc.mu.Lock()
	if c, ok := pc.embeds[rawURL]; ok && now.Before(c.expires) {
		pc.mu.Unlock()
		return c.e, c.err
	}
	pc.mu.Unlock()

	v, err, _ := pc.flight.Do("embed:"+rawURL, func() (any, error) {
		e, err := s.buildLinkEmbed(rawURL)
		ttl := linkEmbedTTL
		if err != nil {
			ttl = previewErrTTL
		}
		pc.mu.Lock()
		if len(pc.embeds) >= previewCacheCap {
			pc.embeds = map[string]embedEntry{}
		}
		pc.embeds[rawURL] = embedEntry{e: e, err: err, expires: time.Now().Add(ttl)}
		pc.mu.Unlock()
		return e, err
	})
	if err != nil {
		return LinkEmbed{}, err
	}
	return v.(LinkEmbed), nil
}

func (s *Service) buildLinkEmbed(rawURL string) (LinkEmbed, error) {
	if _, ok := embeddableURL(rawURL); !ok {
		return LinkEmbed{}, fmt.Errorf("app: link not embeddable")
	}
	p, err := s.LinkPreview(rawURL)
	if err != nil {
		return LinkEmbed{}, err
	}
	e := LinkEmbed{
		// The URL as written in the message, not the one the page redirected
		// to or claims: the card's click must open exactly the link the text
		// shows.
		URL:   rawURL,
		Title: plainText(p.Title, linkEmbedTitleMax),
		Desc:  plainText(p.Description, linkEmbedDescMax),
		Site:  plainText(p.SiteName, linkEmbedSiteMax),
	}
	if e.Title == "" && e.Desc == "" {
		return LinkEmbed{}, fmt.Errorf("app: nothing to preview")
	}
	if p.ImageURL != "" {
		if img, err := s.embedImage(p.ImageURL); err == nil {
			e.Image = img
		}
	}
	return e, nil
}

// embedImage downloads a preview picture on THIS device, cleans it and seals
// it as an attachment blob. Failure only costs the picture.
func (s *Service) embedImage(imageURL string) (*LinkEmbedImage, error) {
	pc := s.previews
	u, err := url.Parse(imageURL)
	if err != nil {
		return nil, err
	}
	if err := pc.validate(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", previewUserAgent)
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif")
	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("app: image HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, linkEmbedImageFetch+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > linkEmbedImageFetch {
		return nil, fmt.Errorf("app: preview image too large")
	}
	out, w, h, err := cleanPreviewImage(raw)
	if err != nil {
		return nil, err
	}
	blob, keys, err := s.sealBlob(out)
	if err != nil {
		return nil, err
	}
	return &LinkEmbedImage{Blob: blob, Keys: keys, Type: "jpeg", W: w, H: h}, nil
}

// cleanPreviewImage decodes a png/jpeg/gif, scales it to fit
// linkEmbedImageMax on its longest side and re-encodes it as JPEG over white.
// Re-encoding from decoded pixels is what strips EXIF/XMP/ICC and any trailing
// payload; nothing of the original container survives. The pixel count is
// checked from the header before anything is decoded.
func cleanPreviewImage(raw []byte) (out []byte, w, h int, err error) {
	cfg, _, err := stdimage.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("app: unsupported preview image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > linkEmbedPixelsMax {
		return nil, 0, 0, fmt.Errorf("app: preview image dimensions refused")
	}
	src, _, err := stdimage.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, err
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := sw, sh
	if sw > linkEmbedImageMax || sh > linkEmbedImageMax {
		if sw >= sh {
			dw, dh = linkEmbedImageMax, max(1, sh*linkEmbedImageMax/sw)
		} else {
			dw, dh = max(1, sw*linkEmbedImageMax/sh), linkEmbedImageMax
		}
	}
	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, dw, dh))
	draw.Draw(dst, dst.Bounds(), stdimage.NewUniform(color.White), stdimage.Point{}, draw.Src)
	// Area-average resample: each destination pixel is the mean of the source
	// block it covers, composited over the white backdrop.
	for y := 0; y < dh; y++ {
		y0, y1 := y*sh/dh, max(y*sh/dh+1, (y+1)*sh/dh)
		for x := 0; x < dw; x++ {
			x0, x1 := x*sw/dw, max(x*sw/dw+1, (x+1)*sw/dw)
			var r, g, bl, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					pr, pg, pb, pa := src.At(b.Min.X+xx, b.Min.Y+yy).RGBA() // premultiplied, 16-bit
					r += uint64(pr) + uint64(0xffff-pa)
					g += uint64(pg) + uint64(0xffff-pa)
					bl += uint64(pb) + uint64(0xffff-pa)
					n++
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 78}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), dw, dh, nil
}

// StripLinkEmbed removes the rider token from a message body.
func StripLinkEmbed(content string) string {
	return strings.TrimSpace(linkEmbedTokenRe.ReplaceAllString(content, ""))
}

// ParseLinkEmbeds reads the previews a RECEIVED message carries. The payload
// came from another member, so every field is treated as hostile: sizes are
// capped, text is reduced to plain text, URLs must be http/https, and a
// preview survives only if its URL is exactly one of the links in the message
// text — a card can never point somewhere the words do not. It performs no
// I/O of any kind.
func ParseLinkEmbeds(content string) []LinkEmbed {
	m := linkEmbedTokenRe.FindStringSubmatch(content)
	if m == nil || len(m[1]) > linkEmbedTokenMax {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(m[1])
	if err != nil {
		return nil
	}
	var in []LinkEmbed
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil
	}
	body := linkEmbedTokenRe.ReplaceAllString(content, " ")
	inText := map[string]bool{}
	for _, l := range linkURLs(body, 50) {
		inText[l] = true
	}
	var out []LinkEmbed
	for _, e := range in {
		if len(out) >= linkEmbedMax {
			break
		}
		if _, ok := embeddableURL(e.URL); !ok || !inText[e.URL] {
			continue
		}
		clean := LinkEmbed{
			URL:   e.URL,
			Title: plainText(e.Title, linkEmbedTitleMax),
			Desc:  plainText(e.Desc, linkEmbedDescMax),
			Site:  plainText(e.Site, linkEmbedSiteMax),
		}
		if clean.Title == "" && clean.Desc == "" {
			continue
		}
		if i := e.Image; i != nil && blobIDRe.MatchString(i.Blob) && len(i.Keys) == 75 &&
			i.Type == "jpeg" && i.W > 0 && i.W <= linkEmbedImageMax && i.H > 0 && i.H <= linkEmbedImageMax {
			clean.Image = i
		}
		out = append(out, clean)
	}
	return out
}
