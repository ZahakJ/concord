package app

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/p2p/security/noise"

	"github.com/ZahakJ/concord/internal/mailbox"
)

// startMailboxNode brings up a bare libp2p host serving the mailbox protocol,
// standing in for the rendezvous node. Returns the host and its dialable addr.
func startMailboxNode(t *testing.T, ctx context.Context) (host.Host, string) {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.Security(noise.ID, noise.New),
	)
	if err != nil {
		t.Fatalf("start mailbox node: %v", err)
	}
	mailbox.NewService(mailbox.New()).Attach(ctx, h)
	t.Cleanup(func() { _ = h.Close() })
	return h, h.Addrs()[0].String() + "/p2p/" + h.ID().String()
}

func startServiceWithBootstrap(t *testing.T, ctx context.Context, dir string, bootstrap []string) *Service {
	t.Helper()
	svc, err := Start(ctx, Config{
		DataDir:        dir,
		Passphrase:     "test-pass",
		DisableMDNS:    true,
		BootstrapPeers: bootstrap,
	})
	if err != nil {
		t.Fatalf("Start service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// TestMailboxOfflineDelivery is the acceptance test: A and B share a guild;
// B goes offline; A sends a message that lands in B's mailbox on the node; B
// comes back, drains it, and sees the message — with no other peer online.
func TestMailboxOfflineDelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, nodeAddr := startMailboxNode(t, ctx)

	a := startServiceWithBootstrap(t, ctx, t.TempDir(), []string{nodeAddr})
	bDir := t.TempDir()
	b := startServiceWithBootstrap(t, ctx, bDir, []string{nodeAddr})

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	// Wait until A has learned B's mailbox key (needed to seal for B) and B has
	// registered its mailbox with the node (needed for the node to accept it).
	waitUntil(t, 20*time.Second, func() bool {
		_, ok := a.mailboxPubFor(b.Fingerprint())
		return ok && len(b.mailboxNodes()) > 0
	}, "A never learned B's mailbox key / B never reached the node")

	// B goes offline (kept data dir so it can return as the same member).
	_ = b.Close()
	// Let A notice B dropped so it treats B as offline.
	waitUntil(t, 15*time.Second, func() bool {
		for _, p := range a.host.Peers() {
			if presenceFor(p).Fingerprint == b.Fingerprint() {
				return false
			}
		}
		return true
	}, "A still sees B as connected")

	// A sends while B is away — this should deposit into B's mailbox.
	if _, err := a.SendMessage(channel, "message while B was offline", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	time.Sleep(2 * time.Second) // let the async deposit complete

	// B returns; it connects to the node, drains the mailbox, and receives it.
	b2 := startServiceWithBootstrap(t, ctx, bDir, []string{nodeAddr})
	rb := &recorder{}
	b2.OnMessage(rb.add)

	waitUntil(t, 30*time.Second, func() bool {
		msgs, err := b2.Messages(channel, 0)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.Content == "message while B was offline" {
				return true
			}
		}
		return false
	}, "B never received the offline message from the mailbox")
}
