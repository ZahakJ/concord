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
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"

	"github.com/zahak/concord/internal/identity"
	"github.com/zahak/concord/internal/mailbox"
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

	// Relay service for NAT'd peers. Generous per-circuit limits (a friend group
	// behind a symmetric NAT may hold a long relayed session) but NOT infinite:
	// infinite limits turn the public fly.io node into a free open relay anyone
	// can proxy unbounded traffic through, exhausting its bandwidth/bill.
	relayRes := relay.DefaultResources()
	relayRes.Limit = &relay.RelayLimit{Duration: time.Hour, Data: 512 << 20} // 512 MB/hr per circuit
	relayRes.MaxReservations = 512
	relayRes.MaxCircuits = 64
	relayRes.MaxReservationsPerPeer = 8
	relayRes.MaxReservationsPerIP = 16

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(listen...),
		libp2p.Security(noise.ID, noise.New),
		libp2p.EnableRelayService(relay.WithResources(relayRes)),
		libp2p.EnableNATService(),
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
	mbox.Attach(ctx, h)

	fmt.Println("Concord rendezvous node running (DHT + relay + mailbox).")
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
