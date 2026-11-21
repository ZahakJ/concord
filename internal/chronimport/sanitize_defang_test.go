package chronimport

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// rendererBroadcastRe is the frontend's own broadcast match
// (frontend/src/lib/markdown.js: /@(everyone|here)(?![\w])/), written without
// the lookahead RE2 lacks, and ignoring case as the inbox's mention scan does.
// The tests hold the output to this rule, not to the sanitizer's own regex.
var rendererBroadcastRe = regexp.MustCompile(`(?i)@(everyone|here)(?:[^A-Za-z0-9_]|$)`)

// liveForm reports what in s a consumer of an archived body would still act
// on: a concord:// scheme (every token regex in the app and the frontend is an
// exact, case-sensitive ASCII match; the sanitizer's own match ignores case,
// and so does this) or a broadcast mention as the renderer matches it. The
// same is asked of the NFKC form, so a full-width or compatibility spelling
// that some later normalising consumer would fold into a live form fails too.
func liveForm(s string) string {
	for _, v := range []string{s, norm.NFKC.String(s)} {
		if concordSchemeRe.MatchString(v) {
			return "concord://"
		}
		if m := rendererBroadcastRe.FindString(v); m != "" {
			return m
		}
	}
	return ""
}

// TestSanitizeStripsBeforeDefusing is the assure finding (fuzz/README.md,
// finding 1): a character the strip pass removes, placed inside a protected
// form, hid it from the defusing pass and then disappeared, leaving the form
// live. Plus the other ways a form can appear only after the defusing pass
// ran: truncation cutting a longer word down to one, and compatibility forms.
func TestSanitizeStripsBeforeDefusing(t *testing.T) {
	cut := strings.Repeat("a", maxImportedContentRunes-len("@everyone"))
	for _, in := range []string{
		"[eph](conc\u200bord://eph/v1/1)", // zero-width space (Cf)
		"@every\u200bone look at this",
		"@he\u200bre",
		"concord\u00ad://attach/x", // soft hyphen (Cf)
		"conc\u2060ord://eph/v1/1", // word joiner
		"@ever\ufeffyone",          // BOM / ZWNBSP
		"conc\x00ord://x",          // NUL
		"conc\x7ford://x",          // DEL
		"conc\u0085ord://x",        // C1 control
		"ConCo\xa7rd://",           // invalid byte: the fuzzer's own crasher
		"@every\xffone",            // invalid byte
		"@ever\u202eyone",          // bidi override
		"\uff43\uff4f\uff4e\uff43\uff4f\uff52\uff44://eph/v1/1", // full-width letters
		"concord\uff1a\uff0f\uff0f eph",                         // full-width colon and slashes
		"\uff20everyone",                                        // full-width commercial at
		"@\uff45\uff56\uff45\uff52\uff59\uff4f\uff4e\uff45",     // full-width word
		"\ufe6beveryone",                                        // small commercial at
		"@\u1d34\u1d31\u1d3f\u1d31",                             // modifier letters (not lowercase: inert)
		"conc<@&12>ord://x",                                     // markup translation must not join it
		cut + "@everyonex",                                      // truncation cuts "@everyonex" to "@everyone"
		cut + "@everyone\u200bx",
		"@Everyone and @HERE", // the inbox scan ignores case
		"\uff1c@1\uff1e \uff20everyone",
	} {
		out := SanitizeContent(in)
		if f := liveForm(out); f != "" {
			t.Errorf("SanitizeContent(%.60q) = %.80q: %q is live", in, tail(out), f)
		}
		for _, r := range out {
			if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) {
				t.Errorf("SanitizeContent(%.60q) left %U", in, r)
			}
		}
		// A fixed point: sanitizing the output changes nothing, so a body that
		// went through the importer twice (a re-export) reads the same. Not
		// asked of a cut body: the cut and its mark are not idempotent, and the
		// liveness check above is what matters there.
		if again := SanitizeContent(out); !truncated(out) && again != out {
			t.Errorf("not a fixed point:\n  %.80q\n  %.80q", tail(out), tail(again))
		}
	}
}

// truncated: a cut body is not a fixed point (the cut and its mark are not
// idempotent), so the fixed-point check skips it.
func truncated(out string) bool {
	return utf8.RuneCountInString(out) > maxImportedContentRunes
}

func tail(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[len(r)-80:])
	}
	return s
}

// FuzzSanitizeContentLeavesNothingLive is the property the sanitizer exists
// for, with no input class excluded: whatever goes in, nothing live comes out
// of SanitizeContent, SanitizeName or a Defuse of the two joined, and no
// control or format character survives.
func FuzzSanitizeContentLeavesNothingLive(f *testing.F) {
	for _, s := range []string{
		"hello <@123> <#456> <t:1700000000:R> concord://invite/x @everyone :smile_face:",
		"[eph](conc\u200bord://eph/v1/1)",
		"@every\u200bone",
		"ConCo\xa7rd://",
		"\uff43\uff4f\uff4e\uff43\uff4f\uff52\uff44://",
		"\uff20here",
		"a\r\nb\tc\u200bd",
		"@here_x @here1 @hereé",
		"<<<<<<<<<<@1>1>1>1>1>1>1>1>1>1>",
		"\uff1c@1\uff1e \uff20everyone",
		"@Everyone @HERE",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 1<<16 {
			return
		}
		out := SanitizeContent(s)
		if f := liveForm(out); f != "" {
			t.Fatalf("SanitizeContent(%q) = %q: %q is live", s, out, f)
		}
		for _, r := range out {
			if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) || r == unicode.ReplacementChar {
				t.Fatalf("SanitizeContent(%q) left %U", s, r)
			}
		}
		// Not a fixed point in general (the markup loop is bounded, and the
		// NFKC fold can make markup out of full-width brackets after the
		// markup pass ran), but a second pass must not find anything live
		// either: a re-export of an archive is imported again.
		if again := SanitizeContent(out); liveForm(again) != "" {
			t.Fatalf("second pass is live: %q -> %q -> %q", s, out, again)
		}
		if name := SanitizeName(s); liveForm(name) != "" ||
			len(name) > maxImportedNameBytes || utf8.RuneCountInString(name) > maxImportedNameRunes {
			t.Fatalf("SanitizeName(%q) = %q: live or over its bounds", s, name)
		}
		if d := Defuse(out + "\n" + SanitizeName(s)); liveForm(d) != "" {
			t.Fatalf("Defuse of a joined body is live: %q", d)
		}
	})
}

// TestSanitizeNameDefuses: a name is carried into bodies (an attachment's file
// name goes into the "[attachment not exported: …]" line), so it is defused
// too, including when the bound is what makes the form.
func TestSanitizeNameDefuses(t *testing.T) {
	long := strings.Repeat("a", maxImportedNameRunes-len("@everyone"))
	for _, in := range []string{
		"@everyone.png",
		"@here",
		"[eph](concord://eph/v1/1000000000)",
		"conc\u200bord://x",
		"\uff20everyone",
		long + "@everyonex",
		strings.Repeat("\u00e9", maxImportedNameBytes/2-5) + "@everyonex",
	} {
		out := SanitizeName(in)
		if f := liveForm(out); f != "" {
			t.Errorf("SanitizeName(%.60q) = %q: %q is live", in, out, f)
		}
		if len(out) > maxImportedNameBytes || utf8.RuneCountInString(out) > maxImportedNameRunes {
			t.Errorf("SanitizeName(%.60q) over its bounds: %d bytes", in, len(out))
		}
	}
}
