package chronimport

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Imported text is the only text in Concord that nobody in the guild wrote and
// nobody in the guild reviewed. Everything else on the render path was typed by
// an authenticated member into their own client; an archive is a decade of
// strings from a system with different rules, arriving in bulk, on the word of
// one person who probably has not read them either.
//
// So it gets a gate the live path does not have. Not because the renderer is
// unsafe — it escapes first and layers a fixed tag set on top, which is why
// there is no HTML-injection hole to plug here — but because ESCAPING IS NOT
// THE ONLY THING A STRING CAN DO. The renderer interprets a handful of in-band
// forms that mean something to this application, and an imported body carrying
// one of them is an instruction, not a quotation.
//
// Three of those matter enough to name:
//
//   - concord:// tokens. The message body is where this app carries structured
//     content: attachments, polls, embeds, sealed timestamps, screen effects —
//     and `[eph](concord://eph/v1/<epoch>)`, which is a self-destruct. A sweep
//     expires any message whose eph stamp has passed, so ONE archived line
//     carrying a past-dated eph token would delete itself out of every member's
//     store seconds after it first rendered. An import must not be able to
//     write that sentence, whether by malice or by an unlucky substring in a
//     conversation about this very format.
//
//   - @everyone and @here. Those two are matched with no table behind them —
//     the renderer treats them as a broadcast for every viewer unconditionally —
//     so an archived shout from 2014 would ping a guild that did not exist yet.
//     Ordinary @Name mentions are a different problem with a different fix: they
//     resolve only against the tables the caller passes, so the archive view
//     passes none, and this file leaves them alone rather than mangling every
//     name in a decade of conversation.
//
//   - NUL and SOH. The renderer stashes its own generated spans behind \x00 and
//     \x01 placeholders and splices them back at the end. A body containing
//     those bytes collides with the placeholders and splices in the wrong span —
//     not an escape, but a mention pill pointing at the wrong person, which is
//     its own kind of wrong.
//
// Everything else here is translation rather than defence: the export's own
// mention and emoji markup means nothing in this application and would render
// as noise, so it becomes plain text on the way in.

// maxImportedContentRunes bounds one archived message body.
//
// The live path has no cap at all, which is defensible for something a person
// typed and indefensible for something a loop produced: the chunk builder
// refuses a single message that seals to over a megabyte, so an unbounded body
// is a way to make an import fail at the very last step, after everything has
// been read and sealed. 16,384 runes is longer than any message anybody wrote
// on purpose and two orders of magnitude inside the chunk ceiling.
const maxImportedContentRunes = 16384

// truncationMark is appended to a body that was cut, so a reader is told rather
// than left with a sentence that stops.
const truncationMark = " […]"

var (
	// The export's own markup forms. All ASCII, all anchored on angle brackets,
	// none of which means anything to this application's renderer — so they
	// would otherwise show up verbatim in the middle of archived prose.
	exportEmojiRe   = regexp.MustCompile(`<a?:([A-Za-z0-9_~-]{1,64}):([0-9]{1,32})>`)
	exportUserRe    = regexp.MustCompile(`<@!?([0-9]{1,32})>`)
	exportRoleRe    = regexp.MustCompile(`<@&([0-9]{1,32})>`)
	exportChannelRe = regexp.MustCompile(`<#([0-9]{1,32})>`)
	// A unix-seconds stamp with an optional style letter. Rendered as an actual
	// date, because "sometime" is what the alternative says.
	exportStampRe = regexp.MustCompile(`<t:(-?[0-9]{1,15})(?::[tTdDfFR])?>`)

	// The scheme this application carries structured content on.
	concordSchemeRe = regexp.MustCompile(`(?i)concord://`)

	// The unconditional broadcast mentions, matched as the renderer matches
	// them (an ASCII word boundary after the word) so that neutralising one
	// here neutralises it there — and without regard to case, because the
	// inbox's mention scan ignores case even though the renderer does not.
	broadcastMentionRe = regexp.MustCompile(`(?i)@(everyone|here)(?:\b|$)`)

	emojiNameStripRe = regexp.MustCompile(`[^a-z0-9_]+`)
)

// SanitizeContent turns one exported message body into something safe and
// readable to store in a chronicle.
//
// Deliberately NOT an escaper: the renderer escapes, and doing it twice would
// leave a decade of history reading "&amp;lt;3". This removes what would be
// executed, translates what would be noise, and leaves the prose alone.
func SanitizeContent(s string) string {
	if s == "" {
		return ""
	}

	// Characters first. The defusing passes below match ASCII spellings, and
	// a character this pass drops, sitting inside one of those spellings,
	// hides it from them and then disappears: "conc\u200bord://" and
	// "@every\u200bone" used to come out live. So nothing is matched until
	// everything that is going to be dropped has been.
	s = stripInvisible(s)

	// Translation, while the ASCII markup is intact, then defusing.
	s = defuse(translateAll(s))

	// The cap, and defusing again after every cut: cutting "@everyonex" to
	// "@everyone" makes a broadcast that was not one, and the defusing can
	// lengthen the tail by a rune, which the next cut takes back. Two rounds
	// at most for an ASCII tail; the bound is a backstop.
	cut := false
	for i := 0; i < 8 && utf8.RuneCountInString(s) > maxImportedContentRunes; i++ {
		runes := []rune(s)
		s = defuse(strings.TrimRight(string(runes[:maxImportedContentRunes]), " \n"))
		cut = true
	}
	if cut {
		s += truncationMark
	}
	return strings.TrimSpace(s)
}

// translateAll runs translateMarkup until nothing changes, because one
// replacement can complete another ("<<@1>23>" becomes "<@123>"). Every rule
// but the stamp shortens the string and the stamp's date output cannot
// complete a markup form, so this stops at once in practice; the bound is a
// backstop against nesting built to make it quadratic, and what it leaves is
// inert text either way.
func translateAll(s string) string {
	for i := 0; i < 8; i++ {
		t := translateMarkup(s)
		if t == s {
			break
		}
		s = t
	}
	return s
}

// translateMarkup turns the export's own markup into plain text.
func translateMarkup(s string) string {
	s = exportEmojiRe.ReplaceAllStringFunc(s, func(m string) string {
		g := exportEmojiRe.FindStringSubmatch(m)
		// The shortcode form this application uses. If the emoji was imported
		// too, it renders as the picture; if it was not, it renders as
		// ":partyparrot:", which is what the original said anyway.
		if n := SanitizeEmojiName(g[1]); n != "" {
			return ":" + n + ":"
		}
		return ":" + g[1] + ":"
	})
	// Mentions keep the source's id rather than inventing a name. The id means
	// nothing in the destination guild — which is the point, since it therefore
	// cannot resolve to a living member — but it is what the export recorded,
	// and somebody holding the original can still look it up.
	s = exportUserRe.ReplaceAllString(s, "@$1")
	s = exportRoleRe.ReplaceAllString(s, "@$1")
	s = exportChannelRe.ReplaceAllString(s, "#$1")
	return exportStampRe.ReplaceAllStringFunc(s, func(m string) string {
		g := exportStampRe.FindStringSubmatch(m)
		n, err := strconv.ParseInt(g[1], 10, 64)
		if err != nil {
			return m
		}
		t := time.Unix(n, 0).UTC()
		if y := t.Year(); y < 1500 || y > 3000 {
			return m
		}
		return t.Format("2006-01-02 15:04 UTC")
	})
}

// defuse renames the scheme and breaks the broadcast mentions.
//
// The scheme is renamed rather than the token deleted: the reader still sees
// what was written, no token regex matches any more, and the renderer's
// autolink pass only ever follows http and https, so the result is inert
// literal text. One space is enough to break a broadcast match and costs the
// sentence nothing; "@ everyone" still reads as what it was. Neither
// replacement can complete a new match next to it, so one pass of each is a
// fixed point.
//
// Every consumer matches these forms as exact ASCII, so a full-width
// "＠everyone" or "ｃｏｎｃｏｒｄ://" is
// inert as written. It is not left that way: a later consumer that
// normalises (a search index, a copy into the composer) would fold it into
// the live form. A body whose NFKC form carries a live form is stored in its
// NFKC form, cleaned and defused like any other. That is lossy for that one
// message — compatibility characters elsewhere in it fold too — and costs
// nothing for every message that does not spell a protected form in them.
func defuse(s string) string {
	s = defuseASCII(s)
	if n := norm.NFKC.String(s); n != s && (concordSchemeRe.MatchString(n) || broadcastMentionRe.MatchString(n)) {
		// Translated again: the fold can turn full-width brackets into markup
		// the first translation never saw.
		s = defuseASCII(translateAll(stripInvisible(n)))
	}
	return s
}

// Defuse is the defusing pass alone, for text assembled from already
// sanitized parts: the importer joins a sanitized body with placeholder lines
// that carry sanitized file names, and the join is defused as a whole so no
// part, and no seam between parts, is the one place a live form gets through.
func Defuse(s string) string { return defuse(stripInvisible(s)) }

func defuseASCII(s string) string {
	s = concordSchemeRe.ReplaceAllString(s, "concord-archived://")
	return broadcastMentionRe.ReplaceAllString(s, "@ $1")
}

// stripInvisible drops control and invisible codepoints. Newlines survive (a
// message is allowed to have paragraphs), tabs become spaces, and everything
// else in the C0/C1 range, the DEL, the format category (bidi overrides,
// zero-width joiners) and unpaired surrogates go — the same screen
// validEventText holds live records to, applied here as a filter rather than a
// refusal because a twelve-year-old message with a stray byte in it is still
// history.
func stripInvisible(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevCR := false
	for _, r := range s {
		cr := false
		switch {
		case r == '\r':
			// A lone CR is a newline; a CRLF pair is ONE newline, which the
			// prevCR flag below is what makes true.
			b.WriteRune('\n')
			cr = true
		case r == '\n':
			if !prevCR {
				b.WriteRune('\n')
			}
		case r == '\t':
			b.WriteRune(' ')
		case r == utf8.RuneError:
			// Invalid UTF-8 in the source. Dropped rather than kept as U+FFFD:
			// a row of replacement characters is not information.
		case unicode.IsControl(r): // C0, DEL and C1
		case unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cs, r):
		default:
			b.WriteRune(r)
		}
		prevCR = cr
	}
	return b.String()
}

// SanitizeEmojiName folds an exported emoji name into the guild-emoji charset —
// `^[a-z0-9_]{2,32}$`, the same regex AddCustomEmoji enforces — or returns ""
// when nothing usable survives.
//
// Folding rather than refusing because the names in a real export are mostly
// fine and occasionally have a hyphen or a capital in them, and losing an emoji
// over a hyphen would be a silly reason. A name that collapses to one character
// or to nothing is refused: two is the floor the live path sets, and inventing
// padding to reach it would produce an emoji nobody could type.
func SanitizeEmojiName(name string) string {
	n := emojiNameStripRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "_")
	n = strings.Trim(n, "_")
	// Collapse runs of underscores the strip just produced.
	for strings.Contains(n, "__") {
		n = strings.ReplaceAll(n, "__", "_")
	}
	if len(n) > 32 {
		n = strings.TrimRight(n[:32], "_")
	}
	if len(n) < 2 {
		return ""
	}
	return n
}

// maxImportedNameRunes and maxImportedNameBytes bound a channel, category or
// author name coming out of an export. The live create path has no cap of its
// own, so this is the only thing between a malformed export and a sidebar entry
// that is a paragraph.
//
// BOTH, because they bind in different places. The rune count is what a person
// perceives as a long name; the byte count is what the manifest validates, and a
// hundred emoji is a hundred runes and four hundred bytes. A name that passed
// one gate and failed the other would fail at signing time — after the whole
// import had been read.
const (
	maxImportedNameRunes = 100
	maxImportedNameBytes = 200
)

// SanitizeName cleans a single-line name — a channel, a category, an author.
// Control and invisible codepoints go, whitespace collapses to single spaces,
// and the result is bounded. Returns "" when nothing is left, which every
// caller turns into a generated fallback rather than a nameless row.
func SanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || r == utf8.RuneError ||
			unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cs, r) {
			return -1
		}
		return r
	}, s)
	// Defused like a body. A name is not rendered as markdown where it is a
	// name, but it is carried into bodies: an attachment's file name goes into
	// the "[attachment not exported: …]" line, and "@everyone.png" there is a
	// broadcast.
	s = strings.Join(strings.Fields(defuse(s)), " ")
	// Bounded, and defused again after every cut, for the reason
	// SanitizeContent gives. Converges in two rounds; a name that has not by
	// the bound is dropped rather than returned over its size or live.
	for i := 0; ; i++ {
		s = boundName(s)
		d := defuse(s)
		if d == s {
			break
		}
		if i == 8 {
			return ""
		}
		s = d
	}
	return strings.TrimSpace(s)
}

// boundName cuts a name to maxImportedNameRunes and maxImportedNameBytes,
// whole runes only: half a codepoint is a name that renders as a replacement
// character on every member's screen.
func boundName(s string) string {
	if utf8.RuneCountInString(s) > maxImportedNameRunes {
		s = string([]rune(s)[:maxImportedNameRunes])
	}
	for len(s) > maxImportedNameBytes {
		r := []rune(s)
		s = string(r[:len(r)-1])
	}
	return s
}

// ChannelTypeOf maps an export's channel-type string onto the types this
// application has. Exports name types in their own vocabulary and different
// versions of the same tool have used different spellings, so the match is on
// substrings of the lowercased value rather than on an exact table — an unknown
// type becomes a text channel, which is the answer that loses the least.
//
// A voice channel maps to a voice channel and imports as STRUCTURE ONLY: an
// export of one carries join and leave notices and nothing anybody said, so
// there is no history to put in it.
func ChannelTypeOf(t string) string {
	l := strings.ToLower(t)
	switch {
	case strings.Contains(l, "voice"), strings.Contains(l, "stage"):
		return "voice"
	case strings.Contains(l, "forum"):
		return "forum"
	case strings.Contains(l, "news"), strings.Contains(l, "announce"):
		return "announcement"
	}
	return "text"
}
