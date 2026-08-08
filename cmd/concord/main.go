// Command concord is the Concord entry point.
//
// The Wails desktop GUI is the primary front end, but it depends on native
// WebView libraries (webkit2gtk on Linux) that may not be installed on every
// machine. To keep the core independently runnable and testable, this binary
// also supports headless modes that drive the same internal/app Service the
// GUI will bind to:
//
//	concord --status   boot identity, print fingerprint/PeerID, exit
//	concord --serve    boot identity + networking, discover LAN peers, run
//
// Passphrase handling: the passphrase is read from a hidden terminal prompt, or
// from CONCORD_PASSPHRASE when there is no TTY (CI / local multi-peer scripts).
// The GUI will prompt for it in-app.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/identity"
	"golang.org/x/term"
)

func main() {
	serve := flag.Bool("serve", false, "boot identity + networking and run until interrupted (headless)")
	flag.Bool("status", false, "boot identity and print status, then exit (default mode)")
	flag.Parse()

	var err error
	if *serve {
		err = runServe()
	} else {
		err = runStatus()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "concord:", err)
		os.Exit(1)
	}
}

// runStatus boots only the identity layer and reports it.
func runStatus() error {
	ksPath, err := app.KeystorePath()
	if err != nil {
		return err
	}
	pass, err := readPassphrase()
	if err != nil {
		return err
	}
	id, created, err := identity.LoadOrCreate(ksPath, pass)
	if err != nil {
		return err
	}
	if created {
		fmt.Println("Created a new Concord identity.")
	} else {
		fmt.Println("Loaded existing Concord identity.")
	}
	fmt.Println("Keystore:    ", ksPath)
	fmt.Println("Fingerprint: ", id.Fingerprint())
	return nil
}

// runServe brings up the full Service and logs peer presence until Ctrl-C.
func runServe() error {
	dataDir, err := app.DataDir()
	if err != nil {
		return err
	}
	pass, err := readPassphrase()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := app.Config{DataDir: dataDir, Passphrase: pass}
	if bs := os.Getenv("CONCORD_BOOTSTRAP"); bs != "" {
		cfg.BootstrapPeers = strings.Split(bs, ",")
	}
	if os.Getenv("CONCORD_DISABLE_MDNS") == "1" {
		cfg.DisableMDNS = true
	}
	svc, err := app.Start(ctx, cfg)
	if err != nil {
		return err
	}
	defer svc.Close()

	fmt.Println("Concord node running (mDNS LAN discovery on).")
	return runREPL(ctx, svc)
}

// readPassphrase reads the keystore passphrase without echoing it, falling back
// to the CONCORD_PASSPHRASE environment variable when stdin is not a terminal.
func readPassphrase() (string, error) {
	if env := os.Getenv("CONCORD_PASSPHRASE"); env != "" {
		return env, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no TTY and CONCORD_PASSPHRASE is unset")
	}
	fmt.Print("Passphrase: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", fmt.Errorf("passphrase must not be empty")
	}
	return string(b), nil
}
