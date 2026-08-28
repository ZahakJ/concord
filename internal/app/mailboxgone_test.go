package app

import (
	"context"
	"testing"
	"time"
)

// The same acceptance test, with the sender gone.
//
// TestMailboxOfflineDelivery leaves A running while B returns, and A is
// reachable: B remembers its address from before and dials it on start, so the
// message can arrive over ordinary catch-up and the mailbox is never asked for
// anything. Closing A first is what makes the deposit the only path there is.
func TestMailboxDeliversWithTheSenderGone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, nodeAddr := startMailboxNode(t, ctx)
	aDir, bDir := t.TempDir(), t.TempDir()
	a := startServiceWithBootstrap(t, ctx, aDir, []string{nodeAddr})
	b := startServiceWithBootstrap(t, ctx, bDir, []string{nodeAddr})

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)
	waitUntil(t, 20*time.Second, func() bool {
		_, ok := a.mailboxPubFor(b.Fingerprint())
		return ok && len(b.mailboxNodes()) > 0
	}, "A never learned B's mailbox key / B never reached the node")

	_ = b.Close()
	waitUntil(t, 15*time.Second, func() bool {
		for _, p := range a.host.Peers() {
			if presenceFor(p).Fingerprint == b.Fingerprint() {
				return false
			}
		}
		return true
	}, "A still sees B as connected")

	for _, body := range []string{"one", "two", "three"} {
		if _, err := a.SendMessage(channel, body, "", ""); err != nil {
			t.Fatalf("SendMessage %q: %v", body, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(3 * time.Second)
	_ = a.Close() // THE DIFFERENCE: nobody is left who holds this message

	b2 := startServiceWithBootstrap(t, ctx, bDir, []string{nodeAddr})
	waitUntil(t, 60*time.Second, func() bool {
		msgs, _ := b2.Messages(channel, 0)
		got := map[string]bool{}
		for _, m := range msgs {
			got[m.Content] = true
		}
		return got["one"] && got["two"] && got["three"]
	}, "the mailbox did not deliver every message whose only other copy was offline")
	msgs, _ := b2.Messages(channel, 0)
	var bodies []string
	for _, m := range msgs {
		bodies = append(bodies, m.Content)
	}
	t.Logf("B holds: %v", bodies)
}
