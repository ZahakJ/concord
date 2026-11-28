package main

import (
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

// The development console prints what peers wrote on one line each, with
// nothing in it that can drive the terminal or hide or reorder text.
func TestTheConsolePrintsPeerTextClean(t *testing.T) {
	m := domain.Message{ChannelID: "ch-1234567890", Content: "hi\x1b]52;c;cGF3bmVk\x07\x1b[2J\xe2\x80\xaegnp.exe\nfake prompt> "}
	got := replLine(m)
	if strings.ContainsAny(got, "\x1b\x07\xe2\x80\xae\n") {
		t.Fatalf("console line kept control text: %q", got)
	}
	if !strings.Contains(got, "hi") || !strings.Contains(got, "fake prompt>") {
		t.Fatalf("console line lost the words: %q", got)
	}
}

// The characters that render as nothing (or as a blank) are removed with
// the controls: soft hyphen, invisible operators, deprecated format
// characters, the Mongolian vowel separator, Hangul fillers and tag
// characters.
func TestHostileCoversTheInvisibleFormatCharacters(t *testing.T) {
	for _, r := range []rune{0xad, 0x2060, 0x2061, 0x2064, 0x206a, 0x206f, 0x180e, 0x115f, 0x1160, 0x3164, 0xffa0, 0x2800, 0xe0000, 0xe0041, 0xe007f} {
		if !hostile(r) {
			t.Errorf("U+%04X is not removed", r)
		}
	}
	for _, r := range []rune{'a', 'ع', 0x2065, 0x3165, 0xe0080, '👍'} {
		if hostile(r) {
			t.Errorf("U+%04X is removed but is ordinary text", r)
		}
	}
}
