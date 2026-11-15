package app

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// snippet.go — one message, one line, no source.
//
// The activity inbox cuts its previews here, in Go, before they reach a
// browser that could have flattened them. It used to be a whitespace collapse
// and nothing else, so the newest feature's first screen printed
//
//	![image](concord://attach/v2/20ed697d4012474c24a40a5de4f800133…
//
// at people, and two more rows showed literal backticks around `rules.apply`.
//
// The rule is the same one lib/snippet.js applies on the client: a body that
// IS a card is named, block content becomes a token, everything else is prose
// with its formatting flattened. The two files carry the same table of names
// deliberately — the alert-word matcher already lives twice for the same
// reason — because a label that drifts is a row that says something different
// depending on which half of the app wrote it. `TestSnippetMatchesClientRule`
// pins the shared cases.

var (
	snPollRe     = regexp.MustCompile(`\[poll\]\(concord://poll/v1/([A-Za-z0-9_-]+)\)`)
	snAnnounceRe = regexp.MustCompile(`\[announcement\]\(concord://announce/v1/([A-Za-z0-9_-]+)\)`)
	snGameRe     = regexp.MustCompile(`\[game\]\(concord://game/v1/[A-Za-z0-9_-]+\)`)
	snDoodleRe   = regexp.MustCompile(`\[doodle\]\(concord://doodle/v1/[A-Za-z0-9_-]+\)`)
	snSfxRe      = regexp.MustCompile(`\[sound\]\(concord://sfx/v1/[A-Za-z0-9_-]+\)`)
	snEventRe    = regexp.MustCompile(`\[event\]\(concord://event/v1/([A-Za-z0-9_-]+)\)`)
	// Decorations that ride in FRONT of ordinary words: a send effect, a
	// disappearing timer, a sealed timestamp. Never the message.
	snRiderRe = regexp.MustCompile(`\[(?:fx|eph|ts|lp)\]\(concord://(?:fx|eph|ts|lp)/v1/[A-Za-z0-9_-]+\)`)
	// Attachments, image and file, v1 and v2. Mirrors lib/attachments.js and
	// internal/store's own pair.
	snImageRe  = regexp.MustCompile(`!\[image\]\(concord://attach/v[12]/[^)\s]+\)`)
	snFileRe   = regexp.MustCompile(`\[file\]\(concord://file/v1/[^)\s]+\)`)
	snFenceRe  = regexp.MustCompile("(?s)```.*?(?:```|$)")
	snInlineRe = regexp.MustCompile("`([^`]*)`")
	snImgMdRe  = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	snLinkMdRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	// A link card's lines (linkcard.go; lib/linkcard.js parseCard).
	snCardLineRe  = regexp.MustCompile(`^\[((?:\\.|[^\]\\\n])+)\]\((https?://[^\s)]+)\)(?: · (\S+))?$`)
	snCardThumbRe = regexp.MustCompile(`^!\[image\]\(concord://attach/v2/[^\s)/]+/[^\s)/]+/[^\s)/]+/[^\s)/]+/(\d+)/[^\s)]*\)$`)
	snUnescRe     = regexp.MustCompile(`\\(.)`)
	snBold3Re     = regexp.MustCompile(`\*\*\*([^*]+)\*\*\*`)
	snBold2Re     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	snItalicRe    = regexp.MustCompile(`\*([^*]+)\*`)
	snUnder3Re    = regexp.MustCompile(`___([^_]+)___`)
	snUnder2Re    = regexp.MustCompile(`__([^_]+)__`)
	snStrikeRe    = regexp.MustCompile(`~~([^~]+)~~`)
	snSpoilRe     = regexp.MustCompile(`\|\|([^|]+)\|\|`)
	snDanglRe     = regexp.MustCompile(`(\*\*|~~|\|\|)`)
	snHeadRe      = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	snQuoteRe     = regexp.MustCompile(`(?m)^\s*>\s?`)
	snListRe      = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d{1,3}\.)\s+`)
)

// The names a quoted card answers to. Short: this is a label in a one-line
// row, not a description. Kept byte-identical to TOKEN_LABELS in
// lib/snippet.js.
const (
	labelGame   = "🎲 game"
	labelDoodle = "🖌 doodle"
	labelSound  = "🔊 sound"
	labelCode   = "📄 code block"
)

func parseInviteSnippet(body string) (string, bool) {
	var n inviteNote
	if json.Unmarshal([]byte(strings.TrimSpace(body)), &n) != nil || n.Op == "" {
		return "", false
	}
	name := strings.TrimSpace(n.Guild)
	if name == "" {
		name = n.What
	}
	if name == "" {
		name = "invite"
	}
	if n.Op == "joined" {
		return "joined " + name, true
	}
	return "invite to " + name, true
}

func snB64(s string) []byte {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return nil
	}
	return b
}

// snippet is the bit of the message the inbox shows. One line, bounded, cut on
// a rune boundary so a truncated emoji does not become a replacement glyph.
func snippet(body string) string {
	const max = 160
	s := flattenBody(body)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

// flattenBody turns a message body into the one line a reader can read.
func flattenBody(body string) string {
	s := snRiderRe.ReplaceAllString(body, " ")

	// A body that IS a card is named rather than quoted. Poll and announcement
	// carry text worth showing; the rest are their own name. Both decodes can
	// fail on a payload something truncated, which is exactly when it matters
	// that the fallback is a label and not the base64 that is left.
	if m := snPollRe.FindStringSubmatch(s); m != nil {
		var p struct {
			Q string `json:"q"`
		}
		if err := json.Unmarshal(snB64(m[1]), &p); err == nil && p.Q != "" {
			return "📊 " + collapse(p.Q)
		}
		return "📊 Poll"
	}
	if m := snAnnounceRe.FindStringSubmatch(s); m != nil {
		var a struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(snB64(m[1]), &a); err == nil && a.Body != "" {
			return "📣 " + collapse(flattenMarkdown(a.Body))
		}
		return "📣 Announcement"
	}
	if m := snEventRe.FindStringSubmatch(s); m != nil {
		var e struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(snB64(m[1]), &e); err == nil && e.Title != "" {
			return "📅 " + collapse(e.Title)
		}
		return "📅 Event"
	}
	if snGameRe.MatchString(s) {
		return labelGame
	}
	if snDoodleRe.MatchString(s) {
		return labelDoodle
	}
	if snSfxRe.MatchString(s) {
		return labelSound
	}
	if note, ok := parseInviteSnippet(s); ok {
		return note
	}

	// A link card: the words around it if there are any, otherwise its title
	// and where it goes. Its picture is the card's, not an image the message
	// carries, so it never reads as "🖼 image".
	if title, dom, rest, ok := snCard(s); ok {
		if strings.TrimSpace(snRiderRe.ReplaceAllString(rest, "")) == "" {
			return "🔗 " + title + " · " + dom
		}
		s = rest
	}

	// Block content inside prose becomes a token too. A fenced block flattened
	// into one line is eleven lines of somebody's code with the newlines taken
	// out, which is a preview of nothing.
	s = snFenceRe.ReplaceAllString(s, " "+labelCode+" ")

	// Attachments to a placeholder. The caption, if there is one, is the
	// preview; otherwise the placeholder is.
	hasImage := snImageRe.MatchString(s)
	hasFile := snFileRe.MatchString(s)
	if hasImage || hasFile {
		s = snImageRe.ReplaceAllString(s, " ")
		s = snFileRe.ReplaceAllString(s, " ")
		rest := collapse(flattenMarkdown(s))
		glyph, label := "🖼", "image"
		if hasFile && !hasImage {
			glyph, label = "📎", "file"
		}
		if rest != "" {
			return glyph + " " + rest
		}
		return glyph + " " + label
	}
	return collapse(flattenMarkdown(s))
}

// flattenMarkdown reduces formatting to the words inside it. Paired markers
// only, so snake_case and 3 * 4 survive, then a sweep for the multi-character
// markers a truncated excerpt can leave dangling.
const spoilerMask = "[spoiler]"

func flattenMarkdown(s string) string {
	s = snInlineRe.ReplaceAllString(s, "$1")
	s = snImgMdRe.ReplaceAllString(s, " ")
	s = snLinkMdRe.ReplaceAllStringFunc(s, snMasked)
	s = snBold3Re.ReplaceAllString(s, "$1")
	s = snBold2Re.ReplaceAllString(s, "$1")
	s = snItalicRe.ReplaceAllString(s, "$1")
	s = snUnder3Re.ReplaceAllString(s, "$1")
	s = snUnder2Re.ReplaceAllString(s, "$1")
	s = snStrikeRe.ReplaceAllString(s, "$1")
	// A spoiler stays covered: the inbox printed it in clear. Same mask as
	// SPOILER_MASK in frontend/src/lib/forum.js.
	s = snSpoilRe.ReplaceAllString(s, spoilerMask)
	s = snDanglRe.ReplaceAllString(s, "")
	s = snHeadRe.ReplaceAllString(s, "")
	s = snQuoteRe.ReplaceAllString(s, "")
	s = snListRe.ReplaceAllString(s, "")
	return s
}

func collapse(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

// LastSaid describes the newest thing said in a channel: the line a DM list
// shows under the name, whether it was us, and when. Flattened by the same rule
// the inbox uses, so a poll reads as a poll and an image as an image rather
// than as a token.
//
// One row read and one secretbox open per call. That is affordable for a
// handful of DMs on a guild-list refresh and would not be for every channel of
// every guild, which is why nothing else asks for it.
func (s *Service) LastSaid(channelID string) (text string, mine bool, atMillis int64) {
	m, ok, err := s.store.NewestMessage(channelID)
	if err != nil || !ok {
		return "", false, 0
	}
	if m.Deleted {
		return "", false, m.Sent.UnixMilli()
	}
	mine = accountFingerprintOf(m.Sender) == s.Fingerprint()
	if t, ok := callPreview(m.Kind, m.Content, mine); ok {
		// Never "You: …": the line already says who did what, from this side.
		return t, false, m.Sent.UnixMilli()
	}
	return snippet(m.Content), mine, m.Sent.UnixMilli()
}

// callPreview words a call record for the DM list the way the conversation
// words it (frontend/src/lib/callflow.js recordText). The raw content is a
// token for the renderer, and the caller's list used to read "You: Missed
// call" — about a call they placed and nobody missed.
func callPreview(kind, content string, mine bool) (string, bool) {
	switch kind {
	case "call-missed":
		switch {
		case content == "Declined" && mine:
			return "Call declined", true
		case content == "Declined":
			return "You declined a call", true
		case mine:
			return "You called — no answer", true
		default:
			return "Missed call", true
		}
	case "call":
		if rest, ok := strings.CutPrefix(content, "ended:"); ok {
			n, _ := strconv.Atoi(rest)
			return "Call ended · " + callLength(n), true
		}
		return "Missed call", true
	}
	return "", false
}

// callLength is lib/callflow.js callLength: "45s", "2m", "1h 5m".
func callLength(secs int) string {
	if secs < 60 {
		return strconv.Itoa(max(secs, 0)) + "s"
	}
	m := secs / 60
	if m < 60 {
		return strconv.Itoa(m) + "m"
	}
	if m%60 == 0 {
		return strconv.Itoa(m/60) + "h"
	}
	return strconv.Itoa(m/60) + "h " + strconv.Itoa(m%60) + "m"
}

// snMasked flattens one masked link. A link to the web keeps its words and
// gains its registrable domain after them, as the feed prints it: the words
// can say anything, and a notification reading "paypal.com" for a link to
// evil.example is the disguise the feed refuses. Words that are the address
// itself become the address.
func snMasked(m string) string {
	sub := snLinkMdRe.FindStringSubmatch(m)
	words, target := sub[1], sub[2]
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return words
	}
	if snSameAsURL(words, target) {
		return target
	}
	return words + " (" + snDomain(u) + ")"
}

// snDomain is what a reader prints for a link: its registrable domain, or the
// whole host where there is none (an address, a bare suffix).
func snDomain(u *url.URL) string {
	if d := cardDomain(u); d != "" {
		return d
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}

// snSameAsURL: the words are the URL give or take the scheme, a www. and a
// trailing slash (lib/regdomain.js sameAsURL).
func snSameAsURL(words, target string) bool {
	norm := func(s string) string {
		s = strings.TrimSpace(foldForCheck(s))
		if i := strings.Index(s, "://"); i > 0 && !strings.ContainsAny(s[:i], "/ ") {
			s = s[i+3:]
		}
		s = strings.TrimSuffix(s, "/")
		s = strings.ToLower(s)
		return strings.TrimPrefix(s, "www.")
	}
	return norm(words) == norm(target)
}

// snCard recognises a link card at the end of a body, by the rule the reader
// uses (lib/linkcard.js parseCard): an optional flagged picture last, then a
// masked link alone on its line, optionally followed by " · <its domain>",
// whose words are not an address of another site.
func snCard(body string) (title, dom, rest string, ok bool) {
	lines := strings.Split(strings.TrimRight(body, " \t\r\n"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if m := snCardThumbRe.FindStringSubmatch(last); m != nil {
		flags, _ := strconv.Atoi(m[1])
		if flags&attachCardThumb == 0 || flags&1 != 0 || len(lines) < 2 {
			return "", "", "", false
		}
		lines = lines[:len(lines)-1]
		last = strings.TrimSpace(lines[len(lines)-1])
	}
	m := snCardLineRe.FindStringSubmatch(last)
	if m == nil {
		return "", "", "", false
	}
	u, err := url.Parse(m[2])
	if err != nil || u.Host == "" {
		return "", "", "", false
	}
	dom = snDomain(u)
	if m[3] != "" && strings.ToLower(m[3]) != dom {
		return "", "", "", false
	}
	raw := snUnescRe.ReplaceAllString(m[1], "$1")
	title = cleanCardText(raw, cardTitleMax)
	if title == "" || snSameAsURL(title, m[2]) {
		return "", "", "", false
	}
	for _, t := range []string{raw, title} {
		if named := namedDomain(strings.Trim(t, " \"'`*_~|()[]{}<>«»“”‘’.,:;!?")); named != "" && named != dom {
			return "", "", "", false
		}
	}
	return title, dom, strings.Join(lines[:len(lines)-1], "\n"), true
}
