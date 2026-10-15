package app

import (
	"context"
	"testing"
	"time"
)

// TestForumPostArrivesWithItsOpener: a member's new post reached everyone else
// without its opening message. The opener went out on the post's own topic the
// instant the post was announced, before anybody had seen the announcement and
// subscribed to that topic, so it reached nobody; the replies that followed
// arrived at once, and the board showed the first reply as the post until a
// catch-up minutes later. The opener has to arrive with the post.
func TestForumPostArrivesWithItsOpener(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, _ := guildOf(t, ctx, 3)
	a, b, c := svcs[0], svcs[1], svcs[2]
	forum, err := a.CreateChannel(guildID, "help", "forum", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	waitUntil(t, 15*time.Second, func() bool {
		for _, s := range []*Service{b, c} {
			if _, ok := s.channelGuild(forum.ID); !ok {
				return false
			}
		}
		return true
	}, "the forum never reached the members")

	// B, not the owner, opens a post.
	post, err := b.CreateThread(guildID, forum.ID, "Cannot log in", "the opening message", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	// The opener must be there when the post is. Before, it could only be
	// recovered after the fact — by gossip's short-lived re-offer of recent
	// messages if the subscriber joined the topic within a few heartbeats
	// (in a quiet test, about a second later; on a busy network, often never),
	// or by a catch-up minutes later. So the bound here is far below a
	// heartbeat, measured from the moment each peer learns of the post.
	held := func(s *Service) bool {
		msgs, err := s.Messages(post.ID, 0)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.Content == "the opening message" {
				return true
			}
		}
		return false
	}
	for name, s := range map[string]*Service{"owner": a, "member": c} {
		waitUntil(t, 10*time.Second, func() bool {
			_, ok := s.channelGuild(post.ID)
			return ok
		}, "the post never reached the "+name)
		deadline := time.Now().Add(250 * time.Millisecond)
		for !held(s) {
			if time.Now().After(deadline) {
				t.Fatalf("the %s got the post without its opening message", name)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
