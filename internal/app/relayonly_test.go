package app

// The topology every real deployment has and no test ever had.
//
// Every linked-device test in this package runs on loopback, where the two
// devices dial each other DIRECTLY — so the entire message path had only ever
// been verified over a kind of connection the user's phone and desktop, behind
// two different NATs, can never form. Their only path to each other is the
// rendezvous relay circuit. These tests build exactly that: each device lives
// on its own loopback alias, the other's alias is blocked (the simulated NAT),
// and the only shared reachable address is the relay's.
//
// What the field reported, and what this topology reproduced: both devices
// show ONLINE (presence is connection-level and the relayed connection exists)
// while messages never cross in either direction and voice presence never
// arrives (both ride gossipsub, which refuses to attach to a peer whose only
// connection is limited — see the root-cause note on RendezvousRelayResources
// in internal/net/relay.go, and the per-rung reproduction in
// internal/net/relayonly_test.go).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/multiformats/go-multiaddr"

	"github.com/ZahakJ/concord/internal/mailbox"
	cnet "github.com/ZahakJ/concord/internal/net"
)

// productionRendezvous is testRendezvous with the pieces that matter for a
// relay-only topology built the way cmd/rendezvous builds them: the circuit
// relay carrying cmd/rendezvous's exact resource configuration, and the
// mailbox. A test relay with library-default resources would pass or fail for
// reasons production doesn't have.
func productionRendezvous(t *testing.T, ctx context.Context) string {
	return rendezvousWithRelay(t, ctx, cnet.RendezvousRelayResources())
}

// rendezvousWithRelay is productionRendezvous with the relay's resource
// configuration chosen by the caller, so a test can stand up yesterday's
// deployed rendezvous as easily as today's.
func rendezvousWithRelay(t *testing.T, ctx context.Context, res relay.Resources) string {
	t.Helper()
	// One fabricated public address rides along with the loopback one: autorelay
	// only composes a client's /p2p-circuit address from a relay's PUBLIC addrs
	// (autorelay/addrsplosion.go cleanupAddressSet), so a loopback-only relay
	// grants reservations that no address ever points at. It is never dialled —
	// the hop stream always finds the existing bootstrap connection first.
	fakePublic := multiaddr.StringCast("/ip4/11.0.0.1/tcp/4001")
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0", "/ip4/127.0.0.1/udp/0/quic-v1"),
		libp2p.Security(noise.ID, noise.New),
		libp2p.AddrsFactory(func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
			return append(addrs, fakePublic)
		}),
	)
	if err != nil {
		t.Fatalf("rendezvous host: %v", err)
	}
	if _, err := relay.New(h, relay.WithResources(res)); err != nil {
		t.Fatalf("rendezvous relay: %v", err)
	}
	kdht, err := dht.New(h, dht.Mode(dht.ModeServer))
	if err != nil {
		t.Fatalf("rendezvous dht: %v", err)
	}
	if err := kdht.Bootstrap(ctx); err != nil {
		t.Fatalf("rendezvous bootstrap: %v", err)
	}
	mailbox.NewService(mailbox.New()).Attach(ctx, h)
	t.Cleanup(func() { _ = kdht.Close(); _ = h.Close() })
	for _, a := range h.Addrs() {
		if strings.HasPrefix(a.String(), "/ip4/127.0.0.1/tcp/") {
			return fmt.Sprintf("%s/p2p/%s", a, h.ID())
		}
	}
	t.Fatal("rendezvous has no loopback TCP addr")
	return ""
}

// startNATed starts a Service that listens only on its own loopback alias and
// cannot reach the other device's alias at all — the in-process equivalent of
// being behind a different NAT, with the relay as the only path across.
func startNATed(t *testing.T, ctx context.Context, dir, boot, ownIP string, unreachable ...string) *Service {
	t.Helper()
	svc, err := Start(ctx, Config{
		DataDir: dir, Passphrase: "test-pass", DisableMDNS: true,
		BootstrapPeers: []string{boot},
		listenAddrs: []string{
			fmt.Sprintf("/ip4/%s/tcp/0", ownIP),
			fmt.Sprintf("/ip4/%s/udp/0/quic-v1", ownIP),
		},
		blockedIPs: unreachable,
	})
	if err != nil {
		t.Fatalf("Start NAT'd service in %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// relayOnlyPair links a desktop and phone the way it happens in real life —
// side by side, direct connection — then moves each behind its own simulated
// NAT, from where the relay circuit is the only route between them. It returns
// them once both sides show the other ONLINE again, with the connection
// verified to actually be relayed.
func relayOnlyPair(t *testing.T, ctx context.Context, boot string) (desk, phone *Service, textCh, voiceCh string) {
	t.Helper()
	deskDir, phoneDir := t.TempDir(), t.TempDir()
	desk, phone, textCh, voiceCh = linkedPair(t, ctx, deskDir, phoneDir, boot)

	// The move: both devices go home to different networks. Their remembered
	// direct addresses are useless there, so clear them like a changed network
	// would.
	_ = desk.Close()
	_ = phone.Close()
	_ = os.Remove(filepath.Join(deskDir, "peers.json"))
	_ = os.Remove(filepath.Join(phoneDir, "peers.json"))

	desk = startNATed(t, ctx, deskDir, boot, "127.0.0.2", "127.0.0.3")
	phone = startNATed(t, ctx, phoneDir, boot, "127.0.0.3", "127.0.0.2")

	// Presence first: this is the state the user reported — both devices ONLINE
	// in the device list. It worked before the fix too (presence is
	// connection-level); it is asserted so a failure below cannot be blamed on
	// the devices simply not finding each other.
	waitUntil(t, 90*time.Second, func() bool {
		return deviceOnline(desk) && deviceOnline(phone)
	}, "the relay-only pair never saw each other online")

	// And the connection they share must genuinely be a relay circuit — if a
	// direct dial slipped through, this test would be verifying loopback again.
	conns := desk.host.Libp2p().Network().ConnsToPeer(phone.host.PeerID())
	if len(conns) == 0 {
		t.Fatalf("no connection between desk and phone despite both reporting online")
	}
	for _, c := range conns {
		if _, err := c.RemoteMultiaddr().ValueForProtocol(multiaddr.P_CIRCUIT); err != nil {
			t.Fatalf("desk holds a DIRECT connection to the phone (%s); the simulated NAT leaks", c.RemoteMultiaddr())
		}
	}
	return desk, phone, textCh, voiceCh
}

// dumpConns logs (and polices) the connections between the pair at a point
// where the test just proved something crossed: every one must still be a
// relay circuit, or the "NAT" leaked and the proof is about the wrong path.
func dumpConns(t *testing.T, label string, a, b *Service) {
	t.Helper()
	for _, c := range a.host.Libp2p().Network().ConnsToPeer(b.host.PeerID()) {
		t.Logf("%s: conn %s limited=%v", label, c.RemoteMultiaddr(), c.Stat().Limited)
		if _, err := c.RemoteMultiaddr().ValueForProtocol(multiaddr.P_CIRCUIT); err != nil {
			t.Fatalf("%s: DIRECT connection %s — the simulated NAT leaks, this test proves nothing", label, c.RemoteMultiaddr())
		}
	}
}

func deviceOnline(s *Service) bool {
	for _, d := range s.LinkedDevices() {
		if d.Online && !d.ThisOne {
			return true
		}
	}
	return false
}

// TestMessagesCrossWhenOnlyTheRelayConnects is the user's exact report: phone
// and desktop, same account, both showing ONLINE, relay reserved on both — "when
// I send a message I cannot see said message on desktop and vice versa".
func TestMessagesCrossWhenOnlyTheRelayConnects(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := productionRendezvous(t, ctx)
	desk, phone, textCh, _ := relayOnlyPair(t, ctx, boot)

	const fromPhone = "sent from the phone behind its NAT"
	if _, err := phone.SendMessage(textCh, fromPhone, "", ""); err != nil {
		t.Fatalf("phone SendMessage: %v", err)
	}
	waitUntil(t, 90*time.Second, func() bool {
		return hasMessage(desk, textCh, fromPhone)
	}, "the desktop never received a message the phone sent over the relay")
	dumpConns(t, "after phone->desk", desk, phone)

	const fromDesk = "sent from the desktop behind its NAT"
	if _, err := desk.SendMessage(textCh, fromDesk, "", ""); err != nil {
		t.Fatalf("desk SendMessage: %v", err)
	}
	waitUntil(t, 90*time.Second, func() bool {
		return hasMessage(phone, textCh, fromDesk)
	}, "the phone never received a message the desktop sent over the relay")
	dumpConns(t, "after desk->phone", phone, desk)
}

// TestMessagesSurviveALimitMeteredRelay stands up the rendezvous as it was
// DEPLOYED before this fix — its relay metering every circuit at 1h/512MB —
// because that server exists right now and clients will meet it until it is
// upgraded. Over a metered circuit the connection is "limited": gossipsub will
// not deliver across it, so live publish is off the table no matter what the
// client does. What must still work: the hello/sync streams (they now opt into
// limited connections) and above all the mailbox — the sender must notice that
// a limited-only peer is unreachable-by-publish and deposit for it, and the
// recipient's periodic sweep must pick it up. Budgeted generously since the
// sweep runs about once a minute.
func TestMessagesSurviveALimitMeteredRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res := cnet.RendezvousRelayResources()
	res.Limit = &relay.RelayLimit{Duration: time.Hour, Data: 512 << 20} // the deployed v0.43.6 config
	boot := rendezvousWithRelay(t, ctx, res)
	desk, phone, textCh, _ := relayOnlyPair(t, ctx, boot)

	const fromPhone = "metered relay: phone to desktop"
	if _, err := phone.SendMessage(textCh, fromPhone, "", ""); err != nil {
		t.Fatalf("phone SendMessage: %v", err)
	}
	waitUntil(t, 3*time.Minute, func() bool {
		return hasMessage(desk, textCh, fromPhone)
	}, "the desktop never received the phone's message through a metered relay (mailbox fallback)")
	dumpConns(t, "metered after phone->desk", desk, phone)

	const fromDesk = "metered relay: desktop to phone"
	if _, err := desk.SendMessage(textCh, fromDesk, "", ""); err != nil {
		t.Fatalf("desk SendMessage: %v", err)
	}
	waitUntil(t, 3*time.Minute, func() bool {
		return hasMessage(phone, textCh, fromDesk)
	}, "the phone never received the desktop's message through a metered relay (mailbox fallback)")
	dumpConns(t, "metered after desk->phone", phone, desk)
}

func hasMessage(s *Service, ch, body string) bool {
	msgs, err := s.Messages(ch, 50)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, body) {
			return true
		}
	}
	return false
}

// TestVoicePresenceCrossesTheRelay is the second half of the report: "if I join
// vc on phone it doesn't show that I joined on the desktop".
func TestVoicePresenceCrossesTheRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := productionRendezvous(t, ctx)
	desk, phone, _, voiceCh := relayOnlyPair(t, ctx, boot)

	joined := make(chan struct{}, 8)
	desk.OnVoicePresence(func(from, fingerprint, channelID, action, target, dest string) {
		if channelID == voiceCh && action == "join" {
			select {
			case joined <- struct{}{}:
			default:
			}
		}
	})
	if err := phone.JoinVoice(voiceCh); err != nil {
		t.Fatalf("phone JoinVoice: %v", err)
	}
	select {
	case <-joined:
	case <-time.After(90 * time.Second):
		t.Fatal("the desktop never saw the phone join voice over the relay")
	}
}
