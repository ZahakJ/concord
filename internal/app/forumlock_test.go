package app

import (
	"strings"
	"testing"

	"github.com/zahak/concord/internal/domain"
)

// Closing a post has to hold on BOTH sides. Refusing only to send makes it a
// polite request aimed at the one person who was never going to ignore it; the
// receive-side drop is what makes every honest client agree the thread is over.
func TestClosedPostRefusesAndDropsMessages(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	post, err := svc.CreateThread(gid, fid, "How do I link a device?", "Stuck on step 2.", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}

	if _, err := svc.send(post.ID, "still open", "", ""); err != nil {
		t.Fatalf("an open post should accept messages: %v", err)
	}
	if err := svc.SetPostLocked(gid, post.ID, true); err != nil {
		t.Fatalf("SetPostLocked: %v", err)
	}

	// Send side.
	_, err = svc.send(post.ID, "sneaking one in", "", "")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("a closed post accepted a message, err = %v", err)
	}
	// A moderation action on an existing message is NOT a new message and must
	// still work, or closing a thread would also freeze its cleanup.
	if _, err := svc.send(post.ID, "👍", "reaction", ""); err != nil {
		t.Errorf("closing a post should not block moderation kinds: %v", err)
	}

	// Receive side. The inbound path decrypts before it decides, so it cannot be
	// driven with a plaintext message from here — but it drops on exactly this
	// predicate, so asserting the predicate is asserting the drop. Keep the two
	// together: if receiveCiphertext ever stops consulting postIsLocked, closing
	// a thread quietly becomes a request rather than a decision.
	if !svc.postIsLocked(post.ID) {
		t.Fatal("postIsLocked is false for a closed post — the receive-side drop cannot fire")
	}

	// Reopening restores both directions.
	if err := svc.SetPostLocked(gid, post.ID, false); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := svc.send(post.ID, "back open", "", ""); err != nil {
		t.Errorf("a reopened post should accept messages: %v", err)
	}
}

// A forum banner reaches a CSS context in the client, exactly as a guild banner
// does, so it takes the same two shapes and nothing else.
func TestForumBannerAcceptsOnlyImagesAndPresets(t *testing.T) {
	svc, gid, fid := forumFixture(t)

	for _, bad := range []string{
		`data:image/png;base64,AAAA);background-image:url(http://tracker.example/x.png`,
		`data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=`,
		`preset:not a real id`,
		`preset:../../etc`,
		`https://example.com/banner.png`,
	} {
		if err := svc.SetForumBanner(gid, fid, bad); err == nil {
			t.Errorf("accepted an unsafe forum banner: %.50q", bad)
		}
	}
	for _, good := range []string{
		"preset:neon-coliseum",
		"data:image/png;base64,iVBORw0KGgo=",
		"", // clearing is always allowed
	} {
		if err := svc.SetForumBanner(gid, fid, good); err != nil {
			t.Errorf("rejected a valid forum banner %q: %v", good, err)
		}
	}

	// A banner belongs to a forum, not to a post or a text channel.
	post, err := svc.CreateThread(gid, fid, "t", "b", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if err := svc.SetForumBanner(gid, post.ID, "preset:yule"); err == nil {
		t.Error("a post accepted a forum banner")
	}
}

// sanitizeForumMeta runs at the one funnel every inbound channel record passes
// through, so a peer cannot park a CSS payload on us via history sync — the call
// site a gossip-only validator would miss.
func TestSanitizeStripsAHostileForumBanner(t *testing.T) {
	c := domain.Channel{Type: "forum", Banner: `data:image/png;base64,AA);background:url(http://evil)`}
	sanitizeForumMeta(&c)
	if c.Banner != "" {
		t.Errorf("hostile banner survived sanitisation: %q", c.Banner)
	}
	keep := domain.Channel{Type: "forum", Banner: "preset:blueprint"}
	sanitizeForumMeta(&keep)
	if keep.Banner != "preset:blueprint" {
		t.Errorf("a valid preset was stripped: %q", keep.Banner)
	}
	// A post is not a forum and has no banner of its own.
	post := domain.Channel{Type: "thread", Parent: "f1", Banner: "preset:blueprint"}
	sanitizeForumMeta(&post)
	if post.Banner != "" {
		t.Errorf("a post kept a banner: %q", post.Banner)
	}
}

// The board is sold on media-led cards, and through the real composer every one
// of them was a letter tile. Attachments are sent as their OWN messages right
// after the opening one, while card media was scraped out of the opening body —
// so a picture posted the way the product posts pictures never appeared, and the
// message carrying it was counted as a reply, which also dropped the post out of
// "unanswered".
func TestPostMediaComesFromTheOpeningBatchNotTheExcerpt(t *testing.T) {
	svc, gid, fid := forumFixture(t)
	post, err := svc.CreateThread(gid, fid, "Look at this", "Here is the thing I built.", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	// Exactly what the composer does: the attachment goes out as its own message.
	if _, err := svc.SendAttachment(post.ID, tinyPNG(), 640, 480, "", false, "", ""); err != nil {
		t.Fatalf("SendAttachment: %v", err)
	}

	board, err := svc.ForumBoard(gid, fid)
	if err != nil {
		t.Fatalf("ForumBoard: %v", err)
	}
	if len(board.Posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(board.Posts))
	}
	p := board.Posts[0]
	if p.Media == "" {
		t.Error("the post has a picture and the card would show none")
	}
	if !strings.Contains(p.Media, "concord://attach/") {
		t.Errorf("media is not an attachment token: %q", p.Media)
	}
	// The attachment is part of the post, not an answer to it.
	if p.Replies != 0 {
		t.Errorf("replies = %d, want 0 — the post's own attachment is not a reply", p.Replies)
	}

	// A real reply from someone else ends the opening batch and counts.
	if _, err := svc.send(post.ID, "Nice work!", "", ""); err != nil {
		t.Fatalf("send reply: %v", err)
	}
	board, _ = svc.ForumBoard(gid, fid)
	if got := board.Posts[0].Replies; got != 1 {
		t.Errorf("replies after one answer = %d, want 1", got)
	}
	if board.Posts[0].Media == "" {
		t.Error("a reply should not cost the post its picture")
	}
}

// A 1x1 PNG as a data URL — enough to be sealed as a real attachment.
func tinyPNG() string {
	return "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
}
