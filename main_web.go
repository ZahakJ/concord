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
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/ZahakJ/concord/internal/bridge"
	"github.com/ZahakJ/concord/internal/httpapi"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
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
	mux.Handle("/", staticAssets())

	// Clear the parked binary from a completed self-update, if any.
	bridge.CleanupOldBinary()

	httpSrv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()

	// The token rides the opened URL once; the page stores it and strips it
	// from the address bar (frontend/src/main.js), so it does not linger in
	// browser history or in a screenshot of the window.
	url := "http://" + addr + "/?t=" + token
	// Bind with a short retry: after a self-update restart the previous process
	// may hold the port for a beat (Windows spawn-and-exit path especially).
	var ln net.Listener
	var err error
	for i := 0; i < 25; i++ {
		ln, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "concord:", err)
		os.Exit(1)
	}
	// Print the URL WITH the token: on a headless box (CONCORD_NO_OPEN) this
	// line is the only way in, and the terminal is already as trusted as the
	// process itself.
	fmt.Printf("Concord is running — %s\n", url)
	if os.Getenv("CONCORD_NO_OPEN") == "" {
		go openBrowser(url)
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
