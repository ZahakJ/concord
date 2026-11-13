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
//	concord --bot      boot identity + networking, speak JSON lines on
//	                   stdin/stdout for a script (docs/BOTS.md)
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
	"github.com/ZahakJ/concord/internal/cli"
	"github.com/ZahakJ/concord/internal/identity"
	"golang.org/x/term"
)

const usage = `Usage: concord [--status | --serve | --bot | --version | --help]

Runs Concord without a window: for servers, scripts and bots.

Flags:
  --status   unlock (or create) the identity, print its fingerprint and exit;
             the default when no flag is given
  --serve    unlock the identity, join the network and run until interrupted,
             with a small command prompt on stdin
  --bot      unlock the identity, join the network and speak newline-delimited
             JSON on stdin/stdout (docs/BOTS.md)
  --version  print the version and exit
  --help     print this help and exit

Environment:
  CONCORD_PASSPHRASE    the identity's passphrase; required when stdin is not a
                        terminal, otherwise it is asked for
  CONCORD_HOME          keep the profile in this directory instead of the
                        default concord folder in the user config directory
  CONCORD_BOOTSTRAP     comma-separated rendezvous multiaddrs to bootstrap from
  CONCORD_DISABLE_MDNS  set to 1 to stop finding peers on the local network
`

func main() {
	fs := flag.NewFlagSet("concord", flag.ContinueOnError)
	serve := fs.Bool("serve", false, "boot identity + networking and run until interrupted (headless)")
	botMode := fs.Bool("bot", false, "boot identity + networking and speak newline-delimited JSON on stdin/stdout (see docs/BOTS.md)")
	fs.Bool("status", false, "boot identity and print status, then exit (default mode)")
	if done, code := cli.Parse(fs, os.Args[1:], "headless", usage, os.Stdout, os.Stderr); done {
		os.Exit(code)
	}

	var err error
	switch {
	case *botMode:
		err = runBot()
	case *serve:
		err = runServe()
	default:
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

	svc, err := app.Start(ctx, serviceConfig(dataDir, pass))
	if err != nil {
		return err
	}
	defer svc.Close()

	fmt.Println("Concord node running (mDNS LAN discovery on).")
	return runREPL(ctx, svc)
}

// serviceConfig is the Service configuration every headless mode shares,
// including the environment overrides local multi-peer scripts rely on.
func serviceConfig(dataDir, pass string) app.Config {
	cfg := app.Config{DataDir: dataDir, Passphrase: pass}
	if bs := os.Getenv("CONCORD_BOOTSTRAP"); bs != "" {
		cfg.BootstrapPeers = strings.Split(bs, ",")
	}
	if os.Getenv("CONCORD_DISABLE_MDNS") == "1" {
		cfg.DisableMDNS = true
	}
	return cfg
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
