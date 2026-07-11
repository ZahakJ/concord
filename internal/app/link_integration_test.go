package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDeviceLinkingEndToEnd is the definitive device-linking proof: an issuer
// device with a guild links a new device, and the two then coexist under ONE
// account — different libp2p PeerIDs (no collision), the same account
// fingerprint, and the linked device joins the existing guild and receives the
// issuer's messages.
func TestDeviceLinkingEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	issuerDir := t.TempDir()
	joinerDir := t.TempDir()

	// Issuer: an account with a guild and a message.
	issuer := startServiceInDir(t, ctx, issuerDir)
	g, err := issuer.CreateGuild("Shared Guild")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	if _, err := issuer.SendMessage(channel, "before linking", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Issuer shows a link code; the new device redeems it.
	code, err := issuer.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	res, err := RedeemLink(ctx, joinerDir, code, "test-pass")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	invites := res.GuildInvites
	if len(invites) == 0 {
		t.Fatal("expected at least one guild invite from the issuer")
	}
	// The joiner is now a linked device on disk.
	if _, err := os.Stat(filepath.Join(joinerDir, "device.json")); err != nil {
		t.Fatalf("device marker not written: %v", err)
	}

	// "Log in" the linked device.
	joiner := startServiceInDir(t, ctx, joinerDir)

	// Same account, different device: the whole point of linking.
	if joiner.Fingerprint() != issuer.Fingerprint() {
		t.Fatalf("linked device should share the account fingerprint: %s vs %s",
			joiner.Fingerprint(), issuer.Fingerprint())
	}
	if joiner.PeerID() == issuer.PeerID() {
		t.Fatal("linked device must have a DIFFERENT libp2p PeerID (no collision)")
	}

	// The linked device joins the shared guild via the issuer's invite.
	for _, ic := range invites {
		if _, err := joiner.JoinViaInvite(ic); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}

	// Two leaves (issuer + linked device) now share the group…
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := issuer.MemberCount(g.ID)
		return n == 2
	}, "issuer never saw the linked device join the group")

	// Keep the two connected and let the gossipsub mesh for the channel warm up
	// before publishing (gossip is fire-and-forget; a message sent before the
	// mesh forms is dropped — the mailbox/sync cover that in real use).
	_ = joiner.host.Connect(ctx, issuer.host.AddrInfo())
	time.Sleep(3 * time.Second)

	// …and a fresh message from the issuer reaches the linked device.
	if _, err := issuer.SendMessage(channel, "hello linked device", ""); err != nil {
		t.Fatalf("SendMessage after link: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		joiner.syncFromPeer(issuer.host.PeerID()) // belt-and-suspenders vs gossip races
		msgs, err := joiner.Messages(channel, 0)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.Content == "hello linked device" {
				return true
			}
		}
		return false
	}, "linked device did not receive the issuer's message")
}
