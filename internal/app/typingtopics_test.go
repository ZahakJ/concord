package app

import (
	"context"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

// A joined gossipsub topic is a mesh, and a mesh costs heartbeats whether or not
// anything ever crosses it. Concord opens two topics per channel — messages and
// typing — so half of a guild's per-channel gossip floor is a feature that
// cannot possibly be used while the app is off screen.
//
// What this pins is the asymmetry, which is the whole point: typing goes,
// messages stay. A backgrounded phone that stopped meshing its message topics
// would stop receiving messages, which is the one thing background mode has
// always been careful not to do.

func typingTopicsTestService(t *testing.T, ctx context.Context) *Service {
	t.Helper()
	svc, err := Start(ctx, Config{
		DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true,
	})
	if err != nil {
		t.Fatalf("Start service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func TestBackgroundingLeavesTypingTopicsAndForegroundRejoins(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := typingTopicsTestService(t, ctx)
	g, err := svc.CreateGuild("Quiet")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	if len(g.Channels) == 0 {
		t.Fatal("a new guild came with no channels to watch")
	}
	channelID := g.Channels[0].ID
	typing := domain.TypingTopicID(g.GroupID, channelID)
	messages := domain.TopicID(g.GroupID, channelID)

	joined := func(topic string) bool { return svc.ps.Subscribed(topic) }

	if !joined(typing) {
		t.Fatal("a foreground app did not join its typing topic at all")
	}
	if !joined(messages) {
		t.Fatal("the message topic was never joined; the rig is wrong, not the code")
	}

	// Off screen.
	svc.SetBackground(true)
	if joined(typing) {
		t.Fatal("the typing topic stayed meshed while the app was off screen — the whole saving is that it does not")
	}
	if !joined(messages) {
		t.Fatal("backgrounding dropped the MESSAGE topic; a backgrounded phone must keep receiving messages")
	}

	// A stray keystroke arriving across the transition must not undo it:
	// publishing floods every peer subscribed to the topic and opens a fanout
	// set, which is exactly the traffic the leave was for.
	if err := svc.SendTyping(channelID); err != nil {
		t.Fatalf("SendTyping while backgrounded: %v", err)
	}
	if joined(typing) {
		t.Fatal("publishing typing while backgrounded re-joined the topic")
	}

	// A channel created while off screen must not quietly re-open one either.
	newCh, err := svc.CreateChannel(g.ID, "later", "text", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if joined(domain.TypingTopicID(g.GroupID, newCh.ID)) {
		t.Fatal("a channel added while backgrounded joined its typing topic")
	}

	// Back on screen. Both channels' typing topics come back, including the one
	// that never had a subscription to restore.
	svc.SetBackground(false)
	if !joined(typing) {
		t.Fatal("returning to the foreground did not re-join the typing topic; typing indicators would be dead for the session")
	}
	if !joined(domain.TypingTopicID(g.GroupID, newCh.ID)) {
		t.Fatal("the channel added while backgrounded never got its typing topic")
	}
	if !joined(messages) {
		t.Fatal("the message topic did not survive the round trip")
	}

	// Idempotence in both directions: the shells call this on every screen
	// transition, and Android sends duplicates.
	svc.SetBackground(false)
	svc.syncTypingTopics()
	if !joined(typing) {
		t.Fatal("a redundant foreground sweep dropped the typing topic")
	}
	svc.SetBackground(true)
	svc.SetBackground(true)
	svc.syncTypingTopics()
	if joined(typing) {
		t.Fatal("a redundant background sweep left the typing topic joined")
	}
	svc.SetBackground(false)
	if !joined(typing) || !joined(messages) {
		t.Fatal("the topics did not recover from a second background/foreground cycle")
	}
}
