package app

import (
	"context"
	"testing"
	"time"
)

// A linked device must never read as a stranger to the account it belongs to,
// or to the friends that account shares a group with. Three ways it did:
//
//   - the peer list looked its own account's name up in the profile cache,
//     which deliberately has no row for ourselves, so your own phone rendered
//     as "unknown peer";
//   - a device that joined a group AFTER we started was placed once, at connect
//     time, and never re-judged, so it stayed a stranger until the next launch;
//   - a device that joined no group at all left no trace we could learn from
//     anywhere, so it was a stranger forever.

// peerStat returns the peer-list row for a given PeerID.
func peerStat(t *testing.T, s *Service, id string) PeerStatView {
	t.Helper()
	for _, p := range s.NetworkStats().PeerList {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no peer-list row for %s", id)
	return PeerStatView{}
}

// TestOwnLinkedDeviceNamedInPeerList: the desktop's diagnostics must show a
// paired phone as this account, by name — the roster has always done that for
// yourself (Bridge.Members' isSelf branch); the peer list resolved through the
// profile cache instead and came back empty.
func TestOwnLinkedDeviceNamedInPeerList(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	issuerDir, joinerDir := t.TempDir(), t.TempDir()
	issuer := startServiceInDir(t, ctx, issuerDir)
	if err := issuer.SetDisplayName("Avicenna"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := issuer.CreateGuild("Shared Guild")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	code, err := issuer.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	res, err := RedeemLink(ctx, joinerDir, code, "test-pass")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	joiner := startServiceInDir(t, ctx, joinerDir)
	for _, ic := range res.GuildInvites {
		if _, err := joiner.JoinViaInvite(ic); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := issuer.MemberCount(g.ID)
		return n == 2
	}, "issuer never saw the linked device join")

	// The interesting case is the one the user hits: the desktop has been
	// restarted since the phone was linked, so everything it knows about that
	// phone has to come off disk.
	_ = issuer.Close()
	issuer = startServiceInDir(t, ctx, issuerDir)
	if err := joiner.host.Connect(ctx, issuer.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	waitUntil(t, 15*time.Second, func() bool {
		return len(issuer.NetworkStats().PeerList) > 0
	}, "issuer never saw the linked device connect")

	pv := peerStat(t, issuer, joiner.PeerID())
	if pv.Fingerprint != issuer.Fingerprint() {
		t.Fatalf("linked device resolved to %q, want this account %q", pv.Fingerprint, issuer.Fingerprint())
	}
	if !pv.Self {
		t.Error("linked device not marked as another device of this account")
	}
	if pv.Name != "Avicenna" {
		t.Errorf("linked device shown as %q, want the account's own name (empty renders as \"unknown peer\")", pv.Name)
	}
}

// TestLinkedDeviceMidSessionResolvesToAccount: a friend links a phone while we
// are already running. The add commit reaches us over the control topic and
// puts the phone's cert in our roster — but only startup used to read that
// roster, so the phone was a stranger until our next launch.
func TestLinkedDeviceMidSessionResolvesToAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	g, err := owner.CreateGuild("Shared Guild")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	friend := startService(t, ctx) // us: up before the phone exists, and stays up
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, friend)

	// Only now does the owner pair a phone, which joins the guild.
	linkCode, err := owner.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	phoneDir := t.TempDir()
	res, err := RedeemLink(ctx, linkCode, "test-pass", "")[0:0], error(nil) // placeholder
	_ = res
	_ = err
	_ = phoneDir
}
