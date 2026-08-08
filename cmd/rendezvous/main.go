// Command rendezvous is Concord's minimal, untrusted infrastructure node. It
// runs a libp2p host that:
//
//   - serves the Kademlia DHT (server mode), so peers can advertise and find
//     each other under the Concord rendezvous key beyond their local network;
//   - offers a Circuit Relay v2 service, so peers stuck behind hard NATs can be
//     reached until a direct hole-punched connection is established.
//
// It never sees message plaintext or media — everything it carries is already
// end-to-end encrypted. It is designed to run on a tiny always-on host (e.g.
// fly.io). Peers use its printed multiaddrs as bootstrap peers.
//
// Identity: a bootstrap node needs a STABLE PeerID (peers embed it in the
// bootstrap multiaddr). Provide a 32-byte hex seed via CONCORD_RELAY_SEED to fix
// it; otherwise a random identity is generated per run (fine for local testing).
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/mailbox"
	cnet "github.com/ZahakJ/concord/internal/net"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rendezvous:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	port := os.Getenv("PORT")
	if port == "" {
		port = "4001"
	}
	listen := []string{
		"/ip4/0.0.0.0/tcp/" + port,
		"/ip4/0.0.0.0/udp/" + port + "/quic-v1",
	}

	id, err := relayIdentity()
	if err != nil {
		return err
	}
	priv, err := p2pcrypto.UnmarshalEd25519PrivateKey(id.PrivateKey())
	if err != nil {
		return err
	}

	// Relay service for NAT'd peers. The resource numbers live in internal/net
	// (RendezvousRelayResources) so the tests that stand in for this node run
	// the same relay this node does.
	relayRes := cnet.RendezvousRelayResources()

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(listen...),
		libp2p.Security(noise.ID, noise.New),
		libp2p.EnableRelayService(relay.WithResources(relayRes)),
		libp2p.EnableNATService(),
		// WITHOUT THIS THE RELAY NEVER STARTS, and nothing else in Concord works
		// for anyone behind a NAT.
		//
		// EnableRelayService does not start the relay; it hands it to a manager
		// that waits for AutoNAT to announce EvtLocalReachabilityChanged(Public)
		// — see go-libp2p p2p/host/relaysvc/relay.go. AutoNAT reaches that verdict
		// only when other peers dial us back unsolicited, and on a fly.io machine
		// that verdict may never arrive. Until it does, the node does not speak
		// circuit v2 at all: AutoRelay on every client rejects it with "doesn't
		// speak circuit v2", no client ever gets a reservation, and every NAT'd
		// device ends up advertising nothing but LAN addresses. Two people behind
		// two different routers then cannot reach each other, which presents as
		// "my phone shows no peers" and "my other device never comes online".
		//
		// A rendezvous is a public server by definition — it is deployed at a
		// known hostname precisely so it can be dialled. It has no business
		// inferring its own reachability, so it is told.
		libp2p.ForceReachabilityPublic(),
	)
	if err != nil {
		return fmt.Errorf("start host: %w", err)
	}
	defer h.Close()

	// Full DHT server so provider records (used for rendezvous) are stored here.
	kdht, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		return fmt.Errorf("start dht: %w", err)
	}
	defer kdht.Close()
	if err := kdht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrap dht: %w", err)
	}

	// Encrypted store-and-forward mailbox: holds E2EE envelopes for members who
	// are offline, so a message reaches them on reconnect even when no other
	// peer is online. The node sees only opaque ciphertext + a 16-byte tag.
	// Bounded and in-memory (no disk, no storage cost to grow).
	mbox := mailbox.NewService(mailbox.New())
	// Optional push wake bridge: if APNs/FCM credentials are configured, register
	// a persistent token store (survives restarts, unlike envelopes) and a
	// notifier that sends contentless wakes on deposit. With no credentials this
	// is a no-op and the mailbox works exactly as before. See internal/mailbox/push.go.
	notifier, err := mailbox.NewNotifier()
	if err != nil {
		fmt.Fprintln(os.Stderr, "push notifier disabled:", err)
	} else if notifier != nil {
		tokPath := os.Getenv("CONCORD_PUSH_TOKENS")
		if tokPath == "" {
			tokPath = "push-tokens.json"
		}
		mbox.WithPush(mailbox.OpenPushStore(tokPath), notifier)
		fmt.Println("Push wake bridge enabled (tokens:", tokPath+").")
	}
	mbox.Attach(ctx, h)

	// TURN relay: lets a call hide participants' IPs from each other by relaying
	// media through this node (see turn.go). Off unless CONCORD_TURN_SECRET is
	// set. Its credential endpoint is served on the guest gateway below.
	ts := serveTURN(ctx)

	// Guest gateway: plain-HTTPS door for browser guests joining a meeting
	// (see guest.go). It relays their WebSocket to the meeting host and never
	// reads the meeting itself; it also serves TURN credentials when TURN is on.
	serveGuestGateway(ctx, h, ts)

	// GIF search proxy: members search Tenor through this node so that Google
	// never sees their IP or their search terms (see gifsearch.go). Always
	// registered — with no CONCORD_TENOR_KEY it answers "unavailable", which is
	// what lets the client explain the difference between an unconfigured node
	// and a broken one.
	serveGifSearch(ctx, h)

	fmt.Println("Concord rendezvous node running (DHT + relay + mailbox + guest gateway).")
	fmt.Println("PeerID:", h.ID())

	// When deployed behind a stable public hostname (e.g. fly.io), print the
	// exact address to share with friends — no manual multiaddr assembly.
	if host := os.Getenv("CONCORD_PUBLIC_HOST"); host != "" {
		fmt.Println("\n>>> SHARE THIS ADDRESS WITH YOUR FRIENDS <<<")
		fmt.Printf("  /dns/%s/tcp/%s/p2p/%s\n", host, port, h.ID())
		fmt.Printf("  /dns/%s/udp/%s/quic-v1/p2p/%s\n\n", host, port, h.ID())
	}

	fmt.Println("Local bootstrap addresses:")
	for _, a := range h.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", a, h.ID())
	}

	<-ctx.Done()
	fmt.Println("\nShutting down.")
	return nil
}

// relayIdentity returns a stable identity from CONCORD_RELAY_SEED (32-byte hex),
// or a fresh random one when unset.
func relayIdentity() (*identity.Identity, error) {
	seedHex := os.Getenv("CONCORD_RELAY_SEED")
	if seedHex == "" {
		fmt.Fprintln(os.Stderr, "warning: CONCORD_RELAY_SEED unset — using a random identity (PeerID changes each run)")
		return identity.Generate()
	}
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		return nil, fmt.Errorf("CONCORD_RELAY_SEED must be hex: %w", err)
	}
	return identity.FromSeed(seed)
}
