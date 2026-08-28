package app

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
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
	// Decorations that ride in FRONT of ordinary words: a send effect, a
	// disappearing timer, a sealed timestamp. Never the message.
	snRiderRe = regexp.MustCompile(`\[(?:fx|eph|ts)\]\(concord://(?:fx|eph|ts)/v1/[A-Za-z0-9_-]+\)`)
	// Attachments, image and file, v1 and v2. Mirrors lib/attachments.js and
	// internal/store's own pair.
	snImageRe  = regexp.MustCompile(`!\[image\]\(concord://attach/v[12]/[^)\s]+\)`)
	snFileRe   = regexp.MustCompile(`\[file\]\(concord://file/v1/[^)\s]+\)`)
	snFenceRe  = regexp.MustCompile("(?s)```.*?(?:```|$)")
	snInlineRe = regexp.MustCompile("`([^`]*)`")
	snImgMdRe  = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	snLinkMdRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	snBold3Re  = regexp.MustCompile(`\*\*\*([^*]+)\*\*\*`)
	snBold2Re  = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	snItalicRe = regexp.MustCompile(`\*([^*]+)\*`)
	snUnder3Re = regexp.MustCompile(`___([^_]+)___`)
	snUnder2Re = regexp.MustCompile(`__([^_]+)__`)
	snStrikeRe = regexp.MustCompile(`~~([^~]+)~~`)
	snSpoilRe  = regexp.MustCompile(`\|\|([^|]+)\|\|`)
	snDanglRe  = regexp.MustCompile(`(\*\*|~~|\|\|)`)
	snHeadRe   = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	snQuoteRe  = regexp.MustCompile(`(?m)^\s*>\s?`)
	snListRe   = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d{1,3}\.)\s+`)
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
	if snGameRe.MatchString(s) {
		return labelGame
	}
	if snDoodleRe.MatchString(s) {
		return labelDoodle
	}
	if snSfxRe.MatchString(s) {
		return labelSound
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
func flattenMarkdown(s string) string {
	s = snInlineRe.ReplaceAllString(s, "$1")
	s = snImgMdRe.ReplaceAllString(s, " ")
	s = snLinkMdRe.ReplaceAllString(s, "$1")
	s = snBold3Re.ReplaceAllString(s, "$1")
	s = snBold2Re.ReplaceAllString(s, "$1")
	s = snItalicRe.ReplaceAllString(s, "$1")
	s = snUnder3Re.ReplaceAllString(s, "$1")
	s = snUnder2Re.ReplaceAllString(s, "$1")
	s = snStrikeRe.ReplaceAllString(s, "$1")
	s = snSpoilRe.ReplaceAllString(s, "$1")
	s = snDanglRe.ReplaceAllString(s, "")
	s = snHeadRe.ReplaceAllString(s, "")
	s = snQuoteRe.ReplaceAllString(s, "")
	s = snListRe.ReplaceAllString(s, "")
	return s
}

func collapse(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}
