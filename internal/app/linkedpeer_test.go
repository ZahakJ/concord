package app

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// A linked device must never read as a stranger — not to the account it belongs
// to, and not to the friends that account shares a group with. Three ways it
// did:
//
//   - the peer list looked its own account's name up in the profile cache,
//     which deliberately holds no row for ourselves, so your own phone rendered
//     as "unknown peer";
//   - a device that joined a group AFTER we started was judged once, at connect
//     time, and never again, so it stayed a stranger until the next launch;
//   - a device that joined no group at all left nothing anywhere for us to
//     learn from, so it was a stranger forever.

// deviceStat returns the OWN-DEVICE row for a given PeerID. Your own devices
// live in their own list now, not among the peers the rendezvous introduced you
// to — see LinkedDeviceView.
func deviceStat(t *testing.T, s *Service, id string) LinkedDeviceView {
	t.Helper()
	for _, d := range s.LinkedDevices() {
		if d.PeerID == id {
			return d
		}
	}
	t.Fatalf("no device row for %s", id)
	return LinkedDeviceView{}
}

// waitDevice blocks until s lists id as one of its own devices, online.
func waitDevice(t *testing.T, s *Service, id string) {
	t.Helper()
	waitUntil(t, 15*time.Second, func() bool {
		for _, d := range s.LinkedDevices() {
			if d.PeerID == id && d.Online {
				return true
			}
		}
		return false
	}, "device "+id+" never showed up as an online device of this account")
}

// waitPeer blocks until s holds a live connection to id in the peer list. Used
// for OTHER people's devices, which do belong in that list.
func waitPeer(t *testing.T, s *Service, id string) {
	t.Helper()
	waitUntil(t, 15*time.Second, func() bool {
		for _, p := range s.NetworkStats().PeerList {
			if p.ID == id {
				return true
			}
		}
		return false
	}, "peer "+id+" never showed up in the peer list")
}

// peerIDOf is another Service's libp2p peer id, typed.
func (s *Service) peerIDOf(t *testing.T, other *Service) peer.ID {
	t.Helper()
	return other.host.PeerID()
}

// noStrangerRow asserts that a peer is NOT in the generic peer list — a device
// of yours filed among strangers is what used to render as "unknown peer".
func noStrangerRow(t *testing.T, s *Service, id string) {
	t.Helper()
	for _, p := range s.NetworkStats().PeerList {
		if p.ID == id {
			t.Errorf("own device %s is in the stranger peer list (name=%q)", id, p.Name)
		}
	}
}

// TestOwnLinkedDeviceNamedInPeerList: the desktop's diagnostics must show a
// paired phone as this account, by name. The roster has always done this for
// yourself (Bridge.Members' isSelf branch); the peer list resolved through the
// profile cache instead, which has no self row by design, and so labelled your
// own phone "unknown peer".
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

	// The case the user actually hits: the desktop has been restarted since the
	// phone was linked, so everything it knows about that phone comes off disk.
	_ = issuer.Close()
	issuer = startServiceInDir(t, ctx, issuerDir)
	if err := joiner.host.Connect(ctx, issuer.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	waitDevice(t, issuer, joiner.PeerID())
	noStrangerRow(t, issuer, joiner.PeerID())

	if fpr := issuer.presence(issuer.peerIDOf(t, joiner)).Fingerprint; fpr != issuer.Fingerprint() {
		t.Fatalf("linked device resolved to %q, want this account %q", fpr, issuer.Fingerprint())
	}
	dv := deviceStat(t, issuer, joiner.PeerID())
	if !dv.Online {
		t.Error("linked device not shown as online")
	}
	if dv.LastSeen == 0 {
		t.Error("linked device has no last-seen stamp")
	}
	if dv.Transport == "" {
		t.Error("linked device row does not say how it is reached")
	}
}

// TestOwnLinkedDeviceResolvesWithoutSharedGroup: a device that linked but never
// joined a group (every guild handover failed, or its leaf was later removed)
// has no cert in any roster we can read. Relearning cannot help; only the certs
// we issued ourselves can. Without them this device was a permanent stranger.
func TestOwnLinkedDeviceResolvesWithoutSharedGroup(t *testing.T) {
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
	code, err := issuer.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	if _, err := RedeemLink(ctx, joinerDir, code, "test-pass"); err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	joiner := startServiceInDir(t, ctx, joinerDir)

	// Restart the issuer: anything the link handshake taught it in memory is
	// gone, and there is no group roster to recover the mapping from.
	_ = issuer.Close()
	issuer = startServiceInDir(t, ctx, issuerDir)
	if err := joiner.host.Connect(ctx, issuer.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	waitDevice(t, issuer, joiner.PeerID())
	noStrangerRow(t, issuer, joiner.PeerID())
	if fpr := issuer.presence(issuer.peerIDOf(t, joiner)).Fingerprint; fpr != issuer.Fingerprint() {
		t.Fatalf("groupless linked device resolved to %q, want this account %q",
			fpr, issuer.Fingerprint())
	}
}

// TestLinkedDeviceMidSessionResolvesToAccount: a friend pairs a phone while we
// are already running. The add commit reaches us over the control topic and
// puts the phone's cert in our roster — but only startup read that roster, so
// the phone stayed a stranger for the rest of the session.
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
	res, err := RedeemLink(ctx, phoneDir, linkCode, "test-pass")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := startServiceInDir(t, ctx, phoneDir)
	for _, ic := range res.GuildInvites {
		if _, err := phone.JoinViaInvite(ic); err != nil {
			t.Fatalf("phone JoinViaInvite: %v", err)
		}
	}
	waitUntil(t, 40*time.Second, func() bool {
		n, _ := friend.MemberCount(g.ID)
		return n == 3
	}, "we never saw the phone's leaf arrive")

	if err := phone.host.Connect(ctx, friend.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	waitPeer(t, friend, phone.PeerID())

	// No restart in between: the commit itself has to teach us the mapping.
	waitUntil(t, 15*time.Second, func() bool {
		for _, p := range friend.NetworkStats().PeerList {
			if p.ID == phone.PeerID() {
				return p.Fingerprint == owner.Fingerprint()
			}
		}
		return false
	}, "the friend's phone never resolved to their account without a restart")
}
