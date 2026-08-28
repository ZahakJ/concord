package app

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
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

// ---- the read model --------------------------------------------------------
//
// The bug this covers shipped: the inbox's own mark starts at zero, so on the
// first open every mention anyone had ever sent was born unread and the bell
// claimed a backlog the user had read months earlier, in the channels.

// inboxService is a Service with a real store and a real identity behind it —
// Inbox() reads the store and the (empty) guild/roster maps, and nothing else.
func inboxService(t *testing.T) (*Service, *identity.Identity) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "concord.db"), bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	me := mustID(t)
	s := &Service{store: st, id: me}
	if err := st.SetSetting("display_name", "ada"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	return s, me
}

// saveMention writes a message from someone else that names "ada".
func saveMention(t *testing.T, s *Service, channelID, id string, at time.Time) {
	t.Helper()
	other := mustID(t)
	if _, err := s.store.SaveMessage(domain.Message{
		ID: id, ChannelID: channelID, Sender: other.PublicKey(), Name: "rowan",
		Content: fmt.Sprintf("@ada could you look at %s", id), Sent: at,
	}); err != nil {
		t.Fatalf("SaveMessage %s: %v", id, err)
	}
}

func inboxOf(t *testing.T, s *Service) InboxPage {
	t.Helper()
	page, err := s.Inbox(nil, 0, 50, false)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	return page
}

func TestInboxBacklogIsBornReadWhenTheChannelWasRead(t *testing.T) {
	s, _ := inboxService(t)
	now := time.Now()
	old := now.Add(-30 * 24 * time.Hour)

	// A month of mentions in one channel, all read in the channel a week ago.
	for i := 0; i < 3; i++ {
		saveMention(t, s, "ch-history", fmt.Sprintf("old-%d", i), old.Add(time.Duration(i)*time.Hour))
	}
	if _, err := s.store.AdvanceReadState("ch-history", now.Add(-7*24*time.Hour).UnixMilli()); err != nil {
		t.Fatalf("AdvanceReadState: %v", err)
	}

	page := inboxOf(t, s)
	if len(page.Entries) != 3 {
		t.Fatalf("the entries must still be listed, got %d", len(page.Entries))
	}
	if page.Unread != 0 {
		t.Fatalf("a backlog read in the channel is born READ, got %d unread", page.Unread)
	}
	for _, e := range page.Entries {
		if e.Unread {
			t.Fatalf("%s came back unread despite the channel read mark", e.MessageID)
		}
	}

	// A channel nobody has opened has no mark, so its mentions ARE unread.
	saveMention(t, s, "ch-never-opened", "fresh-1", old)
	if page = inboxOf(t, s); page.Unread != 1 {
		t.Fatalf("an unopened channel's mention is unread, got %d", page.Unread)
	}
}

func TestInboxUnreadFollowsBothMarks(t *testing.T) {
	s, _ := inboxService(t)
	now := time.Now()

	saveMention(t, s, "ch", "m-old", now.Add(-2*time.Hour))
	if _, err := s.store.AdvanceReadState("ch", now.Add(-time.Hour).UnixMilli()); err != nil {
		t.Fatalf("AdvanceReadState: %v", err)
	}
	if page := inboxOf(t, s); page.Unread != 0 {
		t.Fatalf("the read backlog should be quiet, got %d", page.Unread)
	}

	// A mention arrives now, after the channel mark: unread.
	saveMention(t, s, "ch", "m-new", now)
	page := inboxOf(t, s)
	if page.Unread != 1 {
		t.Fatalf("a fresh mention is unread, got %d", page.Unread)
	}
	if !page.Entries[0].Unread || page.Entries[0].MessageID != "m-new" {
		t.Fatalf("the newest entry should be the unread one, got %+v", page.Entries[0])
	}

	// Reading the channel past it retires the entry — nothing is written to the
	// inbox to make that happen, it is derived.
	if _, err := s.store.AdvanceReadState("ch", now.Add(time.Second).UnixMilli()); err != nil {
		t.Fatalf("AdvanceReadState: %v", err)
	}
	if page := inboxOf(t, s); page.Unread != 0 {
		t.Fatalf("reading the channel must clear the inbox entry, got %d unread", page.Unread)
	}

	// And the other half: an entry in a channel you have NOT caught up on is
	// dismissible with the inbox's own mark.
	saveMention(t, s, "ch2", "m-other", now)
	if page := inboxOf(t, s); page.Unread != 1 {
		t.Fatalf("a mention in an unread channel is unread, got %d", page.Unread)
	}
	if err := s.MarkInboxRead(now.Add(time.Second).UnixMilli()); err != nil {
		t.Fatalf("MarkInboxRead: %v", err)
	}
	page = inboxOf(t, s)
	if page.Unread != 0 {
		t.Fatalf("mark-all-read must clear everything, got %d unread", page.Unread)
	}
	if page.ReadAt == 0 {
		t.Fatal("the inbox mark should have been recorded")
	}

	// A mention arriving after mark-all-read is unread again, in a channel whose
	// own mark is still behind.
	saveMention(t, s, "ch2", "m-later", now.Add(time.Minute))
	if page := inboxOf(t, s); page.Unread != 1 {
		t.Fatalf("a mention after mark-all-read is unread, got %d", page.Unread)
	}
}

// unreadOnly must not hand back entries a channel mark has already retired: the
// SQL floor can only see the inbox mark, so the filter has to run in Go.
func TestInboxUnreadOnlyRespectsTheChannelMark(t *testing.T) {
	s, _ := inboxService(t)
	now := time.Now()

	saveMention(t, s, "ch-read", "seen", now.Add(-time.Hour))
	saveMention(t, s, "ch-unread", "unseen", now.Add(-time.Hour))
	if _, err := s.store.AdvanceReadState("ch-read", now.UnixMilli()); err != nil {
		t.Fatalf("AdvanceReadState: %v", err)
	}

	page, err := s.Inbox(nil, 0, 50, true)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].MessageID != "unseen" {
		t.Fatalf("unread-only should return exactly the unread entry, got %+v", page.Entries)
	}
	if page.Unread != 1 {
		t.Fatalf("unread count = %d, want 1", page.Unread)
	}
}

// The decision itself, stated once so the two callers cannot disagree about it.
func TestInboxUnreadRule(t *testing.T) {
	cases := []struct {
		at, inbox, channel int64
		want               bool
	}{
		{at: 100, inbox: 0, channel: 0, want: true},      // nothing read: unread
		{at: 100, inbox: 0, channel: 200, want: false},   // read in the channel
		{at: 100, inbox: 200, channel: 0, want: false},   // dismissed in the inbox
		{at: 100, inbox: 200, channel: 200, want: false}, // both
		{at: 300, inbox: 200, channel: 200, want: true},  // newer than both
		{at: 100, inbox: 100, channel: 0, want: false},   // the mark is inclusive
	}
	for _, c := range cases {
		if got := inboxUnread(c.at, c.inbox, c.channel); got != c.want {
			t.Errorf("inboxUnread(%d, %d, %d) = %v, want %v", c.at, c.inbox, c.channel, got, c.want)
		}
	}
}

// hitWith builds the minimum an inboxReason call needs.
func hitWith(body string, reply bool) store.InboxHit {
	h := store.InboxHit{RepliesToYou: reply}
	h.Content = body
	return h
}
