package store

import "testing"

// The grammar has to be exactly the one lib/games.js's isGameToken uses: every
// peer receives the same message and decides for itself whether its badge
// counts it, so an unread total that differs between two people playing the
// same game is what a disagreement here looks like.
func TestIsQuietIsTheTokenAndNothingElse(t *testing.T) {
	tok := "[game](concord://game/v1/eyJrIjoibXYiLCJpIjoiYWIxMjM0In0)"
	cases := []struct {
		in   string
		want bool
	}{
		{tok, true},
		{"  " + tok + "\n", true},
		{"nice one " + tok, false}, // words around it are somebody talking
		{tok + " got you", false},
		{"", false},
		{"just a message", false},
		{"[poll](concord://poll/v1/abc)", false},
		{"[game](concord://game/v1/)", false},    // no payload
		{"[game](concord://game/v2/abc)", false}, // a version we do not know
		{tok + " " + tok, false},                 // two tokens is not the token
	}
	for _, c := range cases {
		if got := IsQuiet(c.in); got != c.want {
			t.Errorf("IsQuiet(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
