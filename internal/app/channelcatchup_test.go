package app

import (
	"context"
	"testing"
	"time"
)

// A member who was offline while a channel was renamed, given a topic and
// moved into a category must come back to all three. They used to keep the
// old name and no topic for good: catch-up adopted only channels they did not
// have, and the meta frames that carry those edits reach only who is online.
func TestChannelEditsReachAMemberWhoWasAway(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	cDir := t.TempDir()
	c := startServiceInDir(t, ctx, cDir)
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	general := g.Channels[0].ID
	side, err := a.CreateChannel(g.ID, "side", "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := c.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, c)
	waitUntil(t, 30*time.Second, func() bool { return c.channelInGuild(side.ID, g.ID) },
		"the member never saw the second channel")

	_ = c.Close()
	cat, err := a.CreateCategory(g.ID, "Stuff")
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if err := a.RenameChannel(g.ID, general, "lobby"); err != nil {
		t.Fatalf("RenameChannel: %v", err)
	}
	if err := a.SetChannelMeta(g.ID, general, "text", "", 0, "set while away"); err != nil {
		t.Fatalf("SetChannelMeta: %v", err)
	}
	if err := a.SetChannelMeta(g.ID, side.ID, "text", cat.ID, 0, "moved while away"); err != nil {
		t.Fatalf("SetChannelMeta: %v", err)
	}

	c2 := startServiceInDir(t, ctx, cDir)
	if err := c2.host.Connect(ctx, a.host.AddrInfo()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	c2.syncFromPeer(a.host.PeerID())
	waitUntil(t, 30*time.Second, func() bool {
		gen, sd := c2.channelByID(g.ID, general), c2.channelByID(g.ID, side.ID)
		return gen.Name == "lobby" && gen.Topic == "set while away" &&
			sd.Topic == "moved while away" && sd.Category == cat.ID
	}, "the member who was away kept the old channel name, topic or category")

	// And it is what they still have after a restart, not just in memory.
	_ = c2.Close()
	c3 := startServiceInDir(t, ctx, cDir)
	gen, sd := c3.channelByID(g.ID, general), c3.channelByID(g.ID, side.ID)
	if gen.Name != "lobby" || gen.Topic != "set while away" || sd.Category != cat.ID || sd.Topic != "moved while away" {
		t.Fatalf("after a restart: #%s %q, #%s %q in %q", gen.Name, gen.Topic, sd.Name, sd.Topic, sd.Category)
	}
}
