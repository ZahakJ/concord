package app

import (
	"encoding/base64"
	"strings"
	"testing"
)

func b64u(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// The cases lib/snippet.js is also held to. If one half learns a token the
// other does not, an inbox row and a reply strip quoting the same message say
// different things, which is the bug this file exists to stop coming back.
func TestSnippetFlattensTheSameWayTheClientDoes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"an event announcement reads as its title, never its payload",
			"[event](concord://event/v1/eyJpZCI6ImUxIiwidGl0bGUiOiJPcGVuIGJ1aWxkIG5pZ2h0In0)",
			"📅 Open build night",
		},
		{
			"a truncated event token still falls back to a label",
			"[event](concord://event/v1/AAAA)",
			"📅 Event",
		},
		{
			"an image token never reaches the reader",
			"![image](concord://attach/v2/20ed697d4012474c24a40a5de4f800133abc/" +
				strings.Repeat("A", 75) + "/png/800/600/0//)",
			"🖼 image",
		},
		{
			"an image with a caption shows the caption",
			"look at this ![image](concord://attach/v1/" + strings.Repeat("a", 64) + "/" +
				strings.Repeat("A", 75) + "/png/800x600)",
			"🖼 look at this",
		},
		{
			"a file token becomes a paperclip",
			"[file](concord://file/v1/" + strings.Repeat("a", 64) + "/" +
				strings.Repeat("A", 75) + "/1234/dGV4dA/bmFtZQ)",
			"📎 file",
		},
		{"inline code loses its backticks", "`rules.apply` returning null…", "rules.apply returning null…"},
		{
			"a guild invite is named, never its JSON",
			`{"op":"offered","what":"guild","guild":"Dar al-Hikma"}`,
			"invite to Dar al-Hikma",
		},
		{
			"a join notice in the 1:1 is named too",
			`{"op":"joined","what":"meeting","guild":"Office hours"}`,
			"joined Office hours",
		},
		{"bold and italics lose their markers", "the **whole** trick is *the fold*", "the whole trick is the fold"},
		{"a link keeps its label", "see [the spec](https://example.org/spec) first", "see the spec first"},
		{"a quote loses its angle", "> quoted line", "quoted line"},
		{
			"a fenced block is named, not flattened",
			"here it is in eleven lines:\n```js\nexport function fold(a, b) {\n  return a + b;\n}\n```",
			"here it is in eleven lines: 📄 code block",
		},
		{
			"an unterminated fence is named too",
			"cut mid-payload:\n```js\nexport function fo",
			"cut mid-payload: 📄 code block",
		},
		{"a poll is its question", "[poll](concord://poll/v1/" + b64u(`{"q":"Which surface next?","opts":["a","b"]}`) + ")", "📊 Which surface next?"},
		{"a corrupt poll is still not base64", "[poll](concord://poll/v1/zzzz)", "📊 Poll"},
		{"a game is named", "[game](concord://game/v1/AQIDBAUGBwgJ)", "🎲 game"},
		{"a doodle is named", "[doodle](concord://doodle/v1/AQIDBAUGBwgJ)", "🖌 doodle"},
		{"a sound is named", "[sound](concord://sfx/v1/AQIDBAUGBwgJ)", "🔊 sound"},
		{
			"an announcement is its body",
			"[announcement](concord://announce/v1/" + b64u(`{"body":"**Doors** at seven","from":"general"}`) + ")",
			"📣 Doors at seven",
		},
		{"a send effect is not the message", "[fx](concord://fx/v1/confetti) we shipped", "we shipped"},
		{"a disappearing timer is not the message", "[eph](concord://eph/v1/1234567890) burn after reading", "burn after reading"},
		{"snake_case survives", "call read_all_rows now", "call read_all_rows now"},
		{"newlines collapse", "one\n\ntwo\nthree", "one two three"},
	}
	for _, c := range cases {
		if got := snippet(c.in); got != c.want {
			t.Errorf("%s:\n  in   %q\n  got  %q\n  want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestSnippetCutsOnARuneBoundary(t *testing.T) {
	body := strings.Repeat("سلام ", 80) // 400 runes, far past the 160 cap
	got := snippet(body)
	r := []rune(got)
	if len(r) > 161 || len(r) < 150 { // 160 runes, less any space trimmed, plus the ellipsis
		t.Fatalf("expected the cut near 160 runes, got %d", len(r))
	}
	if r[len(r)-1] != '…' {
		t.Fatalf("expected a trailing ellipsis, got %q", string(r[len(r)-1]))
	}
	if !strings.HasPrefix(got, "سلام") {
		t.Fatalf("cut mangled the first rune: %q", got[:12])
	}
}
