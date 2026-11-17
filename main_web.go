//go:build !wails

// Browser-served front end: the default build. It serves the same Svelte UI as
// the Wails desktop app, but over a local HTTP server with no native
// dependencies — so it runs anywhere Go does. The RPC/SSE transport lives in
// httpapi.go (shared with the Wails build); this file just wires it to a
// loopback HTTP listener and opens the browser.
package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/ZahakJ/concord/internal/bridge"
	"github.com/ZahakJ/concord/internal/cli"
	"github.com/ZahakJ/concord/internal/httpapi"
)

//go:embed all:frontend/dist
var assets embed.FS

// flavour names this build in --version.
const flavour = "web"

const usage = `Usage: concord [flag]

Starts Concord and opens it in your web browser. The interface is served on a
local address that is printed, with its access token, at startup.

Flags:
  --version  print the version and exit
  --help     print this help and exit

Environment:
  CONCORD_WEB_ADDR      address to serve the interface on (default 127.0.0.1:8787)
  CONCORD_NO_OPEN       set to anything to print the address without opening
                        a browser
  CONCORD_API_TOKEN     use this access token instead of a fresh random one
` + appEnvHelp

func main() {
	if done, code := cli.Parse(flag.NewFlagSet("concord", flag.ContinueOnError), os.Args[1:],
		flavour, usage, os.Stdout, os.Stderr); done {
		os.Exit(code)
	}

	addr := os.Getenv("CONCORD_WEB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Loopback is NOT a trust boundary. Every process on this machine can reach
	// 127.0.0.1, and this RPC surface includes RevealMnemonic — the 24 words
	// that ARE the account. Unauthenticated, any program the user runs could
	// have walked off with the identity, which is not a threat model a
	// key-is-your-account app gets to ignore. So the browser build mints a
	// bearer token exactly like the mobile shells already did, and hands it to
	// the page through the URL it opens.
	//
	// CONCORD_API_TOKEN pins it instead, which is how the dev scripts and the
	// multi-peer test harness drive the same surface (see CONTRIBUTING.md).
	// There is deliberately no "disable auth" switch: an escape hatch that
	// turns the lock off is the lock nobody notices is off.
	token := os.Getenv("CONCORD_API_TOKEN")
	if token == "" {
		var b [32]byte
		if _, err := rand.Read(b[:]); err != nil {
			fmt.Fprintln(os.Stderr, "concord: could not generate an API token:", err)
			os.Exit(1)
		}
		token = hex.EncodeToString(b[:])
	}

	// Browser build: enforce the cross-origin CSRF guard (trusted=false) and
	// stream events over SSE.
	srv := httpapi.New(ctx, false, httpapi.WithAuthToken(token))
	srv.WireSSE()
	defer srv.Bridge().Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", srv.HandleRPC)
	mux.HandleFunc("/events", srv.HandleEvents)
	mux.HandleFunc("/launch", srv.HandleLaunch)
	mux.Handle("/", staticAssets())

	// Clear the parked binary from a completed self-update, if any.
	bridge.CleanupOldBinary()

	httpSrv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()

	// The token never leaves this process on its own. What opens the page is a
	// single-use launch code (internal/httpapi/launch.go), which the page
	// trades for the token and then strips from the address bar
	// (frontend/src/main.js). A code is all that reaches the browser's command
	// line, a terminal or a launcher, and it is spent or dead within minutes.
	sock, isSock := socketPath(addr)
	ln, err := listen(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "concord:", err)
		os.Exit(1)
	}
	// With a token file the token goes there and nowhere else — in particular
	// not to stdout, which a service manager keeps in a log other users read.
	tokenFile := os.Getenv("CONCORD_API_TOKEN_FILE")
	if tokenFile != "" {
		if err := writeTokenFile(tokenFile, token); err != nil {
			fmt.Fprintln(os.Stderr, "concord: could not write the API token file:", err)
			os.Exit(1)
		}
	}
	switch {
	case isSock:
		fmt.Printf("Concord is running — unix socket %s\n", sock)
	case tokenFile != "":
		fmt.Printf("Concord is running — http://%s (token in %s)\n", addr, tokenFile)
	default:
		fmt.Println(startupLine(addr, stdoutIsTerminal(), srv.MintLaunchCode))
	}
	// A launcher (concord-url.sh) asks this socket for a fresh code each time
	// it opens the app, so nothing needs to scrape a log for a way in.
	if run := os.Getenv("XDG_RUNTIME_DIR"); run != "" && !isSock {
		if _, err := serveLaunchSocket(ctx, run, addr, srv.MintLaunchCode); err != nil {
			fmt.Fprintln(os.Stderr, "concord: launch socket:", err)
		}
	}
	// Unlock from a file when asked: a service then comes up signed in, and
	// stays signed in across a self-update's re-exec (same environment). Runs
	// after the listener is up so the UI can watch it happen; the passphrase
	// and its error text never reach the log, only whether it worked.
	if pf := os.Getenv("CONCORD_PASSPHRASE_FILE"); pf != "" {
		go func() {
			pass, err := readPassphraseFile(pf)
			if err != nil {
				fmt.Fprintln(os.Stderr, "concord: passphrase file:", err)
				return
			}
			if err := srv.Bridge().Login(pass); err != nil {
				fmt.Fprintln(os.Stderr, "concord: unlock from passphrase file failed")
				return
			}
			fmt.Println("concord: unlocked from passphrase file")
		}()
	}
	if os.Getenv("CONCORD_NO_OPEN") == "" && !isSock {
		go openBrowser("http://" + addr + "/?c=" + srv.MintLaunchCode())
	}
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "concord:", err)
		os.Exit(1)
	}
}

func staticAssets() http.Handler {
	// The PWA manifest's type isn't in Go's builtin table and the host OS's
	// mime database may not know it either (Windows registry, notably) —
	// register it so installability checks see the right Content-Type.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	sub, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		panic(err) // dist is embedded at build time; absence is a build bug
	}
	return http.FileServer(http.FS(sub))
}

// openBrowser opens the app in the default browser (best-effort) so running
// the binary is all a first-time user has to do. Skipped via CONCORD_NO_OPEN=1.
func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond) // let the listener come up
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// explorer.exe is the least AV-suspicious way to open a URL on Windows
		// (rundll32 is a common "living-off-the-land" pattern Defender flags).
		cmd = exec.Command("explorer", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// startupLine is what the binary says once it is listening. Under a service
// manager that line is kept in a log any process of this user can read, so
// off a terminal it names only the address. On a terminal (a headless box run
// by hand with CONCORD_NO_OPEN, say) it carries a launch code: the person
// reading it is the one who started the process, and the code is single use.
func startupLine(addr string, terminal bool, mint func() string) string {
	if !terminal {
		return "Concord is running — http://" + addr + " (open it with concord-url.sh, or the window it opened)"
	}
	return "Concord is running — http://" + addr + "/?c=" + mint() + "  (one use, 2 minutes; concord-url.sh makes another)"
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
