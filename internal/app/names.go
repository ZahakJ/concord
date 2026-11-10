package app

import (
	"errors"
	"strings"
	"unicode"
)

// A display name is what every member reads next to your messages, and it used
// to be accepted as long as TrimSpace left anything. Three zero-width spaces
// passed: the account went nameless and initial-less everywhere, and the
// "type your name to confirm" erase could no longer be confirmed because there
// was nothing anybody could type. A right-to-left override passed too, which
// turns a name into a way of making the text BESIDE it read backwards.

// invisibleRune reports a rune that occupies no visible space on its own:
// whitespace, format controls (zero-width spaces and joiners, bidi controls,
// soft hyphens), lone combining marks, and the blank "letters" that are
// popular precisely because they look like nothing.
func invisibleRune(r rune) bool {
	switch r {
	case 'ᅟ', 'ᅠ', 'ㅤ', 'ﾠ', '⠀':
		return true
	}
	return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r) ||
		unicode.Is(unicode.Me, r) || unicode.IsControl(r)
}

// HasVisible reports whether s has at least one character a person can see.
func HasVisible(s string) bool {
	for _, r := range s {
		if !invisibleRune(r) {
			return true
		}
	}
	return false
}

// bidiControl: the explicit embedding, override and isolate controls. A name
// is shown in its own bidi-isolated box everywhere, so these do nothing for
// the person using them and everything for one trying to spoof.
func bidiControl(r rune) bool {
	return (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩')
}

var errInvisibleName = errors.New("a name needs at least one visible character")

// cleanDisplayName trims a display name and drops bidi controls, refusing one
// with nothing visible left. Zero-width joiners stay: Persian names and emoji
// sequences need them, and between visible letters they hide nothing.
func cleanDisplayName(name string) (string, error) {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if bidiControl(r) {
			return -1
		}
		return r
	}, name))
	if !HasVisible(name) {
		return "", errInvisibleName
	}
	return name, nil
}

// Typeable reports whether a person could type s from a keyboard: it has
// something visible and no invisible format characters to reproduce. The
// erase confirmation asks for the display name back, and falls back to a
// fixed word when this says no.
func Typeable(s string) bool {
	if !HasVisible(s) {
		return false
	}
	for _, r := range s {
		if r != ' ' && invisibleRune(r) && !unicode.Is(unicode.Mn, r) {
			return false
		}
	}
	return true
}
