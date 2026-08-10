package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
)

// "The phone and the desktop can talk, but only after about a minute" — the
// three separate reasons, each pinned here with the number that made it obvious.

// testRendezvous is a minimal stand-in for cmd/rendezvous: a DHT server and a
// relay, which is all the discovery path needs.
func testRendezvous(t *testing.T, ctx context.Context) string {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0", "/ip4/127.0.0.1/udp/0/quic-v1"),
		libp2p.Security(noise.ID, noise.New),
	)
	if err != nil {
		t.Fatalf("rendezvous host: %v", err)
	}
	if _, err := relay.New(h); err != nil {
		t.Fatalf("rendezvous relay: %v", err)
	}
	kdht, err := dht.New(h, dht.Mode(dht.ModeServer))
	if err != nil {
		t.Fatalf("rendezvous dht: %v", err)
	}
	if err := kdht.Bootstrap(ctx); err != nil {
		t.Fatalf("rendezvous bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = kdht.Close(); _ = h.Close() })
	return fmt.Sprintf("%s/p2p/%s", h.Addrs()[0], h.ID())
}

func startServiceOn(t *testing.T, ctx context.Context, dir, boot string) *Service {
	t.Helper()
	svc, err := Start(ctx, Config{
		DataDir: dir, Passphrase: "test-pass", DisableMDNS: true,
		BootstrapPeers: []string{boot},
	})
	if err != nil {
		t.Fatalf("Start service in %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// linkedPair returns a desktop and the phone linked into its account, both on
// the given rendezvous, both members of one guild. The guild and its voice
// channel come back too.
func linkedPair(t *testing.T, ctx context.Context, deskDir, phoneDir, boot string) (desk, phone *Service, textCh, voiceCh string) {
	t.Helper()
	desk = startServiceOn(t, ctx, deskDir, boot)
	if err := desk.SetDisplayName("Avicenna"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := desk.CreateGuild("Shared")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	vc, err := desk.CreateChannel(g.ID, "Voice", "voice", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	res, err := RedeemLink(ctx, phoneDir, code, "test-pass")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone = startServiceOn(t, ctx, phoneDir, boot)
	for _, ic := range res.GuildInvites {
		if _, err := phone.JoinViaInvite(ic); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := desk.MemberCount(g.ID)
		return n == 2
	}, "the phone never joined the shared guild")
	return desk, phone, g.Channels[0].ID, vc.ID
}

// TestReturningDeviceIsFoundQuickly is the headline number.
//
// A phone that wakes up on a different network has to be found again, and the
// only mechanism used to be the rendezvous provider record — published by
// discovery/util.Advertise, whose first attempt ALWAYS fails (the routing table
// is empty at that moment) and which then sleeps a flat two minutes before
// trying again. Measured end to end, desktop-sees-returning-phone: 120.06s.
// With the advertise loop waiting for a routing table and retrying on a short
// backoff, and with the account dialling its own devices by peer id instead of
// searching for them: 4.06s.
//
// The bound here is deliberately loose — this is a real network test on a real
// DHT — but 30s is far below the two-minute floor the old code could not beat.
func TestReturningDeviceIsFoundQuickly(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	deskDir, phoneDir := t.TempDir(), t.TempDir()
	desk, phone, _, _ := linkedPair(t, ctx, deskDir, phoneDir, boot)

	phoneID := phone.PeerID()
	_ = phone.Close()
	// A phone that changed networks: neither side's cached address is any use.
	_ = os.Remove(filepath.Join(phoneDir, "peers.json"))
	_ = os.Remove(filepath.Join(deskDir, "peers.json"))
	desk.peers = LoadPeerCache(deskDir)
	waitUntil(t, 30*time.Second, func() bool {
		for _, p := range desk.host.Peers() {
			if p.String() == phoneID {
				return false
			}
		}
		return true
	}, "the desktop never noticed the phone go away")

	t0 := time.Now()
	startServiceOn(t, ctx, phoneDir, boot)
	waitUntil(t, 30*time.Second, func() bool {
		for _, p := range desk.host.Peers() {
			if p.String() == phoneID {
				return true
			}
		}
		return false
	}, "the desktop never found the returning phone")
	t.Logf("desktop found the returning phone in %.2fs", time.Since(t0).Seconds())
}

// TestUnplaceableDeviceStillGetsRecognised covers the device the roster cannot
// help with: linked by a build that kept no certificate, or whose leaf is in no
// group this install can read.
//
// Before the hello exchange this was not slow, it was permanent. Measured over
// 75s with the roster mapping unavailable: presence() never resolved, the UI
// presence feed never fired, the message the phone had sent never arrived (the
// connect tail declines to sync from a peer it cannot place, and gives up for
// good after one recheck), and the diagnostics panel showed a stranger. After:
// all three inside 0.10s of the connection.
func TestUnplaceableDeviceStillGetsRecognised(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deskDir, phoneDir := t.TempDir(), t.TempDir()
	desk := startServiceInDir(t, ctx, deskDir)
	if err := desk.SetDisplayName("Avicenna"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := desk.CreateGuild("Shared")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	res, err := RedeemLink(ctx, phoneDir, code, "test-pass")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := startServiceInDir(t, ctx, phoneDir)
	for _, ic := range res.GuildInvites {
		if _, err := phone.JoinViaInvite(ic); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := desk.MemberCount(g.ID)
		return n == 2
	}, "the phone never joined")

	_ = desk.Close()
	_ = phone.Close()
	desk = startServiceInDir(t, ctx, deskDir)
	// The device this desktop has no record of: forget both the certificate we
	// issued and everything the roster taught us at startup.
	_ = desk.store.SetSetting(deviceRegistryKey, "")
	_ = desk.store.SetSetting(ownDevicesKey, "")
	desk.deviceMu.Lock()
	desk.deviceAccounts = map[string]string{}
	desk.deviceMu.Unlock()
	phone = startServiceInDir(t, ctx, phoneDir)

	var mu sync.Mutex
	sawPresence := false
	desk.OnPeerConnected(func(pp PeerPresence) {
		if pp.PeerID == phone.PeerID() {
			mu.Lock()
			sawPresence = true
			mu.Unlock()
		}
	})

	// The phone speaks while the two are apart, then they meet.
	if _, err := phone.SendMessage(channel, "from my phone", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if err := phone.host.Connect(ctx, desk.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	waitUntil(t, 20*time.Second, func() bool {
		return desk.presence(phone.host.PeerID()).Fingerprint == desk.Fingerprint()
	}, "the desktop never recognised its own phone")
	waitUntil(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sawPresence
	}, "the presence feed never mentioned the phone")
	waitUntil(t, 20*time.Second, func() bool {
		msgs, err := desk.Messages(channel, 0)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.Content == "from my phone" {
				return true
			}
		}
		return false
	}, "the desktop never caught up on what the phone had said")

	// …and the recognition is now on disk, so it survives with no roster and no
	// network next time.
	if len(desk.ownDeviceCerts()) == 0 {
		t.Error("the desktop learned its phone's certificate but did not keep it")
	}
}

// TestVoicePresenceNamesTheAccount: a linked device in a call must be reported
// under its ACCOUNT fingerprint. watchVoice used presenceFor, which reads the
// key out of the PeerID — for a linked device, a key that belongs to no member —
// so your own phone joined the call as an unnameable stranger and the front
// end's "(other device)" label could never fire.
func TestVoicePresenceNamesTheAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, _, voiceCh := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	var mu sync.Mutex
	got := ""
	desk.OnVoicePresence(func(from, fpr, ch, action, target, dest string) {
		if from == phone.PeerID() && ch == voiceCh {
			mu.Lock()
			got = fpr
			mu.Unlock()
		}
	})
	if err := phone.host.Connect(ctx, desk.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := phone.JoinVoice(voiceCh); err != nil {
		t.Fatalf("JoinVoice: %v", err)
	}
	waitUntil(t, 25*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return got != ""
	}, "the desktop never saw the phone in the call")
	mu.Lock()
	defer mu.Unlock()
	if got != desk.Fingerprint() {
		t.Fatalf("voice presence reported %q; want this account %q — a linked device must read as its owner", got, desk.Fingerprint())
	}
}

// TestOwnDeviceIsNotAStranger: the diagnostics split. A device of yours belongs
// in the device list, with a transport and a last-seen — and nowhere near the
// list of peers the rendezvous introduced you to, where it rendered as "unknown
// peer".
func TestOwnDeviceIsNotAStranger(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	if err := phone.host.Connect(ctx, desk.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	waitDevice(t, desk, phone.PeerID())

	ns := desk.NetworkStats()
	for _, p := range ns.PeerList {
		if p.ID == phone.PeerID() {
			t.Fatalf("the phone is in the stranger peer list as %q", p.Name)
		}
	}
	if len(ns.DeviceList) < 2 {
		t.Fatalf("device list has %d rows; want this device and the phone", len(ns.DeviceList))
	}
	// This device is always present, and always first.
	if !ns.DeviceList[0].ThisOne {
		t.Error("the device you are holding is not the first row")
	}
	// …and the phone's row answers the questions the panel exists for.
	dv := deviceStat(t, desk, phone.PeerID())
	if !dv.Online || dv.Transport == "" || dv.LastSeen == 0 {
		t.Errorf("phone row is missing diagnostics: %+v", dv)
	}
}
