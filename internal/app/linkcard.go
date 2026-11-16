package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	goimage "image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/text/unicode/norm"
)

// Link cards: a link's title, picture, site and length, fetched ONCE by the
// sender's device when the message is sent and carried inside it, so that the
// people reading it contact nobody until they tap.
//
// The card travels as markdown every Concord client already reads, appended to
// the message body (frontend/src/lib/linkcard.js reads it back):
//
//	[<title>](<url>)
//	![image](concord://attach/v2/<blob>/<keys>/jpeg/<w>x<h>/2/<name>/<desc>)
//
// The title is a masked link's text; the picture an ordinary sealed attachment
// with flag bit 1 (attachCardThumb) set; its description "<site>" or
// "<site> · <duration>". The masked line ends with " · <domain>", the link's
// registrable domain in plain text: a v0.60 reader draws the title as a link
// and would otherwise see no address anywhere, and a new reader hides the
// copy because it prints the domain it computes itself. Everything is inside Content, so the author's
// signature (recordsig.go: sha256 of Content) covers every word of it and a
// member serving history cannot edit a title. A v0.60 peer stores and relays
// the bytes untouched and draws a titled link with a captioned picture.
//
// This replaces the first sender-side form, a rider token
// "[lp](concord://lp/v1/<base64 JSON>)" after the text: a v0.60 reader printed
// it raw, several hundred characters of base64 under the message. New readers
// still strip such a rider (lib/linkembed.js) and draw no card from it.
//
// Fetching uses the preview fetcher's SSRF-guarded client (preview.go): every
// dial resolves the name itself and refuses loopback/private/link-local
// addresses, so every redirect hop is checked after DNS; no cookie jar, no
// environment proxy, a cap on every body and a deadline on the whole card.

// attachCardThumb is bit 1 of a v2 token's flag field: the picture is a link
// card's thumbnail. Older peers read only bit 0 (spoiler) and show it as an
// ordinary captioned picture, which is the intended fallback.
const attachCardThumb = 2

const (
	cardBudget    = 6 * time.Second // the whole card, every request included
	cardOEmbedCap = 64 << 10
	cardImageCap  = 2 << 20
	// A thumbnail is at most 480x270, and real og:images are about 1 MP. The
	// decoded frame is what costs memory (4 bytes a pixel, 8 for a 16-bit
	// PNG), so the header is checked against these before anything decodes.
	cardMaxPixels  = 4 << 20
	cardMaxSide    = 4096
	cardThumbW     = 480
	cardThumbH     = 270
	cardThumbBytes = 48 << 10
	cardTitleMax   = 200
	cardSiteMax    = 80
	cardCacheTTL   = 10 * time.Minute
	// A failed fetch is remembered only briefly: one timeout must not make the
	// same link go out bare for the next ten minutes.
	cardErrTTL      = 30 * time.Second
	cardCacheMaxLen = 128
)

// The oEmbed and thumbnail endpoints. Variables so the tests can point them at
// a local server; nothing else assigns them.
var (
	ytOEmbedEndpoint    = "https://www.youtube.com/oembed"
	ytThumbBase         = "https://i.ytimg.com/vi/"
	vimeoOEmbedEndpoint = "https://vimeo.com/api/oembed.json"
)

// LinkCard is what the sender's device learned about a link.
type LinkCard struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Site     string `json:"site"`
	Duration string `json:"duration"`
	// Block is the exact text to append to the message (see the top of this
	// file); "" when there is nothing worth a card.
	Block string `json:"block"`

	thumb []byte // re-encoded JPEG, nil for none
	w, h  int
}

type cardEntry struct {
	c       LinkCard
	err     error
	expires time.Time
}

var (
	ytIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	vimeoIDRe = regexp.MustCompile(`^/(\d{1,12})/?$`)
	// The text of a title that reads as a web address: a bare host of two or
	// more labels with an optional path after it.
	hostishRe = regexp.MustCompile(`^(?i)(?:www\.)?((?:[\p{L}\p{N}-]+\.)+[\p{L}\p{N}-]{2,})(?::\d+)?(?:[/?#]\S*)?$`)
)

// LinkCard builds the card for one link. withThumb=false skips the picture (a
// disappearing message gets a title-only card: a sealed blob would outlive
// it). An error, or a card with an empty Block, means "send the message as
// typed".
func (s *Service) LinkCard(rawURL string, withThumb bool) (LinkCard, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return LinkCard{}, fmt.Errorf("app: bad url: %w", err)
	}
	u.Fragment = ""
	if err := validatePreviewURL(u); err != nil {
		return LinkCard{}, fmt.Errorf("app: %w", err)
	}
	key := u.String()
	if withThumb {
		key = "t|" + key
	}

	pc := s.previews
	pc.mu.Lock()
	if e, ok := pc.cards[key]; ok && time.Now().Before(e.expires) {
		pc.mu.Unlock()
		return s.finishCard(e.c, e.err, rawURL)
	}
	pc.mu.Unlock()

	v, err, _ := pc.flight.Do("card|"+key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), cardBudget)
		defer cancel()
		c, err := pc.buildCard(ctx, u, withThumb)
		pc.mu.Lock()
		if len(pc.cards) >= cardCacheMaxLen {
			for k := range pc.cards {
				delete(pc.cards, k)
				break
			}
		}
		ttl := cardCacheTTL
		if err != nil {
			ttl = cardErrTTL
		}
		pc.cards[key] = cardEntry{c: c, err: err, expires: time.Now().Add(ttl)}
		pc.mu.Unlock()
		return c, err
	})
	c, _ := v.(LinkCard)
	return s.finishCard(c, err, rawURL)
}

// finishCard seals the thumbnail (a fresh blob per message, like any picture)
// and writes the block. The URL in the block is the one the user typed, as
// the card describes exactly that link.
func (s *Service) finishCard(c LinkCard, err error, rawURL string) (LinkCard, error) {
	if err != nil {
		return LinkCard{}, err
	}
	c.URL = strings.TrimSpace(rawURL)
	if c.Title == "" {
		return c, nil
	}
	link := cardURL(c.URL)
	if link == "" {
		return c, nil
	}
	block := "[" + escapeCardTitle(c.Title) + "](" + link + ")"
	if u, err := url.Parse(c.URL); err == nil {
		if d := cardDomain(u); d != "" {
			block += " · " + d
		}
	}
	if len(c.thumb) > 0 {
		blobID, keys, err := s.sealBlob(c.thumb)
		if err == nil {
			desc := c.Site
			if c.Duration != "" {
				if desc == "" {
					desc = "Video"
				}
				desc += " · " + c.Duration
			}
			block += fmt.Sprintf("\n![image](concord://attach/v2/%s/%s/jpeg/%dx%d/%d/%s/%s)",
				blobID, keys, c.w, c.h, attachCardThumb, "", b64url.EncodeToString([]byte(clampBytes(desc, maxAttachDescLen))))
		}
	}
	c.Block = block
	return c, nil
}

// cardURL is the URL as it can sit inside "(…)" in a masked link on every
// client: the masked-link pattern ends at whitespace or ")", so those are
// percent-encoded. "" when the URL cannot be made to fit.
func cardURL(raw string) string {
	r := strings.NewReplacer("(", "%28", ")", "%29", " ", "%20")
	out := r.Replace(raw)
	if strings.ContainsAny(out, "\n\r\t") || !strings.HasPrefix(out, "http") {
		return ""
	}
	return out
}

// escapeCardTitle backslash-escapes what the markdown renderer would otherwise
// read as formatting inside the title, on every client including v0.60 (which
// honours the same escapes). Only what is actually live is escaped, so the
// title still reads cleanly in notifications and plain previews.
func escapeCardTitle(t string) string {
	var b strings.Builder
	rs := []rune(t)
	for i, r := range rs {
		next := rune(0)
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		switch r {
		case '\\', '[', ']', '`', '*', '_', '~', '|', '{', '}':
			b.WriteRune('\\')
		case '@', '#':
			if next == '_' || next == '-' || isWordRune(next) {
				b.WriteRune('\\')
			}
		case ':':
			if isWordRune(next) {
				b.WriteRune('\\')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// buildCard fetches what it can inside ctx.
func (pc *previewCache) buildCard(ctx context.Context, u *url.URL, withThumb bool) (LinkCard, error) {
	var c LinkCard
	var thumbURL string
	crop := false
	switch {
	case youtubeVideoID(u) != "":
		id := youtubeVideoID(u)
		var oe oembed
		q := url.Values{"format": {"json"}, "url": {"https://www.youtube.com/watch?v=" + id}}
		if err := pc.getJSON(ctx, ytOEmbedEndpoint+"?"+q.Encode(), &oe); err != nil {
			return c, err
		}
		c.Title, c.Site = oe.Title, oe.AuthorName
		// Rebuilt from the validated id, not taken from the response: hqdefault
		// is 4:3 with the 16:9 picture letterboxed inside it, so it is cropped.
		thumbURL, crop = ytThumbBase+id+"/hqdefault.jpg", true
	case vimeoVideo(u):
		var oe oembed
		q := url.Values{"url": {u.String()}}
		if err := pc.getJSON(ctx, vimeoOEmbedEndpoint+"?"+q.Encode(), &oe); err != nil {
			return c, err
		}
		c.Title, c.Site, thumbURL = oe.Title, oe.AuthorName, oe.ThumbnailURL
		if oe.Duration > 0 {
			c.Duration = fmtDuration(oe.Duration)
		}
	default:
		body, final, err := pc.getLimited(ctx, u.String(), "text/html", previewBodyCap, isHTML)
		if err != nil {
			return c, err
		}
		p := parsePreviewHTML(bytes.NewReader(body), final)
		c.Title, c.Site, thumbURL = p.Title, p.SiteName, p.ImageURL
		if secs, err := strconv.ParseFloat(p.Duration, 64); err == nil && secs >= 1 && secs < 100*3600 {
			c.Duration = fmtDuration(int(secs))
		}
	}
	c.Title = cleanCardText(c.Title, cardTitleMax)
	// The site or channel name is the page's own claim about itself, and the
	// card prints it right above the real domain: a page on evil.example
	// calling itself "Rick Astley - Topic · youtube.com" loses the address.
	c.Site = cleanCardText(stripForeignDomains(c.Site, cardDomain(u)), cardSiteMax)
	if !cardTitleOK(c.Title) {
		c.Title = ""
		return c, nil
	}
	if withThumb && thumbURL != "" {
		if tu, err := url.Parse(thumbURL); err == nil && validatePreviewURL(tu) == nil {
			if jpg, w, h, err := pc.thumbnail(ctx, tu.String(), crop); err == nil {
				c.thumb, c.w, c.h = jpg, w, h
			}
		}
	}
	return c, nil
}

type oembed struct {
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	ThumbnailURL string `json:"thumbnail_url"`
	Duration     int    `json:"duration"`
}

// youtubeVideoID returns the 11-character id of a YouTube watch/short/embed
// link on a real YouTube host, or "".
func youtubeVideoID(u *url.URL) string {
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	var id string
	switch {
	case host == "youtu.be":
		id = strings.Trim(u.Path, "/")
	case host == "youtube.com" || strings.HasSuffix(host, ".youtube.com"):
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(u.Path, "/shorts/"), strings.HasPrefix(u.Path, "/embed/"), strings.HasPrefix(u.Path, "/live/"):
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 {
				id = parts[1]
			}
		}
	}
	if !ytIDRe.MatchString(id) {
		return ""
	}
	return id
}

func vimeoVideo(u *url.URL) bool {
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return (host == "vimeo.com" || host == "www.vimeo.com") && vimeoIDRe.MatchString(u.Path)
}

func fmtDuration(secs int) string {
	h, m, s := secs/3600, secs/60%60, secs%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// cleanCardText strips what reorders or hides text, collapses whitespace and
// caps the length in runes. Stripped: C0/C1 controls, every format character
// (Cf: bidi controls, zero-width space, soft hyphen, word joiner, tag
// characters, U+180E…) and the other default-ignorable code points (Hangul
// fillers and the like). Kept: the zero-width (non-)joiners, which emoji
// sequences and Persian need, and variation selectors, which pick an emoji's
// colour form. The address checks look through all of those (foldForCheck).
func cleanCardText(s string, max int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case r == '\u200c' || r == '\u200d' || unicode.Is(unicode.Variation_Selector, r):
		case unicode.IsSpace(r):
			space = true
			continue
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r):
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return truncateRunes(b.String(), max)
}

// foldForCheck is text as an address check must read it: NFKC (fullwidth
// "ｐａｙｐａｌ．ｃｏｍ" is paypal.com), the full stops a browser's URL parser
// treats as dots made dots (U+3002 and its halfwidth and small forms), and
// every default-ignorable character dropped, joiners included — "pay\u00adpal.com"
// with a soft hyphen reads as paypal.com on screen. The reader does the same
// (frontend/src/lib/regdomain.js foldForCheck).
func foldForCheck(s string) string {
	s = norm.NFKC.String(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\u3002' || r == '\uff0e' || r == '\uff61' || r == '\ufe52':
			b.WriteByte('.')
		case unicode.IsControl(r) && !unicode.IsSpace(r),
			unicode.Is(unicode.Cf, r),
			unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r),
			unicode.Is(unicode.Variation_Selector, r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cardDomain is the registrable domain (eTLD+1) of a link's host, in ASCII
// form — what every reader prints under a card. "" for an IP literal or a
// host that is itself a public suffix.
func cardDomain(u *url.URL) string {
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	if a, err := idna.Lookup.ToASCII(host); err == nil {
		host = a
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	return d
}

// namedDomain is the registrable domain a piece of text names when it reads
// as a web address ("youtube.com", "www.youtube.com/watch", "https://x.org"),
// or "" when it does not. Text with a space in it is words, not an address;
// without a scheme or "www." the last label has to be a top-level domain the
// list knows, so "Node.js" is a name and not a place. The text is folded
// first. lib/regdomain.js textDomain is the reader's copy of this rule.
func namedDomain(text string) string {
	t := strings.ToLower(strings.TrimSpace(foldForCheck(text)))
	if t == "" || strings.ContainsAny(t, " \t\r\n") {
		return ""
	}
	explicit := false
	if i := strings.Index(t, "://"); i > 0 {
		t, explicit = t[i+3:], true
	}
	if i := strings.IndexAny(t, "/?#"); i >= 0 {
		t = t[:i]
	}
	if i := strings.LastIndexByte(t, '@'); i >= 0 && explicit {
		t = t[i+1:]
	}
	if i := strings.LastIndexByte(t, ':'); i >= 0 {
		t = t[:i]
	}
	host := strings.TrimSuffix(t, ".")
	explicit = explicit || strings.HasPrefix(host, "www.")
	if a, err := idna.Lookup.ToASCII(host); err == nil {
		host = a
	}
	if !hostLabelsRe.MatchString(host) {
		if explicit && host != "" {
			return host // plainly meant as an address; whatever it is, it is not ours
		}
		return ""
	}
	if ps, icann := publicsuffix.PublicSuffix(host); !explicit && !icann && !strings.Contains(ps, ".") {
		return ""
	}
	if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return d
	}
	return host
}

var hostLabelsRe = regexp.MustCompile(`^(?:[\p{L}\p{N}-]+\.)+[\p{L}\p{N}-]{2,}$`)

// domainishRe finds stretches of text that read as a web address, inside
// other words too: "Topic·youtube.com" holds one.
var domainishRe = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*://)?(?:[\p{L}\p{N}-]+\.)+[\p{L}\p{N}-]{2,}(?:[/?#]\S*)?`)

// separatorEdgeRe and separatorRunRe tidy what removing an address leaves.
var (
	separatorEdgeRe = regexp.MustCompile(`^[\s·•|:,;/\-–—]+|[\s·•|:,;/\-–—]+$`)
	separatorRunRe  = regexp.MustCompile(`\s*[·•|]\s*(?:[·•|]\s*)+`)
)

// An address can also be written with something that only looks like its dot
// between the labels: a middle dot or one of its cousins ("youtube·com",
// "youtube‧com"), or the "[.]" people write to keep a link from being one.
// These are not dots to a URL parser, and in other text they are ordinary
// punctuation ("col·lecció"), so foldForCheck leaves them. Instead each run
// of words joined by them is read label by label: wherever its labels, read
// as joined by dots, end in a registrable domain that is not the link's,
// those labels go and the words before them stay ("Topic·youtube·com" keeps
// "Topic"). The reader does the same (lib/regdomain.js stripLookalikeRun).
const lookalikeDot = `(?:[\x{00b7}\x{2027}\x{2219}\x{22c5}\x{30fb}\x{ff65}]|\[\.\]|\(\.\)|\{\.\})`

var (
	lookalikeRunRe = regexp.MustCompile(`[\p{L}\p{N}-]+(?:` + lookalikeDot + `[\p{L}\p{N}-]+)+`)
	lookalikeDotRe = regexp.MustCompile(lookalikeDot)
)

func stripLookalikeRun(run, dest string) string {
	labels := lookalikeDotRe.Split(run, -1)
	seps := lookalikeDotRe.FindAllString(run, -1)
	gone := make([]bool, len(labels))
	stripped := false
	for j := len(labels) - 1; j >= 1; j-- {
		d := namedDomain(strings.Join(labels[:j+1], "."))
		if d == "" {
			continue
		}
		k := min(strings.Count(d, ".")+1, j+1)
		if d != dest {
			for i := j - k + 1; i <= j; i++ {
				gone[i] = true
			}
			stripped = true
		}
		j -= k - 1
	}
	if !stripped {
		return run
	}
	var b strings.Builder
	if !gone[0] {
		b.WriteString(labels[0])
	}
	for i := 1; i < len(labels); i++ {
		if gone[i] && gone[i-1] {
			continue
		}
		// A separator next to an address taken out becomes a plain one,
		// which the tidying after collapses or trims.
		if gone[i] || gone[i-1] {
			b.WriteString(" · ")
		} else {
			b.WriteString(seps[i-1])
		}
		if !gone[i] {
			b.WriteString(labels[i])
		}
	}
	return b.String()
}

// stripForeignDomains removes from text every address that names a site other
// than dest, the link's own registrable domain. A channel or site name is the
// page's claim about itself; the only address a card may print is the one
// computed from the URL. Text with nothing to remove is returned unchanged;
// otherwise the folded form, with the addresses taken out, is what remains.
func stripForeignDomains(text, dest string) string {
	folded := foldForCheck(text)
	hit := false
	out := domainishRe.ReplaceAllStringFunc(folded, func(tok string) string {
		d := namedDomain(tok)
		if d == "" || d == dest {
			return tok
		}
		hit = true
		return ""
	})
	out = lookalikeRunRe.ReplaceAllStringFunc(out, func(run string) string {
		r := stripLookalikeRun(run, dest)
		hit = hit || r != run
		return r
	})
	if !hit {
		return text
	}
	out = strings.Join(strings.Fields(out), " ")
	out = separatorRunRe.ReplaceAllString(out, " · ")
	return strings.TrimSpace(separatorEdgeRe.ReplaceAllString(out, ""))
}

// cardTitleOK refuses a title that is no title: empty, an address, or text
// that names a different site from the one the link goes to — a page titled
// "paypal.com" on evil.example must not become a link whose face says
// paypal.com. The reader applies the same rule (lib/regdomain.js), so a card
// that slipped past here would still be refused there.
func cardTitleOK(title string) bool {
	if title == "" {
		return false
	}
	// Read as the eye reads it: invisible characters and fullwidth or
	// ideographic look-alikes do not stop "paypal.com" from naming paypal.com.
	title = foldForCheck(title)
	lower := strings.ToLower(title)
	if strings.Contains(lower, "://") || strings.Contains(lower, "www.") {
		return false
	}
	// Wrapping punctuation is not part of what the words name (the reader's
	// textDomain trims the same set): "*paypal.com*" names paypal.com.
	bare := strings.TrimLeft(strings.TrimRight(title, " \"'`*_~|)]}>\u00bb\u201d\u2019.,:;!?"), " \"'`*_~|([{<\u00ab\u201c\u2018")
	m := hostishRe.FindStringSubmatch(bare)
	if m == nil {
		return true
	}
	host := strings.ToLower(m[1])
	if ps, icann := publicsuffix.PublicSuffix(host); !icann && !strings.Contains(ps, ".") {
		// The last label is no TLD the list knows ("Node.js"): a name, not a place.
		return true
	}
	// It reads as an address. Naming another site is the phish this rule
	// exists for; naming the destination itself only repeats the domain the
	// card prints anyway. Neither is a title.
	return false
}

func isHTML(ct string) bool { return ct == "text/html" }

func isJSON(ct string) bool {
	return ct == "application/json" || ct == "text/json" || ct == "text/javascript" || ct == "application/json+oembed"
}

// getLimited GETs rawURL with the guarded client and returns at most max
// bytes of a 200 response whose media type passes ok. A body longer than max
// is refused rather than cut, except for HTML, whose head is all a card reads.
func (pc *previewCache) getLimited(ctx context.Context, rawURL, accept string, max int64, ok func(string) bool) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", previewUserAgent)
	req.Header.Set("Accept", accept)
	select {
	case pc.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	defer func() { <-pc.sem }()
	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("app: card fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("app: card fetch: HTTP %d", resp.StatusCode)
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !ok(strings.ToLower(mt)) {
		return nil, nil, fmt.Errorf("app: card fetch: unexpected content type %q", mt)
	}
	if resp.ContentLength > max && !isHTML(mt) {
		return nil, nil, fmt.Errorf("app: card fetch: body too large")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, nil, fmt.Errorf("app: card fetch: %w", err)
	}
	if int64(len(body)) > max {
		if !isHTML(mt) {
			return nil, nil, fmt.Errorf("app: card fetch: body too large")
		}
		body = body[:max]
	}
	return body, resp.Request.URL, nil
}

func (pc *previewCache) getJSON(ctx context.Context, rawURL string, out any) error {
	body, _, err := pc.getLimited(ctx, rawURL, "application/json", cardOEmbedCap, isJSON)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("app: card fetch: bad oEmbed: %w", err)
	}
	return nil
}

// thumbnail fetches an image and returns it re-encoded: decoded only after its
// header says it is a sane size, scaled down to fit cardThumbW×cardThumbH, and
// written as a JPEG no larger than cardThumbBytes. Whatever the page served —
// metadata, a polyglot, an animation — none of its bytes leave this function.
func (pc *previewCache) thumbnail(ctx context.Context, rawURL string, crop169 bool) ([]byte, int, int, error) {
	body, _, err := pc.getLimited(ctx, rawURL, "image/jpeg, image/png, image/gif", cardImageCap, func(ct string) bool {
		return ct == "image/jpeg" || ct == "image/png" || ct == "image/gif"
	})
	if err != nil {
		return nil, 0, 0, err
	}
	return cleanCardImage(ctx, body, crop169)
}

// cleanCardImage is the re-encode half of thumbnail, for bytes already
// fetched: only the decoded pixels are written out again, so no metadata the
// page's picture carried survives.
func cleanCardImage(ctx context.Context, body []byte, crop169 bool) ([]byte, int, int, error) {
	// The header alone says how big the decoded frame would be; nothing is
	// decoded until that passes.
	cfg, format, err := goimage.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("app: card image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > cardMaxSide || cfg.Height > cardMaxSide ||
		cfg.Width*cfg.Height > cardMaxPixels {
		return nil, 0, 0, fmt.Errorf("app: card image: %dx%d is too large", cfg.Width, cfg.Height)
	}
	// One decode at a time, process-wide: four cards sent at once would
	// otherwise hold four decoded frames.
	select {
	case cardDecodeSem <- struct{}{}:
	case <-ctx.Done():
		return nil, 0, 0, ctx.Err()
	}
	defer func() { <-cardDecodeSem }()
	n := cardDecoding.Add(1)
	defer cardDecoding.Add(-1)
	if cardDecodeObserve != nil {
		cardDecodeObserve(n)
	}
	var img goimage.Image
	switch format {
	case "jpeg":
		img, err = jpeg.Decode(bytes.NewReader(body))
	case "png":
		img, err = png.Decode(bytes.NewReader(body))
	case "gif":
		img, err = gif.Decode(bytes.NewReader(body))
	default:
		err = fmt.Errorf("app: card image: unsupported format %q", format)
	}
	if err != nil {
		return nil, 0, 0, err
	}
	src := img.Bounds()
	if crop169 {
		if h := src.Dx() * 9 / 16; h > 0 && h < src.Dy() {
			top := src.Min.Y + (src.Dy()-h)/2
			src = goimage.Rect(src.Min.X, top, src.Max.X, top+h)
		}
	}
	w, h := fitWithin(src.Dx(), src.Dy(), cardThumbW, cardThumbH)
	dst := downscale(img, src, w, h)
	for _, q := range []int{78, 68, 56, 44} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: q}); err != nil {
			return nil, 0, 0, err
		}
		if buf.Len() <= cardThumbBytes {
			return buf.Bytes(), w, h, nil
		}
	}
	return nil, 0, 0, fmt.Errorf("app: card image: does not fit in %d bytes", cardThumbBytes)
}

var (
	cardDecodeSem = make(chan struct{}, 1)
	// cardDecoding counts decodes in progress; cardDecodeObserve, when a test
	// sets it, is told the count as each decode starts. Nothing else sets it.
	cardDecoding      atomic.Int32
	cardDecodeObserve func(int32)
)

// fitWithin scales w×h down (never up) to fit inside maxW×maxH.
func fitWithin(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return w, h
	}
	if w*maxH > h*maxW {
		return maxW, max(1, h*maxW/w)
	}
	return max(1, w*maxH/h), maxH
}

// downscale is a box filter: each output pixel is the mean of the source
// pixels under it, each laid on white first since a JPEG has no alpha. It
// reads the decoded image in place; the first version copied the whole frame
// onto a white canvas first, a second full-size frame for nothing.
func downscale(img goimage.Image, src goimage.Rectangle, w, h int) *goimage.RGBA {
	px := pixelOverWhite(img)
	out := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
	sw, sh := src.Dx(), src.Dy()
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var r, g, b, n uint32
			for yy := src.Min.Y + y0; yy < src.Min.Y+y1; yy++ {
				for xx := src.Min.X + x0; xx < src.Min.X+x1; xx++ {
					pr, pg, pb := px(xx, yy)
					r += pr
					g += pg
					b += pb
					n++
				}
			}
			o := out.Pix[y*out.Stride+x*4:]
			o[0], o[1], o[2], o[3] = uint8(r/n), uint8(g/n), uint8(b/n), 255
		}
	}
	return out
}

// pixelOverWhite returns a reader of one pixel as 8-bit RGB composited over
// white. The decoders' own types are read straight from their buffers; any
// other image goes through At, which is slower but the same answer.
func pixelOverWhite(img goimage.Image) func(x, y int) (uint32, uint32, uint32) {
	switch m := img.(type) {
	case *goimage.YCbCr:
		return func(x, y int) (uint32, uint32, uint32) {
			c := m.YCbCrAt(x, y)
			r, g, b := color.YCbCrToRGB(c.Y, c.Cb, c.Cr)
			return uint32(r), uint32(g), uint32(b)
		}
	case *goimage.Gray:
		return func(x, y int) (uint32, uint32, uint32) {
			v := uint32(m.Pix[m.PixOffset(x, y)])
			return v, v, v
		}
	case *goimage.RGBA: // premultiplied
		return func(x, y int) (uint32, uint32, uint32) {
			p := m.Pix[m.PixOffset(x, y):]
			k := 255 - uint32(p[3])
			return uint32(p[0]) + k, uint32(p[1]) + k, uint32(p[2]) + k
		}
	case *goimage.NRGBA:
		return func(x, y int) (uint32, uint32, uint32) {
			p := m.Pix[m.PixOffset(x, y):]
			a := uint32(p[3])
			k := 255 - a
			return uint32(p[0])*a/255 + k, uint32(p[1])*a/255 + k, uint32(p[2])*a/255 + k
		}
	case *goimage.NRGBA64:
		return func(x, y int) (uint32, uint32, uint32) {
			c := m.NRGBA64At(x, y)
			a := uint32(c.A)
			k := 0xffff - a
			return (uint32(c.R)*a/0xffff + k) >> 8, (uint32(c.G)*a/0xffff + k) >> 8, (uint32(c.B)*a/0xffff + k) >> 8
		}
	}
	return func(x, y int) (uint32, uint32, uint32) {
		r, g, b, a := img.At(x, y).RGBA() // premultiplied, 16-bit
		k := 0xffff - a
		return (r + k) >> 8, (g + k) >> 8, (b + k) >> 8
	}
}
