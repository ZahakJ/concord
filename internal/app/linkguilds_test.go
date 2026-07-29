package app

import (
	"context"
	"testing"
	"time"
)

// TestLinkHandsOverGuildsWeOnlyBelongTo is the regression: linking a second
// device handed over only the servers the ISSUER administers. The guild list was
// built from InviteCode, which refuses for a guild we are merely a member of —
// and the error was dropped on the floor, so the new device came up looking
// linked while silently missing every server the user had joined rather than
// created. Nothing retried it and nothing said a word.
func TestLinkHandsOverGuildsWeOnlyBelongTo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)  // runs the server
	member := startService(t, ctx) // merely joined it

	g, err := owner.CreateGuild("Someone Else's Server")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	// The member genuinely cannot mint a code of its own — that gate is correct
	// and stays. The link path has to route around it, not through it.
	if _, err := member.InviteCode(g.ID); err == nil {
		t.Fatal("a plain member minting an invite code would fork the epoch; that gate must stay")
	}

	codes, missing := member.linkGuildInvites()
	if len(missing) != 0 {
		t.Fatalf("guilds the new device would never see: %v", missing)
	}
	var handed *inviteCode
	for _, c := range codes {
		ic, err := decodeInviteCode(c)
		if err != nil {
			t.Fatalf("decode handed-over code: %v", err)
		}
		if ic.GuildID == g.ID {
			handed = &ic
			break
		}
	}
	if handed == nil {
		t.Fatalf("a device linked here would never join %q: %d codes for %d guilds",
			g.Name, len(codes), len(member.Guilds()))
	}
	// It must point at the REAL owner. A code naming the issuer is worse than
	// none: redeeming it advances the joiner onto a private epoch fork that every
	// honest peer drops.
	if handed.OwnerID != owner.PeerID() {
		t.Fatalf("invite points at %s, want the real owner %s", handed.OwnerID, owner.PeerID())
	}
	if len(handed.OwnerAddr) == 0 {
		t.Fatal("invite carries no address for the owner; nothing could redeem it")
	}
}

// TestLinkedDeviceJoinsAServerTheIssuerDoesNotOwn drives the whole path end to
// end: the code minted above is redeemed by a real second device against the
// real owner, and the owner's roster grows to three leaves.
func TestLinkedDeviceJoinsAServerTheIssuerDoesNotOwn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	member := startService(t, ctx)

	g, err := owner.CreateGuild("Someone Else's Server")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}

	// The member links a second device of its own account.
	offer, err := member.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	deviceDir := t.TempDir()
	res, err := RedeemLink(ctx, deviceDir, offer, "test-pass")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	if len(res.MissingGuilds) != 0 {
		t.Fatalf("issuer could not hand over: %v", res.MissingGuilds)
	}
	device := startServiceInDir(t, ctx, deviceDir)
	joined := false
	for _, ic := range res.GuildInvites {
		joinedGuild, err := device.JoinViaInvite(ic)
		if err != nil {
			t.Fatalf("linked device could not join with a handed-over code: %v", err)
		}
		if joinedGuild.ID == g.ID {
			joined = true
		}
	}
	if !joined {
		t.Fatal("the linked device never joined the server its account belongs to")
	}
	// Three leaves under two accounts: the owner, the member, the member's device.
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 3
	}, "the owner never saw the linked device join")
}
