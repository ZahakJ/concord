package app

import (
	"context"
	"testing"
	"time"
)

// startServiceInDir boots a Service in a caller-owned dir so a test can stop it
// and later restart "the same peer" (same identity, MLS state, and history).
func startServiceInDir(t *testing.T, ctx context.Context, dir string) *Service {
	t.Helper()
	svc, err := Start(ctx, Config{
		DataDir:     dir,
		Passphrase:  "test-pass",
		DisableMDNS: true,
	})
	if err != nil {
		t.Fatalf("Start service in %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// TestJoinerProfileKnownToHost pins the fix for "the host shows the joiner's
// fingerprint instead of their name": the display name must arrive over the
// reliable invite stream (not gossip, which races mesh warm-up) and must
// survive a host restart (persisted, not in-memory only).
func TestJoinerProfileKnownToHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	aDir := t.TempDir()
	a := startServiceInDir(t, ctx, aDir)
	b := startService(t, ctx)

	if err := b.SetProfile(Profile{Name: "euclid", Emoji: "🌀"}); err != nil {
		t.Fatalf("B SetProfile: %v", err)
	}

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}

	// The handshake completed, so both sides know each other's name NOW —
	// no gossip warm-up sleep allowed here.
	if got := a.ProfileName(b.Fingerprint()); got != "euclid" {
		t.Fatalf("host sees joiner as %q immediately after join, want \"euclid\"", got)
	}
	if got := b.ProfileName(a.Fingerprint()); got != a.DisplayName() {
		t.Fatalf("joiner sees host as %q, want %q", got, a.DisplayName())
	}

	// Restart the host from the same dir: the learned profile must be restored
	// from the store, not lost with the process.
	_ = a.Close()
	a2 := startServiceInDir(t, ctx, aDir)
	if got := a2.ProfileName(b.Fingerprint()); got != "euclid" {
		t.Fatalf("host restart forgot joiner profile: got %q, want \"euclid\"", got)
	}
}

// TestOfflineCatchUp is the Phase A acceptance test: a member that was offline
// while the guild moved on — new messages, a new MEMBER (an MLS epoch bump,
// previously fatal), edits/deletes/pins/reactions, a new channel, and a guild
// rename — recovers everything after restarting and syncing from one peer.
func TestOfflineCatchUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	cDir := t.TempDir()
	c := startServiceInDir(t, ctx, cDir)

	ra, rb := &recorder{}, &recorder{}
	a.OnMessage(ra.add)
	b.OnMessage(rb.add)

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)
	if _, err := c.JoinViaInvite(code); err != nil {
		t.Fatalf("C JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, a, b, c)

	// Warm traffic C also sees; B's message is the later edit target.
	rc := &recorder{}
	c.OnMessage(rc.add)
	sendUntilReceived(t, a, channel, "before-offline", rb, rc)
	bMsg, err := b.SendMessage(channel, "b-original", "")
	if err != nil {
		t.Fatalf("B SendMessage: %v", err)
	}
	waitUntil(t, 15*time.Second, func() bool { return ra.has("b-original") }, "A did not receive b-original")

	// C goes offline.
	_ = c.Close()

	// While C is away: messages, an epoch bump (D joins), and state changes.
	sendUntilReceived(t, a, channel, "while-away", rb)

	d := startService(t, ctx)
	if _, err := d.JoinViaInvite(code); err != nil {
		t.Fatalf("D JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 4, a, b, d)
	sendUntilReceived(t, a, channel, "after-epoch-bump", rb)

	if err := b.EditMessage(channel, bMsg.ID, "b-edited"); err != nil {
		t.Fatalf("B EditMessage: %v", err)
	}
	if err := b.ToggleReaction(channel, bMsg.ID, "👍"); err != nil {
		t.Fatalf("B ToggleReaction: %v", err)
	}
	waitUntil(t, 15*time.Second, func() bool {
		m, ok, _ := a.store.MessageByID(bMsg.ID)
		return ok && m.Content == "b-edited" && len(m.Reactions["👍"]) == 1
	}, "A did not apply B's edit+reaction")
	if _, err := a.CreateChannel(g.ID, "offtopic", "", ""); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if err := a.RenameGuild(g.ID, "renamed"); err != nil {
		t.Fatalf("RenameGuild: %v", err)
	}

	// C comes back, reconnects to A, and must recover everything.
	c2 := startServiceInDir(t, ctx, cDir)
	if err := c2.host.Connect(ctx, a.host.AddrInfo()); err != nil {
		t.Fatalf("C2 reconnect to A: %v", err)
	}
	// The peer-connect trigger also fires; calling directly keeps the test fast
	// and deterministic (both paths are idempotent).
	c2.syncFromPeer(a.host.PeerID())

	waitUntil(t, 30*time.Second, func() bool {
		n, _ := c2.MemberCount(g.ID)
		return n == 4
	}, "C2 did not catch up on the missed epoch (member count)")
	waitUntil(t, 30*time.Second, func() bool {
		msgs, err := c2.Messages(channel, 0)
		if err != nil {
			return false
		}
		var sawAway, sawBump, sawEdit bool
		for _, m := range msgs {
			sawAway = sawAway || m.Content == "while-away"
			sawBump = sawBump || m.Content == "after-epoch-bump"
			if m.ID == bMsg.ID {
				sawEdit = m.Content == "b-edited" && len(m.Reactions["👍"]) == 1
			}
		}
		return sawAway && sawBump && sawEdit
	}, "C2 did not recover missed messages and state")
	waitUntil(t, 20*time.Second, func() bool {
		for _, gg := range c2.Guilds() {
			if gg.ID == g.ID {
				return gg.Name == "renamed" && len(gg.Channels) == 2
			}
		}
		return false
	}, "C2 did not adopt the new channel and guild name")
	if c2.OutOfSync(g.ID) {
		t.Fatal("guild wrongly flagged out-of-sync after successful catch-up")
	}

	// And C2 can decrypt LIVE traffic at the new epoch — the previously-fatal
	// case (a missed membership commit used to strand the member forever).
	rc2 := &recorder{}
	c2.OnMessage(rc2.add)
	sendUntilReceived(t, a, channel, "live-after-return", rc2)
}
