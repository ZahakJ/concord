package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
)

// TestMessageFromPhoneReachesTheDesktop is the reported symptom, reduced:
// "sending messages on phone don't show on desktop".
//
// Both devices are one account, both on the same rendezvous, both members of the
// same guild. This deliberately does NOT simulate carrier-grade NAT — the point
// is to separate the two candidate causes. If this passes, the linked-device
// message path itself is sound and the fault is reachability between the two
// machines; if it fails, the fault is in the app layer and no amount of network
// work would fix it.
func TestMessageFromPhoneReachesTheDesktop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	const body = "sent from the phone"
	if _, err := phone.SendMessage(textCh, body, ""); err != nil {
		t.Fatalf("phone SendMessage: %v", err)
	}

	waitUntil(t, 60*time.Second, func() bool {
		msgs, err := desk.Messages(textCh, 50)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if strings.Contains(m.Content, body) {
				return true
			}
		}
		return false
	}, "the desktop never received a message the phone sent")
}

// TestDesktopAndPhoneSeeEachOtherOnline is the other half of the report: the
// desktop appears in the phone's linked-device list but never says online.
// LinkedDevices is what that panel renders, so assert on it directly.
func TestDesktopAndPhoneSeeEachOtherOnline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	waitUntil(t, 60*time.Second, func() bool {
		for _, d := range phone.LinkedDevices() {
			if d.Online {
				return true
			}
		}
		return false
	}, "the phone never saw the desktop come online in its own device list")

	waitUntil(t, 60*time.Second, func() bool {
		for _, d := range desk.LinkedDevices() {
			if d.Online {
				return true
			}
		}
		return false
	}, "the desktop never saw the phone come online in its own device list")
}

// TestStrangersAreCountedNotListed pins the peer-list filter.
//
// A first attempt filtered on "has no fingerprint", which filtered NOTHING: a
// fingerprint is derived from the peer's public key (Service.presence falls back
// to identity.FingerprintOf), so every stranger on the DHT has one. The panel
// still showed hundreds of rows and the bug looked fixed from the code alone.
// The real test is whether the peer is somebody you know.
func TestStrangersAreCountedNotListed(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	svc := startServiceOn(t, ctx, t.TempDir(), boot)

	// A stranger: a plain libp2p host that shares no guild and was never verified.
	stranger, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.Security(noise.ID, noise.New),
	)
	if err != nil {
		t.Fatalf("stranger host: %v", err)
	}
	defer stranger.Close()

	if err := stranger.Connect(ctx, svc.host.AddrInfo()); err != nil {
		t.Fatalf("stranger connect: %v", err)
	}

	waitUntil(t, 20*time.Second, func() bool {
		ns := svc.NetworkStats()
		for _, p := range ns.PeerList {
			if p.ID == stranger.ID().String() {
				return false // listed as a person: the bug
			}
		}
		return ns.BackgroundPeers > 0
	}, "a stranger was listed as a peer instead of counted as background")
}

// TestGuildJoinedAfterLinkingReachesTheOtherDevice is the reported bug.
//
// Guild invites were handed to a device exactly once, during linking. A guild
// created or joined afterwards never reached it — so a phone linked on Monday
// was permanently stuck with Monday's servers. It looks like a sync failure from
// the outside ("my messages don't show up on my other device", "joining voice on
// the phone doesn't appear on the desktop") but the phone was never a member of
// that guild at all, so there was no channel for any of it to travel on.
func TestGuildJoinedAfterLinkingReachesTheOtherDevice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	// A guild that did not exist when the phone was linked.
	later, err := desk.CreateGuild("Made After Linking")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	waitUntil(t, 60*time.Second, func() bool {
		for _, g := range phone.Guilds() {
			if g.ID == later.ID {
				return true
			}
		}
		return false
	}, "a guild created after linking never reached the other device")
}

// TestGuildInvitesAreOfferedOnlyToOwnDevices is the security half of the fix
// above. An invite code admits its bearer to the guild, so the hello exchange
// must hand them to a peer that has proved, with an account-signed certificate,
// that it is a device of THIS account — and to nobody else, however friendly.
func TestGuildInvitesAreOfferedOnlyToOwnDevices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk := startServiceOn(t, ctx, t.TempDir(), boot)
	if _, err := desk.CreateGuild("Private"); err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	// A different account entirely, connected to us.
	other := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := other.host.Connect(ctx, desk.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if got := desk.guildInvitesFor(other.host.PeerID()); got != nil {
		t.Fatalf("offered %d invite(s) to a peer that is not our device", len(got))
	}
	// And the receiving side refuses codes that did not come from our own device,
	// so a forged frame cannot walk someone into a guild either.
	codes, _ := desk.linkGuildInvites()
	if len(codes) == 0 {
		t.Fatal("no invite codes to test with")
	}
	before := len(other.Guilds())
	other.redeemOfferedInvites(desk.host.PeerID(), codes)
	if len(other.Guilds()) != before {
		t.Fatal("a peer redeemed invites offered by an account that is not its own")
	}
}
