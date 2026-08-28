package app

import (
	"context"
	"testing"
)

// The moderation log opened with "every role change, ban, mute and handover"
// and offered a Channels tab that could only ever show slow mode. Deleting a
// channel full of messages — gone, for everybody — left no trace at all, so
// "who deleted #introductions and when" had no answer anywhere in the product.
// These assert that the destructive acts now reach the log, and that they reach
// it as SIGNED entries the reader's own machine agrees with, which is the
// standard the rest of the screen already sets.
func TestModerationLogRecordsChannelAndGuildChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	g, err := owner.CreateGuild("Riverside Makers")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	ch, err := owner.CreateChannel(g.ID, "welcome", "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if err := owner.RenameChannel(g.ID, ch.ID, "welcome-and-rules"); err != nil {
		t.Fatalf("RenameChannel: %v", err)
	}
	cat, err := owner.CreateCategory(g.ID, "Commons")
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if err := owner.SetChannelMeta(g.ID, ch.ID, "", cat.ID, 0, ""); err != nil {
		t.Fatalf("SetChannelMeta: %v", err)
	}
	if err := owner.DeleteChannel(g.ID, ch.ID); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if err := owner.RenameGuild(g.ID, "Riverside Makers Guild"); err != nil {
		t.Fatalf("RenameGuild: %v", err)
	}
	if err := owner.AddCustomEmoji(g.ID, "parrot", "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="); err != nil {
		t.Fatalf("AddCustomEmoji: %v", err)
	}
	if err := owner.RemoveCustomEmoji(g.ID, "parrot"); err != nil {
		t.Fatalf("RemoveCustomEmoji: %v", err)
	}

	entries, err := owner.GovernanceLog(g.ID, 0, 100)
	if err != nil {
		t.Fatalf("GovernanceLog: %v", err)
	}
	byType := map[string]GovLogEntry{}
	for _, e := range entries {
		if !e.Verified || !e.Applied {
			t.Fatalf("%s read as verified=%v applied=%v — the owner's own act", e.Type, e.Verified, e.Applied)
		}
		byType[e.Type] = e
	}
	for _, want := range []string{
		"channel_create", "channel_rename", "channel_move", "channel_delete",
		"guild_rename", "emoji_add", "emoji_remove",
	} {
		if _, ok := byType[want]; !ok {
			t.Fatalf("%s never reached the moderation log", want)
		}
	}

	// The two facts that make a row worth reading rather than merely present.
	if got := byType["channel_delete"].ChannelName; got != "welcome-and-rules" {
		t.Fatalf("the delete row names the channel %q — it has to be read before the channel goes", got)
	}
	if got := byType["channel_rename"].PrevName; got != "welcome" {
		t.Fatalf("the rename row says it came from %q, want %q", got, "welcome")
	}
	if got := byType["guild_rename"].Name; got != "Riverside Makers Guild" {
		t.Fatalf("the guild rename row names %q", got)
	}
	if got := byType["emoji_add"].Name; got != "parrot" {
		t.Fatalf("the emoji row names %q", got)
	}
}

// A forum POST is a channel too, and anyone may start one. Logging its deletion
// would fill an audit trail with member activity and, worse, print a "refused"
// verdict on every row — the author who deleted their own post holds no
// manage-channels bit. Rooms are logged; posts are not.
func TestForumPostDeletionStaysOutOfTheModerationLog(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	g, err := owner.CreateGuild("Riverside Makers")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	forum, err := owner.CreateChannel(g.ID, "help-desk", "forum", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	post, err := owner.CreateThread(g.ID, forum.ID, "how do I mount a shelf", "first", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if err := owner.DeleteChannel(g.ID, post.ID); err != nil {
		t.Fatalf("DeleteChannel(post): %v", err)
	}

	entries, _ := owner.GovernanceLog(g.ID, 0, 100)
	for _, e := range entries {
		if e.ChannelID == post.ID {
			t.Fatalf("a forum post's lifecycle reached the moderation log as %s", e.Type)
		}
	}
}
