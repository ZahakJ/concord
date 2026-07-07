//go:build !wails

// Browser-served front end: the default build. It serves the same Svelte UI as
// the Wails desktop app, but over a local HTTP server with no native
// dependencies — so it runs anywhere Go does. The RPC/SSE transport lives in
// httpapi.go (shared with the Wails build); this file just wires it to a
// loopback HTTP listener and opens the browser.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"
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

	// Browser build: enforce the cross-origin CSRF guard (trusted=false) and
	// stream events over SSE.
	srv := newAPIServer(ctx, false)
	srv.wireSSE()
	defer srv.b.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", srv.handleRPC)
	mux.HandleFunc("/events", srv.handleEvents)
	mux.Handle("/", staticAssets())

	httpSrv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()

	url := "http://" + addr
	fmt.Printf("Concord is running — %s\n", url)
	if os.Getenv("CONCORD_NO_OPEN") == "" {
		go openBrowser(url)
	}
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "concord:", err)
		os.Exit(1)
	}
}

func staticAssets() http.Handler {
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
