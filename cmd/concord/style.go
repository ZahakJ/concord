package main

import (
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// style is how the chat commands print: Concord's colours on a terminal, and
// plain text everywhere else (a pipe, a file, a script, NO_COLOR, TERM=dumb),
// so what a script reads never carries escape codes.
type style struct {
	on    bool
	truec bool
}

// The app's palette (frontend/src/app.css, dark theme): its teal accent, the
// muted text, and the danger and warning tones.
var (
	cAccent = rgb{0x14, 0xa3, 0x94}
	cMuted  = rgb{0xa4, 0xab, 0xb0}
	cDanger = rgb{0xe5, 0x63, 0x6a}
	cWarn   = rgb{0xd9, 0xa1, 0x3c}
	// Names take one of these, by fingerprint, so a person keeps one colour
	// in every chat and two people side by side are told apart. All of them
	// hold WCAG AA (4.5:1) on a black terminal and on the app's dark ground.
	namePalette = []rgb{
		{0x3c, 0xc8, 0xb4}, // teal, the accent lifted
		{0x7a, 0xa7, 0xff}, // blue
		{0xe0, 0x8a, 0xd8}, // orchid
		{0xf0, 0xa5, 0x5c}, // amber
		{0x8f, 0xd1, 0x6a}, // green
		{0xff, 0x8a, 0x8a}, // coral
		{0xb5, 0x9c, 0xff}, // violet
		{0x5f, 0xc8, 0xe8}, // sky
	}
)

type rgb struct{ r, g, b uint8 }

// newStyle decides colour for w: mode is "auto", "always" or "never".
func newStyle(w io.Writer, mode string) style {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	truec := ct == "truecolor" || ct == "24bit"
	switch mode {
	case "always":
		return style{on: true, truec: truec}
	case "never":
		return style{}
	}
	if _, set := os.LookupEnv("NO_COLOR"); set || os.Getenv("TERM") == "dumb" {
		return style{}
	}
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return style{}
	}
	return style{on: true, truec: truec}
}

func (s style) fg(c rgb) string {
	if s.truec {
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b)
	}
	// The nearest colour of the xterm 6x6x6 cube.
	q := func(v uint8) int { return (int(v)*5 + 127) / 255 }
	return fmt.Sprintf("\x1b[38;5;%dm", 16+36*q(c.r)+6*q(c.g)+q(c.b))
}

func (s style) wrap(code, text string) string {
	if !s.on || text == "" {
		return text
	}
	return code + text + "\x1b[0m"
}

func (s style) accent(t string) string { return s.wrap(s.fg(cAccent), t) }
func (s style) muted(t string) string  { return s.wrap(s.fg(cMuted), t) }
func (s style) danger(t string) string { return s.wrap(s.fg(cDanger), t) }
func (s style) warn(t string) string   { return s.wrap(s.fg(cWarn), t) }
func (s style) bold(t string) string   { return s.wrap("\x1b[1m", t) }
func (s style) dim(t string) string    { return s.wrap("\x1b[2m", t) }

// badge is an unread count, in the accent, reversed, like the app's pill.
func (s style) badge(n int) string {
	t := fmt.Sprintf(" %d ", n)
	if n > 99 {
		t = " 99+ "
	}
	if !s.on {
		return fmt.Sprintf("(%d)", n)
	}
	return s.wrap("\x1b[1;7m"+s.fg(cAccent), t)
}

// name is a person's name in their colour.
func (s style) name(fpr, name string) string {
	if !s.on {
		return name
	}
	h := fnv.New32a()
	h.Write([]byte(fpr))
	return s.wrap("\x1b[1m"+s.fg(namePalette[h.Sum32()%uint32(len(namePalette))]), name)
}

// clean strips terminal control characters from text a stranger wrote: a
// message must not be able to move the cursor, retitle the window or recolour
// the rest of the screen. Newlines and tabs are kept; every other C0/C1
// control and DEL becomes nothing.
func clean(t string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return -1 // bidi overrides and isolates, which reorder a line
		}
		return r
	}, t)
}
