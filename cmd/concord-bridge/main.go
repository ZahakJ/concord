// concord-bridge — a headless Concord client that serves the local app-bus.
//
// It runs a FULL Concord node with its own identity (own keypair, own data
// dir, invited into guilds like any member), and exposes the tiny loopback
// HTTP contract in internal/appbus so the owner's other local apps (trove,
// sentinel, …) can send and read Concord messages without embedding a node.
// End-to-end encryption is untouched: the bridge is simply another E2EE peer.
//
// Usage:
//
//	concord-bridge serve                 run the daemon (default)
//	concord-bridge id                    print this bridge's fingerprint
//	concord-bridge join <invite-code>    join a guild via an invite code
//	concord-bridge name <display-name>   set the bridge's display name
//
// State lives under $CONCORD_BRIDGE_HOME (default ~/.config/concord-bridge):
// the identity keystore + encrypted DB (standard Concord layout), `pass`
// (the auto-generated keystore passphrase, 0600) and `token` (the API bearer
// token, 0600 — local clients read it from there). The HTTP address comes
// from $CONCORD_BRIDGE_ADDR (default 127.0.0.1:8790) and refuses to bind
// anything but loopback.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	appsvc "github.com/zahak/concord/internal/app"
	"github.com/zahak/concord/internal/appbus"
)

const defaultAddr = "127.0.0.1:8790"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "concord-bridge:", err)
		os.Exit(1)
	}
}

func run() error {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	home, err := bridgeHome()
	if err != nil {
		return err
	}
	switch cmd {
	case "serve":
		return serve(home)
	case "id":
		return withNode(home, func(svc *appsvc.Service) error {
			fmt.Println(svc.Fingerprint())
			return nil
		})
	case "join":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: concord-bridge join <invite-code>")
		}
		return withNode(home, func(svc *appsvc.Service) error {
			g, err := svc.JoinViaInvite(strings.TrimSpace(os.Args[2]))
			if err != nil {
				return err
			}
			// linger so the MLS welcome + first sync settle before exit
			time.Sleep(3 * time.Second)
			fmt.Printf("joined %q (%s)\n", g.Name, g.ID)
			return nil
		})
	case "name":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: concord-bridge name <display-name>")
		}
		return withNode(home, func(svc *appsvc.Service) error {
			if err := svc.SetDisplayName(os.Args[2]); err != nil {
				return err
			}
			fmt.Println("display name set:", os.Args[2])
			return nil
		})
	default:
		return fmt.Errorf("unknown command %q (serve | id | join | name)", cmd)
	}
}

func bridgeHome() (string, error) {
	home := os.Getenv("CONCORD_BRIDGE_HOME")
	if home == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(cfg, "concord-bridge")
	}
	return home, os.MkdirAll(home, 0o700)
}

// secretFile returns the file's contents, generating a fresh random secret
// (0600) on first use.
func secretFile(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return "", err
	}
	return secret, nil
}

func startNode(ctx context.Context, home string) (*appsvc.Service, error) {
	pass, err := secretFile(filepath.Join(home, "pass"))
	if err != nil {
		return nil, err
	}
	cfg := appsvc.Config{DataDir: home, Passphrase: pass}
	if bs := os.Getenv("CONCORD_BOOTSTRAP"); bs != "" {
		cfg.BootstrapPeers = strings.Split(bs, ",")
	}
	if os.Getenv("CONCORD_DISABLE_MDNS") == "1" {
		cfg.DisableMDNS = true
	}
	return appsvc.Start(ctx, cfg)
}

// withNode runs fn against a briefly-started node (the one-shot subcommands).
func withNode(home string, fn func(*appsvc.Service) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := startNode(ctx, home)
	if err != nil {
		return err
	}
	defer svc.Close()
	// give mDNS/DHT a beat to find peers before invite redemption etc.
	time.Sleep(2 * time.Second)
	return fn(svc)
}

func serve(home string) error {
	addr := os.Getenv("CONCORD_BRIDGE_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad CONCORD_BRIDGE_ADDR %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("refusing to bind %q — the app-bus is loopback-only", addr)
	}
	token, err := secretFile(filepath.Join(home, "token"))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, err := startNode(ctx, home)
	if err != nil {
		return err
	}
	defer svc.Close()
	if svc.DisplayName() == "" {
		_ = svc.SetDisplayName("bridge") // honest default; `name` overrides
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           appbus.New(svc, token).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	fmt.Printf("concord-bridge: %s serving %s (token: %s)\n",
		svc.Fingerprint(), addr, filepath.Join(home, "token"))

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errc:
		return err
	}
}
