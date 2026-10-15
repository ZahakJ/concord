package app

import (
	"context"
	"testing"
	"time"
)

// TestJoinQuitRestartStillHears: two members each join by invite and exit at
// once, then come back, with no rendezvous and no mDNS — only the peer cache to
// find the owner by. Both must hear the guild afterwards. A member whose cache
// held only private addresses ran no DHT, the remembered-peer dial lived in the
// DHT's loop, and it came back connected to nobody
// (TestRememberedPeerRedialledWithoutDHT in internal/net pins the mechanism).
func TestJoinQuitRestartStillHears(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	g, err := a.CreateGuild("Exp")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	bDir, cDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{bDir, cDir} {
		s := startServiceInDir(t, ctx, dir)
		if _, err := s.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
		_ = s.Close()
	}
	b := startServiceInDir(t, ctx, bDir)
	c := startServiceInDir(t, ctx, cDir)
	rb, rc := &recorder{}, &recorder{}
	b.OnMessage(rb.add)
	c.OnMessage(rc.add)
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := a.SendMessage(channel, "hello-everyone", "", ""); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		time.Sleep(2 * time.Second)
		if rb.has("hello-everyone") && rc.has("hello-everyone") {
			return
		}
	}
	t.Fatalf("after join-quit-restart: B hears=%v C hears=%v (B out of sync=%v, C out of sync=%v)",
		rb.has("hello-everyone"), rc.has("hello-everyone"), b.OutOfSync(g.ID), c.OutOfSync(g.ID))
}
