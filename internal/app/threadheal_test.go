package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// TestMissedOpenerInANewPostHealsWithinABeat: a message missed on the live
// lane must come back on the next reconcile, including one in a post (thread
// channel) announced after this device last synced, and including when the
// only member still holding it is its author. A beat used to stop at the
// first member that answered — the owner, here, who had missed it too — so it
// could ask the same member every minute and never find it.
func TestMissedOpenerInANewPostHealsWithinABeat(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, _ := guildOf(t, ctx, 3)
	a, b, c := svcs[0], svcs[1], svcs[2]
	if err := c.host.Connect(ctx, b.host.AddrInfo()); err != nil {
		t.Fatalf("connect C to B: %v", err)
	}
	forum, err := a.CreateChannel(guildID, "help", "forum", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	waitUntil(t, 15*time.Second, func() bool {
		_, okB := b.channelGuild(forum.ID)
		_, okC := c.channelGuild(forum.ID)
		return okB && okC
	}, "the forum never reached the members")
	// Everyone has synced with everyone: marks exist from before the post.
	for _, s := range svcs {
		for _, p := range svcs {
			if s != p {
				mustSync(t, s, guildID, p)
			}
		}
	}

	// Missed on the live lane: everyone but its author drops the opener.
	drop := func(m domain.Message) bool { return m.Content == "the opening message" }
	a.dropLive.Store(&drop)
	c.dropLive.Store(&drop)
	post, err := b.CreateThread(guildID, forum.ID, "Cannot log in", "the opening message", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	holds := func(s *Service) bool {
		msgs, _ := s.Messages(post.ID, 0)
		for _, m := range msgs {
			if m.Content == "the opening message" {
				return true
			}
		}
		return false
	}
	waitUntil(t, 15*time.Second, func() bool {
		_, okA := a.channelGuild(post.ID)
		_, okC := c.channelGuild(post.ID)
		return okA && okC
	}, "the post never reached the others")
	if _, err := b.SendMessage(post.ID, "a reply", "", ""); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitUntil(t, 15*time.Second, func() bool {
		msgs, _ := c.Messages(post.ID, 0)
		return len(msgs) > 0
	}, "the reply never arrived live")
	if holds(a) || holds(c) {
		t.Fatal("the opener was not missed")
	}
	a.dropLive.Store(nil)
	c.dropLive.Store(nil)

	// One beat's catch-up for this guild, run to completion.
	c.syncGuildFromAnyPeer(guildID)
	if !holds(c) {
		t.Fatal("one reconcile beat did not bring back a message missed in a new post")
	}
}
