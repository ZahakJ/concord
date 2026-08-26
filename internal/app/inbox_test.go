package app

import (
	"testing"

	"github.com/ZahakJ/concord/internal/store"
)

// The word-boundary rule is the whole difference between an alert word people
// keep switched on and one they turn off after a day. "cat" must not fire on
// "concatenate"; "ci" must not fire on every word containing those two letters.

func TestAlertWordNeedsWholeWordEdges(t *testing.T) {
	cases := []struct {
		body, word string
		want       bool
	}{
		{"the release is broken", "release", true},
		{"Release is broken", "release", true}, // case folds
		{"RELEASE", "release", true},           // both directions
		{"pre-release notes", "release", true}, // a hyphen is an edge
		{"(release)", "release", true},         // so is punctuation
		{"release, then", "release", true},     // and a comma
		{"prerelease notes", "release", false}, // no leading edge
		{"releases", "release", false},         // no trailing edge
		{"unreleased", "release", false},       // neither
		{"concatenate", "cat", false},          // the classic false positive
		{"the cat sat", "cat", true},           //
		{"cat", "cat", true},                   // the whole body
		{"a cat.", "cat", true},                //
		{"scatter", "cat", false},              //
		{"", "cat", false},                     // nothing to match
		{"anything", "", false},                // nothing to match with
		{"ship it", "ship it", true},           // a phrase, spaces and all
		{"we should ship it now", "ship it", true},
		{"shipping it", "ship it", false},
	}
	for _, c := range cases {
		if got := containsWholeWord(c.body, c.word); got != c.want {
			t.Errorf("containsWholeWord(%q, %q) = %v, want %v", c.body, c.word, got, c.want)
		}
	}
}

// Unicode: a word boundary is not a byte boundary. An Arabic or Cyrillic alert
// word has edges too, and the letters either side of it are letters.
func TestAlertWordBoundariesAreUnicodeAware(t *testing.T) {
	cases := []struct {
		body, word string
		want       bool
	}{
		{"نشر الإصدار اليوم", "الإصدار", true},
		{"الإصدارات", "الإصدار", false}, // a longer Arabic word, not a match
		{"выпуск готов", "выпуск", true},
		{"выпускной", "выпуск", false},
		{"日本語 テスト", "テスト", true},
		{"emoji 🎉 party", "party", true},
		{"🎉party", "party", false}, // an emoji is not a letter, so this IS an edge
	}
	for _, c := range cases {
		got := containsWholeWord(c.body, c.word)
		if c.body == "🎉party" {
			// Stated rather than assumed: an emoji is not a letter or a digit, so
			// it counts as a boundary and this matches. The case is here to pin
			// the behaviour, not to forbid it.
			if !got {
				t.Errorf("an emoji should be a word edge: %q / %q", c.body, c.word)
			}
			continue
		}
		if got != c.want {
			t.Errorf("containsWholeWord(%q, %q) = %v, want %v", c.body, c.word, got, c.want)
		}
	}
}

// A mention has a trailing boundary and no leading one, matching the renderer's
// own containsMention. "hi@ada" IS a mention there, and the two must agree or a
// row would highlight without an inbox entry to go with it.
func TestMentionMatchesTheRenderersRule(t *testing.T) {
	cases := []struct {
		body, name string
		want       bool
	}{
		{"hey @ada can you look", "ada", true},
		{"hey @Ada", "ada", true},
		{"hi@ada", "ada", true},    // no leading boundary, deliberately
		{"@adamant", "ada", false}, // trailing boundary is required
		{"@ada!", "ada", true},     // punctuation ends the name
		{"ada", "ada", false},      // no @, no mention
		{"@ada", "Ada Lovelace", false},
		{"@Ada Lovelace, hello", "Ada Lovelace", true}, // names with spaces
	}
	for _, c := range cases {
		if got := mentionsWord(c.body, c.name); got != c.want {
			t.Errorf("mentionsWord(%q, %q) = %v, want %v", c.body, c.name, got, c.want)
		}
	}
}

// The alert list is bounded before it reaches the scan: every word costs a pass
// over every candidate body, and a one-character word matches everything.
func TestAlertWordsAreCleanedAndBounded(t *testing.T) {
	got := cleanAlertWords([]string{"  Release ", "release", "RELEASE", "x", "", "  ", "ship it"})
	if len(got) != 2 || got[0] != "release" || got[1] != "ship it" {
		t.Fatalf("expected [release ship it], got %q", got)
	}

	many := make([]string, 200)
	for i := range many {
		many[i] = string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('0'+i%10))
	}
	if n := len(cleanAlertWords(many)); n > 50 {
		t.Fatalf("the list should be capped, got %d", n)
	}

	long := make([]byte, 100)
	for i := range long {
		long[i] = 'a'
	}
	if n := len(cleanAlertWords([]string{string(long)})); n != 0 {
		t.Fatalf("an over-long word should be dropped, got %d", n)
	}
}

// A finding cannot claim two things at once, and the more specific claim wins.
func TestInboxReasonPrefersTheMostSpecificClaim(t *testing.T) {
	s := &Service{}
	words := []string{"release"}

	mention := hitWith("hey @ada, the release is out", true)
	if r, _ := s.inboxReason(mention, "ada", nil, words); r != InboxMention {
		t.Fatalf("a reply that also names you is a mention, got %q", r)
	}

	reply := hitWith("the release is out", true)
	if r, _ := s.inboxReason(reply, "ada", nil, words); r != InboxReply {
		t.Fatalf("a reply that only carries an alert word is a reply, got %q", r)
	}

	keyword := hitWith("the release is out", false)
	r, term := s.inboxReason(keyword, "ada", nil, words)
	if r != InboxKeyword || term != "release" {
		t.Fatalf("expected a keyword hit naming the word, got %q/%q", r, term)
	}

	// A role you hold reaches you, and says which role did it.
	role := hitWith("@moderators please look", false)
	r, term = s.inboxReason(role, "ada", []string{"moderators"}, nil)
	if r != InboxMention || term != "moderators" {
		t.Fatalf("expected a role mention naming the role, got %q/%q", r, term)
	}

	// A role you do NOT hold in this guild is not your mention. myRoles is
	// per-guild for exactly this reason.
	other := hitWith("@moderators please look", false)
	if r, _ := s.inboxReason(other, "ada", nil, nil); r != "" {
		t.Fatalf("a role you do not hold here must not reach you, got %q", r)
	}

	// A broadcast reaches everyone without a name to match.
	if r, _ := s.inboxReason(hitWith("@everyone standup", false), "", nil, nil); r != InboxMention {
		t.Fatalf("@everyone is a mention, got %q", r)
	}
	if r, _ := s.inboxReason(hitWith("@here standup", false), "", nil, nil); r != InboxMention {
		t.Fatalf("@here is a mention, got %q", r)
	}

	// And a message that concerns you in no way at all is not an entry.
	if r, _ := s.inboxReason(hitWith("morning everyone", false), "ada", nil, words); r != "" {
		t.Fatalf("an unrelated message must not become an entry, got %q", r)
	}
}

// The snippet is bounded and never cuts a rune in half.
func TestSnippetIsBoundedAndRuneSafe(t *testing.T) {
	if got := snippet("one\ntwo   three"); got != "one two three" {
		t.Fatalf("newlines and runs of space collapse: %q", got)
	}
	long := ""
	for i := 0; i < 200; i++ {
		long += "🎉"
	}
	got := snippet(long)
	for _, r := range got {
		if r == '�' {
			t.Fatal("the snippet cut a rune in half")
		}
	}
	if len([]rune(got)) > 161 {
		t.Fatalf("the snippet is not bounded: %d runes", len([]rune(got)))
	}
}

// hitWith builds the minimum an inboxReason call needs.
func hitWith(body string, reply bool) store.InboxHit {
	h := store.InboxHit{RepliesToYou: reply}
	h.Content = body
	return h
}
