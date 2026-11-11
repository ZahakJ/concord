package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

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
	// And no catch-up may bring it back before the beat under test: B, the one
	// member holding it, answers none until then. The catch-ups that run on
	// their own (a connection's ten-second second look, the beat) used to land
	// in this window in most runs, on 76c380e too, and heal the opener before
	// the beat under test ran ("the opener was not missed").
	b.host.HandleSync(func(context.Context, peer.ID, []byte) ([]byte, error) {
		return nil, errors.New("not answering catch-ups during the test's window")
	})
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
	// The reply has to go out live, so B must already know both of them are
	// listening on the new post's topic. Published before that, it missed C's
	// live lane too and arrived only by catch-up, which the window above now
	// holds back ("the reply never arrived live").
	b.mu.RLock()
	topic := domain.TopicID(b.guilds[guildID].GroupID, post.ID)
	b.mu.RUnlock()
	waitUntil(t, 15*time.Second, func() bool {
		seen := map[peer.ID]bool{}
		for _, p := range b.ps.TopicPeers(topic) {
			seen[p] = true
		}
		return seen[a.host.PeerID()] && seen[c.host.PeerID()]
	}, "the others never joined the post's topic")
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
	b.host.HandleSync(b.handleSyncRequest)

	// One beat's catch-up for this guild, run to completion.
	c.syncGuildFromAnyPeer(guildID)
	if !holds(c) {
		t.Fatal("one reconcile beat did not bring back a message missed in a new post")
	}
}
