package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zahak/concord/internal/domain"
)

// ---- pure validation ----

func TestValidHexColorIsStrict(t *testing.T) {
	// Only #rrggbb. The refusals matter more than the acceptances: every one of
	// these would otherwise be interpolated into a CSS colour by the client.
	for _, ok := range []string{"#000000", "#ffffff", "#1a2B3c", "#abcdef"} {
		if !validHexColor(ok) {
			t.Errorf("validHexColor(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{
		"", "#fff", "fff000", "#ffff", "#gggggg", "#00000", "#0000000",
		"red", "rgb(0,0,0)", "var(--accent)",
		"#000000;background:url(x)", "#000000 ", "#00000\n",
		"transparent", "currentColor", "#000000/**/",
	} {
		if validHexColor(bad) {
			t.Errorf("validHexColor(%q) = true, want false", bad)
		}
	}
}

func TestValidTagIDMatchesGeneratedIDs(t *testing.T) {
	// A locally minted id must pass its own validator, or every tag we create is
	// dropped by the receiver.
	for i := 0; i < 20; i++ {
		if id := domain.NewID(); !validTagID(id) {
			t.Fatalf("validTagID(%q) = false for a domain.NewID()", id)
		}
	}
	for _, bad := range []string{
		"", "UPPER", "has space", "semi;colon", "quote\"", "url(x)",
		strings.Repeat("a", 33), "a/b", "a.b", "☃",
	} {
		if validTagID(bad) {
			t.Errorf("validTagID(%q) = true, want false", bad)
		}
	}
}

func TestValidTagTextAllowsWordsButNotLayoutBreakers(t *testing.T) {
	// An interior space is a normal tag name; the rejections are characters that
	// do something other than draw themselves.
	for _, ok := range []string{"Bug", "In progress", "Won't fix", "需要帮助", "🐛"} {
		if !validTagText(ok, maxTagNameRunes, false) {
			t.Errorf("validTagText(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{
		"", "two\nlines", "tab\there", "nul\x00byte",
		" sep", " sep", "‮override", "⁦isolate", "‎mark",
		strings.Repeat("x", maxTagNameRunes+1),
	} {
		if validTagText(bad, maxTagNameRunes, false) {
			t.Errorf("validTagText(%q) = true, want false", bad)
		}
	}
	if !validTagText("", maxTagEmojiRunes, true) {
		t.Error("an empty emoji must be allowed — it is optional")
	}
	// Rune budget, not byte budget: 24 four-byte runes must pass.
	if !validTagText(strings.Repeat("𝔘", maxTagNameRunes), maxTagNameRunes, false) {
		t.Error("a 24-rune multibyte name must pass a RUNE cap")
	}
}

func TestSanitizeForumTagsDropsCapsAndDedupes(t *testing.T) {
	good := domain.ForumTag{ID: "aa", Name: "Bug", Color: "#FF0000"}
	tags := []domain.ForumTag{
		good,
		{ID: "aa", Name: "Duplicate id", Color: "#00ff00"}, // dupe id
		{ID: "bb", Name: "", Color: "#00ff00"},             // empty name
		{ID: "cc", Name: "Bad colour", Color: "red"},
		{ID: "dd", Name: "Injection", Color: "#fff;background:url(x)"},
		{ID: "UP", Name: "Bad id", Color: "#00ff00"},
		{ID: "ee", Name: "Newline\nname", Color: "#00ff00"},
		{ID: "ff", Name: "Fine", Color: "#0000ff", Emoji: "💡"},
	}
	got := sanitizeForumTags(tags)
	if len(got) != 2 {
		t.Fatalf("kept %d tags, want 2: %+v", len(got), got)
	}
	if got[0].ID != "aa" || got[1].ID != "ff" {
		t.Fatalf("kept the wrong tags: %+v", got)
	}
	// Colour is normalised so comparisons and dedupe are stable.
	if got[0].Color != "#ff0000" {
		t.Errorf("colour not lowercased: %q", got[0].Color)
	}

	// The cap must bound a hostile palette, not merely a careless one.
	var many []domain.ForumTag
	for i := 0; i < 500; i++ {
		many = append(many, domain.ForumTag{ID: domain.NewID(), Name: "t", Color: "#123456"})
	}
	if n := len(sanitizeForumTags(many)); n != maxForumTags {
		t.Fatalf("sanitizeForumTags kept %d of 500, want the %d cap", n, maxForumTags)
	}
}

func TestSanitizePostTagsCapsAndDedupes(t *testing.T) {
	if got := sanitizePostTags([]string{"aa", "aa", "bb"}); len(got) != 2 {
		t.Fatalf("dedupe failed: %+v", got)
	}
	if got := sanitizePostTags([]string{"aa", "BAD ID", "url(x)"}); len(got) != 1 {
		t.Fatalf("bad ids not dropped: %+v", got)
	}
	many := make([]string, 50)
	for i := range many {
		many[i] = domain.NewID()
	}
	if n := len(sanitizePostTags(many)); n != maxPostTags {
		t.Fatalf("kept %d of 50 post tags, want the %d cap", n, maxPostTags)
	}
}

func TestSanitizeForumMetaClearsFieldsThatCannotApply(t *testing.T) {
	// A forum has a palette and no board state of its own.
	forum := domain.Channel{Type: "forum", Pinned: true, Solved: true,
		Tags:      []string{"aa"},
		ForumTags: []domain.ForumTag{{ID: "aa", Name: "Bug", Color: "#ff0000"}}}
	sanitizeForumMeta(&forum)
	if forum.Pinned || forum.Solved || forum.Tags != nil {
		t.Fatalf("forum kept post state: %+v", forum)
	}
	if len(forum.ForumTags) != 1 {
		t.Fatalf("forum lost its palette: %+v", forum.ForumTags)
	}

	// A post has tags and board state, never a palette of its own.
	post := domain.Channel{Type: "thread", Parent: "f", Pinned: true, Solved: true,
		Tags:      []string{"aa", "BAD ID"},
		ForumTags: []domain.ForumTag{{ID: "zz", Name: "Smuggled", Color: "#ff0000"}}}
	sanitizeForumMeta(&post)
	if post.ForumTags != nil {
		t.Fatalf("post kept a palette: %+v", post.ForumTags)
	}
	if len(post.Tags) != 1 || post.Tags[0] != "aa" {
		t.Fatalf("post tags not sanitized: %+v", post.Tags)
	}
	if !post.Pinned || !post.Solved {
		t.Fatal("post lost legitimate board state")
	}

	// An ordinary text channel carries none of it. This is the case a hostile
	// sync payload would use to park unbounded data on a channel nobody inspects.
	text := domain.Channel{Pinned: true, Solved: true, Tags: []string{"aa"},
		ForumTags: []domain.ForumTag{{ID: "aa", Name: "Bug", Color: "#ff0000"}}}
	sanitizeForumMeta(&text)
	if text.Pinned || text.Solved || text.Tags != nil || text.ForumTags != nil {
		t.Fatalf("text channel kept forum metadata: %+v", text)
	}
}

func TestPostExcerptCollapsesAndCuts(t *testing.T) {
	if got := postExcerpt("  hello\n\nthere\tworld  "); got != "hello there world" {
		t.Fatalf("postExcerpt collapsed wrong: %q", got)
	}
	if got := postExcerpt(""); got != "" {
		t.Fatalf("empty body gave %q", got)
	}
	long := strings.Repeat("a", maxPostExcerpt+50)
	got := postExcerpt(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("over-long excerpt not marked as cut: %q", got)
	}
	if n := len([]rune(got)); n != maxPostExcerpt+1 {
		t.Fatalf("excerpt is %d runes, want %d plus the ellipsis", n, maxPostExcerpt)
	}
	// Cut on a rune boundary, never mid-character.
	multi := strings.Repeat("𝔘", maxPostExcerpt+50)
	if got := postExcerpt(multi); !strings.HasSuffix(got, "…") || strings.Contains(got, "�") {
		t.Fatalf("multibyte excerpt mangled: %q", got)
	}
}

// ---- local board behaviour, one service ----

// forumFixture boots one service with a guild and a forum channel in it.
func forumFixture(t *testing.T) (*Service, string, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := startService(t, ctx)
	g, err := svc.CreateGuild("board")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	forum, err := svc.CreateChannel(g.ID, "help", "forum", "")
	if err != nil {
		t.Fatalf("CreateChannel(forum): %v", err)
	}
	return svc, g.ID, forum.ID
}

func TestSetForumTagsMintsIDsAndValidates(t *testing.T) {
	svc, gid, fid := forumFixture(t)

	got, err := svc.SetForumTags(gid, fid, []domain.ForumTag{
		{Name: "Bug", Color: "#ff0000", Emoji: "🐛"},
		{Name: "Idea", Color: "#00FF00"},
	})
	if err != nil {
		t.Fatalf("SetForumTags: %v", err)
	}
	if len(got) != 2 || got[0].ID == "" || got[1].ID == "" {
		t.Fatalf("ids not minted: %+v", got)
	}
	if got[1].Color != "#00ff00" {
		t.Errorf("colour not normalised: %q", got[1].Color)
	}

	// Editing keeps the id, which is what keeps tagged posts tagged.
	again, err := svc.SetForumTags(gid, fid, []domain.ForumTag{
		{ID: got[0].ID, Name: "Defect", Color: "#ff0000"},
	})
	if err != nil {
		t.Fatalf("SetForumTags (edit): %v", err)
	}
	if again[0].ID != got[0].ID {
		t.Fatalf("edit changed the tag id %q -> %q", got[0].ID, again[0].ID)
	}

	// A bad colour is REPORTED on the local path, not silently dropped.
	if _, err := svc.SetForumTags(gid, fid, []domain.ForumTag{{Name: "X", Color: "red"}}); err == nil {
		t.Fatal("SetForumTags accepted a non-hex colour")
	}
	if _, err := svc.SetForumTags(gid, fid, []domain.ForumTag{{Name: "", Color: "#ffffff"}}); err == nil {
		t.Fatal("SetForumTags accepted an empty name")
	}
	over := make([]domain.ForumTag, maxForumTags+1)
	for i := range over {
		over[i] = domain.ForumTag{Name: "t", Color: "#123456"}
	}
	if _, err := svc.SetForumTags(gid, fid, over); err == nil {
		t.Fatalf("SetForumTags accepted %d tags, over the %d cap", len(over), maxForumTags)
	}

	// Not a forum: a text channel has no palette.
	text, err := svc.CreateChannel(gid, "chat", "text", "")
	if err != nil {
		t.Fatalf("CreateChannel(text): %v", err)
	}
	if _, err := svc.SetForumTags(gid, text.ID, []domain.ForumTag{{Name: "X", Color: "#ffffff"}}); err == nil {
		t.Fatal("SetForumTags accepted a text channel")
	}
}

func TestForumBoardDerivesAuthorRepliesAndExcerpt(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	tags, err := svc.SetForumTags(gid, fid, []domain.ForumTag{{Name: "Bug", Color: "#ff0000"}})
	if err != nil {
		t.Fatalf("SetForumTags: %v", err)
	}

	post, err := svc.CreateThread(gid, fid, "Cannot log in", "It says my passphrase is wrong.", []string{tags[0].ID})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if _, err := svc.SendMessage(post.ID, "Have you tried the recovery phrase?", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if _, err := svc.SendMessage(post.ID, "That worked, thanks.", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	board, err := svc.ForumBoard(gid, fid)
	if err != nil {
		t.Fatalf("ForumBoard: %v", err)
	}
	if len(board.Tags) != 1 || board.Tags[0].ID != tags[0].ID {
		t.Fatalf("board palette wrong: %+v", board.Tags)
	}
	if len(board.Posts) != 1 {
		t.Fatalf("board has %d posts, want 1", len(board.Posts))
	}
	p := board.Posts[0]
	if p.Title != "Cannot log in" {
		t.Errorf("title = %q", p.Title)
	}
	// Derived, not carried: the author comes from the opening message's
	// MLS-authenticated sender.
	if p.AuthorFingerprint != svc.id.Fingerprint() {
		t.Errorf("authorFingerprint = %q, want the poster's %q", p.AuthorFingerprint, svc.id.Fingerprint())
	}
	// Two replies after the opening message — the opening one is not a reply.
	if p.Replies != 2 {
		t.Errorf("replies = %d, want 2", p.Replies)
	}
	if p.Excerpt != "It says my passphrase is wrong." {
		t.Errorf("excerpt = %q", p.Excerpt)
	}
	if p.Created == 0 || p.LastActivity == 0 || p.LastActivity < p.Created {
		t.Errorf("times wrong: created=%d lastActivity=%d", p.Created, p.LastActivity)
	}
	if len(p.Tags) != 1 || p.Tags[0] != tags[0].ID {
		t.Errorf("post tags = %+v", p.Tags)
	}
	if p.Unanswered {
		t.Error("a post with replies must not be unanswered")
	}

	// System notices and tombstones must not inflate the reply count: a board
	// that advertises replies a reader cannot find is lying.
	svc.sendSystem(post.ID, "pinned a message")
	before := p.Replies
	board, err = svc.ForumBoard(gid, fid)
	if err != nil {
		t.Fatalf("ForumBoard: %v", err)
	}
	if board.Posts[0].Replies != before {
		t.Errorf("a system notice changed the reply count: %d -> %d", before, board.Posts[0].Replies)
	}
}

func TestForumBoardEmptyPostHasNoAuthor(t *testing.T) {
	// The unsynced/bodyless state the board must design for: a post exists as a
	// channel but its opening message is not here. Author, excerpt and created
	// are all zero, and nothing pretends otherwise.
	svc, gid, fid := forumFixture(t)
	if _, err := svc.CreateThread(gid, fid, "Silent post", "", nil); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	board, err := svc.ForumBoard(gid, fid)
	if err != nil {
		t.Fatalf("ForumBoard: %v", err)
	}
	p := board.Posts[0]
	if p.AuthorFingerprint != "" || p.AuthorName != "" || p.Excerpt != "" || p.Created != 0 || p.Replies != 0 {
		t.Fatalf("an empty post invented metadata: %+v", p)
	}
	if !p.Unanswered {
		t.Error("a post with no replies and no answer mark is unanswered")
	}
	if p.Tags == nil {
		t.Error("Tags must marshal as [] not null, so cards need no null check")
	}
}

func TestPinnedPostsSortFirstAndSolvedClearsUnanswered(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	older, err := svc.CreateThread(gid, fid, "Older", "one", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	// Distinct timestamps, so "newest activity first" is actually being tested.
	time.Sleep(5 * time.Millisecond)
	newer, err := svc.CreateThread(gid, fid, "Newer", "two", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}

	board, _ := svc.ForumBoard(gid, fid)
	if board.Posts[0].ID != newer.ID {
		t.Fatalf("default order is not newest-first: %s before %s", board.Posts[0].Title, board.Posts[1].Title)
	}

	if err := svc.SetPostPinned(gid, older.ID, true); err != nil {
		t.Fatalf("SetPostPinned: %v", err)
	}
	board, _ = svc.ForumBoard(gid, fid)
	if board.Posts[0].ID != older.ID || !board.Posts[0].Pinned {
		t.Fatalf("pinned post did not sort first: %+v", board.Posts)
	}

	// Pin state survives a re-announce of the same channel: addChannel is
	// idempotent, and a post that reset its own board state on every gossip
	// re-delivery would unpin itself at random.
	svc.addChannel(gid, domain.Channel{ID: older.ID, GuildID: gid, Name: "Older", Type: "thread", Parent: fid})
	board, _ = svc.ForumBoard(gid, fid)
	if !board.Posts[0].Pinned {
		t.Fatal("re-announcing a post cleared its pin")
	}

	if err := svc.SetPostSolved(gid, newer.ID, true); err != nil {
		t.Fatalf("SetPostSolved: %v", err)
	}
	board, _ = svc.ForumBoard(gid, fid)
	for _, p := range board.Posts {
		if p.ID == newer.ID {
			if !p.Solved || p.Unanswered {
				t.Fatalf("solved post still unanswered: %+v", p)
			}
		}
	}
}

func TestPostTagsMustExistInThePalette(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	tags, _ := svc.SetForumTags(gid, fid, []domain.ForumTag{{Name: "Bug", Color: "#ff0000"}})
	post, err := svc.CreateThread(gid, fid, "P", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if err := svc.SetPostTags(gid, post.ID, []string{tags[0].ID}); err != nil {
		t.Fatalf("SetPostTags: %v", err)
	}
	if err := svc.SetPostTags(gid, post.ID, []string{domain.NewID()}); err == nil {
		t.Fatal("SetPostTags accepted a tag this forum does not define")
	}
	over := make([]string, maxPostTags+1)
	for i := range over {
		over[i] = tags[0].ID
	}
	if _, err := svc.CreateThread(gid, fid, "over", "body", over); err == nil {
		t.Fatalf("CreateThread accepted %d tags, over the %d per-post cap", len(over), maxPostTags)
	}
	if err := svc.SetPostTags(gid, post.ID, over); err == nil {
		t.Fatal("SetPostTags accepted more than the per-post tag cap")
	}
	// A plain text channel is not a post.
	text, _ := svc.CreateChannel(gid, "chat", "text", "")
	if err := svc.SetPostTags(gid, text.ID, nil); err == nil {
		t.Fatal("SetPostTags accepted a text channel")
	}
	if err := svc.SetPostPinned(gid, text.ID, true); err == nil {
		t.Fatal("SetPostPinned accepted a text channel")
	}
}

// TestRemotePostMetaIsAuthorized drives the RECEIVE path directly — the same
// function a peer's gossip frame lands in — because that is where a patched
// client would try to pin its own post or retag someone else's.
func TestRemotePostMetaIsAuthorized(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	tags, _ := svc.SetForumTags(gid, fid, []domain.ForumTag{{Name: "Bug", Color: "#ff0000"}})
	post, err := svc.CreateThread(gid, fid, "P", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}

	stranger := strings.Repeat("f", 64) // a member with no roles: no permissions
	yes := true
	newTags := []string{tags[0].ID}

	// A member with neither ManageMessages nor authorship changes nothing.
	svc.applyPostMeta(gid, stranger, guildMeta{
		Type: "post_meta", ChannelID: post.ID,
		PostTags: &newTags, PostPinned: &yes, PostSolved: &yes,
	})
	board, _ := svc.ForumBoard(gid, fid)
	if p := board.Posts[0]; p.Pinned || p.Solved || len(p.Tags) != 0 {
		t.Fatalf("an unauthorized peer curated the post: %+v", p)
	}

	// The OWNER holds every permission, so the same frame from them applies.
	svc.applyPostMeta(gid, svc.id.Fingerprint(), guildMeta{
		Type: "post_meta", ChannelID: post.ID,
		PostTags: &newTags, PostPinned: &yes, PostSolved: &yes,
	})
	board, _ = svc.ForumBoard(gid, fid)
	if p := board.Posts[0]; !p.Pinned || !p.Solved || len(p.Tags) != 1 {
		t.Fatalf("an authorized frame was dropped: %+v", p)
	}

	// A nil field means UNCHANGED, not false. This is why they are pointers: a
	// frame that only retags a post must not silently unpin it.
	only := []string{}
	svc.applyPostMeta(gid, svc.id.Fingerprint(), guildMeta{
		Type: "post_meta", ChannelID: post.ID, PostTags: &only,
	})
	board, _ = svc.ForumBoard(gid, fid)
	if p := board.Posts[0]; !p.Pinned || !p.Solved || len(p.Tags) != 0 {
		t.Fatalf("a tags-only frame changed pin/solved: %+v", p)
	}

	// An unknown channel, and a channel from no forum, are both refused.
	svc.applyPostMeta(gid, svc.id.Fingerprint(), guildMeta{
		Type: "post_meta", ChannelID: domain.NewID(), PostPinned: &yes})
	svc.applyForumTags(gid, stranger, fid, []domain.ForumTag{{ID: "aa", Name: "Sneak", Color: "#000000"}})
	board, _ = svc.ForumBoard(gid, fid)
	if len(board.Tags) != 1 || board.Tags[0].Name != "Bug" {
		t.Fatalf("an unauthorized peer replaced the palette: %+v", board.Tags)
	}
}

// TestRemoteForumTagsAreValidated proves a peer's palette goes through the same
// validation ours does — the frame is not dropped whole (that would let any
// moderator keep a forum untagged by malforming one entry), but nothing that
// could escape a CSS context survives.
func TestRemoteForumTagsAreValidated(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	svc.applyForumTags(gid, svc.id.Fingerprint(), fid, []domain.ForumTag{
		{ID: "aa", Name: "Fine", Color: "#00FF00"},
		{ID: "bb", Name: "Bad", Color: "#fff;background:url(evil)"},
		{ID: "cc", Name: "Line\nbreak", Color: "#000000"},
		{ID: "UPPER", Name: "Bad id", Color: "#000000"},
	})
	board, _ := svc.ForumBoard(gid, fid)
	if len(board.Tags) != 1 || board.Tags[0].ID != "aa" || board.Tags[0].Color != "#00ff00" {
		t.Fatalf("remote palette not sanitized: %+v", board.Tags)
	}
}

// TestForumMetaSurvivesAChannelMetaUpdate is the clobbering trap: SetChannelMeta
// publishes a bare four-field Channel, so if the palette rode on
// channel_updated, renaming a category would erase every tag in the forum.
func TestForumMetaSurvivesAChannelMetaUpdate(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	if _, err := svc.SetForumTags(gid, fid, []domain.ForumTag{{Name: "Bug", Color: "#ff0000"}}); err != nil {
		t.Fatalf("SetForumTags: %v", err)
	}
	post, err := svc.CreateThread(gid, fid, "P", "body", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if err := svc.SetPostPinned(gid, post.ID, true); err != nil {
		t.Fatalf("SetPostPinned: %v", err)
	}

	// Move/retopic the forum, and move the post, through the very call whose
	// announcement carries a bare four-field Channel.
	if err := svc.SetChannelMeta(gid, fid, "forum", "", 3, "House rules"); err != nil {
		t.Fatalf("SetChannelMeta(forum): %v", err)
	}
	if err := svc.SetChannelMeta(gid, post.ID, "thread", "", 1, ""); err != nil {
		t.Fatalf("SetChannelMeta(post): %v", err)
	}

	board, err := svc.ForumBoard(gid, fid)
	if err != nil {
		t.Fatalf("ForumBoard: %v", err)
	}
	if len(board.Tags) != 1 {
		t.Fatalf("SetChannelMeta erased the palette: %+v", board.Tags)
	}
	if board.Topic != "House rules" {
		t.Errorf("topic not applied: %q", board.Topic)
	}
	if !board.Posts[0].Pinned {
		t.Fatal("a SetChannelMeta on the post unpinned it")
	}
}

// TestMayAddChannelAcceptsMemberPostsOnly is the regression test for the bug this
// work uncovered: an inbound channel_added demanded ManageChannels, so an
// ordinary member's forum post was dropped by every peer's gossip path. It
// asserts the gate directly, because end-to-end the symptom is masked — history
// sync adopts a peer's channel list wholesale and repairs it on a delay, which is
// exactly why the bug went unnoticed.
func TestMayAddChannelAcceptsMemberPostsOnly(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	stranger := strings.Repeat("f", 64) // a member holding no roles: no permissions
	if svc.memberHasPerm(gid, stranger, PermManageChannels) {
		t.Fatal("the stranger holds ManageChannels; the test proves nothing")
	}

	// A post under a real forum: allowed with no permissions at all.
	post := domain.Channel{ID: domain.NewID(), Type: "thread", Parent: fid}
	if !svc.mayAddChannel(gid, stranger, post) {
		t.Error("an ordinary member's forum post was refused")
	}

	// Everything else still needs ManageChannels — the exemption must not become
	// a hole a member can push arbitrary channels through.
	for name, ch := range map[string]domain.Channel{
		"text channel":             {ID: domain.NewID()},
		"voice room":               {ID: domain.NewID(), Type: "voice"},
		"another forum":            {ID: domain.NewID(), Type: "forum"},
		"thread with no parent":    {ID: domain.NewID(), Type: "thread"},
		"thread under a non-forum": {ID: domain.NewID(), Type: "thread", Parent: domain.NewID()},
	} {
		if svc.mayAddChannel(gid, stranger, ch) {
			t.Errorf("a member with no permissions was allowed to add a %s", name)
		}
	}

	// The owner holds everything, so all of it is allowed for them.
	if !svc.mayAddChannel(gid, svc.id.Fingerprint(), domain.Channel{ID: domain.NewID(), Type: "voice"}) {
		t.Error("the owner was refused a voice channel")
	}
}

// ---- two peers: propagation and the compatibility story ----

// TestOrdinaryMemberPostReachesPeers is the end-to-end counterpart: a member with
// no permissions posts in the owner's forum, with a tag from the owner's palette,
// and the owner sees the post AND the tag. It does not pin down WHICH lane
// carried it (gossip or a sync repair) — see TestMayAddChannelAcceptsMemberPostsOnly
// for the gate itself — but it is what proves tags survive a real MLS round trip.
func TestOrdinaryMemberPostReachesPeers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)

	g, err := owner.CreateGuild("forum-guild")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	forum, err := owner.CreateChannel(g.ID, "help", "forum", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	tags, err := owner.SetForumTags(g.ID, forum.ID, []domain.ForumTag{{Name: "Bug", Color: "#ff0000"}})
	if err != nil {
		t.Fatalf("SetForumTags: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)

	// The joiner holds no roles, so it has NO permissions at all — which is the
	// whole point. Its palette arrives with the guild snapshot.
	if member.hasPerm(g.ID, PermManageChannels) {
		t.Fatal("the joiner unexpectedly holds ManageChannels; the test proves nothing")
	}
	var memberTag string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := member.ForumBoard(g.ID, forum.ID); err == nil && len(b.Tags) == 1 {
			memberTag = b.Tags[0].ID
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if memberTag != tags[0].ID {
		t.Fatalf("the member never learned the forum palette (got %q)", memberTag)
	}

	// Retried like every other publish in these tests: a warming gossip mesh
	// drops the first frame often enough that a single attempt is flaky.
	var seen bool
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && !seen {
		if _, err := member.CreateThread(g.ID, forum.ID, "Member post", "please help", []string{memberTag}); err != nil {
			t.Fatalf("member CreateThread: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
		b, err := owner.ForumBoard(g.ID, forum.ID)
		if err != nil {
			t.Fatalf("owner ForumBoard: %v", err)
		}
		for _, p := range b.Posts {
			if p.Title == "Member post" {
				seen = true
				if len(p.Tags) != 1 || p.Tags[0] != memberTag {
					t.Errorf("the post's tags did not travel: %+v", p.Tags)
				}
				// A member without ManageMessages cannot open a post pre-pinned.
				if p.Pinned {
					t.Error("an ordinary member's post arrived pinned")
				}
			}
		}
	}
	if !seen {
		t.Fatal("an ordinary member's forum post never reached the owner")
	}
}

// The sanitize funnel had no test at all: commenting out the call in addChannel
// left the entire suite green. That is the call site the whole design rests on —
// history sync adopts a peer-supplied Channel wholesale, so a validator wired
// only into the gossip path leaves the sync path open.
func TestAddChannelSanitizesAPeerSuppliedChannel(t *testing.T) {
	svc, gid, _ := forumFixture(t)

	hostile := domain.Channel{
		ID: domain.NewID(), GuildID: gid, Name: "gift", Type: "forum",
		Banner: `data:image/png;base64,AA);background:url(http://tracker.example/x)`,
	}
	// Far past the per-forum cap, each with a colour that would reach a CSS rule.
	for i := 0; i < 500; i++ {
		hostile.ForumTags = append(hostile.ForumTags, domain.ForumTag{
			ID: domain.NewID(), Name: "x", Color: "#fff;background:url(http://evil)",
		})
	}
	svc.addChannel(gid, hostile)

	var got domain.Channel
	for _, g := range mustGuilds(t, svc) {
		for _, c := range g.Channels {
			if c.ID == hostile.ID {
				got = c
			}
		}
	}
	if got.ID == "" {
		t.Fatal("channel was not added at all")
	}
	if got.Banner != "" {
		t.Errorf("a CSS-breakout banner survived addChannel: %q", got.Banner)
	}
	if n := len(got.ForumTags); n > maxForumTags {
		t.Errorf("palette kept %d tags, cap is %d", n, maxForumTags)
	}
	for _, tg := range got.ForumTags {
		if !validHexColor(tg.Color) {
			t.Errorf("a tag colour that is not a hex value survived: %q", tg.Color)
		}
	}
}

func mustGuilds(t *testing.T, svc *Service) []domain.Guild {
	t.Helper()
	return svc.Guilds()
}
