//go:build !wails

// Browser-served front end: the default build. It serves the same Svelte UI as
// the Wails desktop app, but over a local HTTP server with no native
// dependencies — so it runs anywhere Go does. Method calls arrive at POST /rpc
// and live events stream over GET /events (SSE). This is the variant to use
// until the system WebView (webkit2gtk) is installed for the native window.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
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

	srv := newWebServer(ctx)
	defer srv.b.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", srv.handleRPC)
	mux.HandleFunc("/events", srv.handleEvents)
	mux.Handle("/", srv.static())

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

// webServer bridges HTTP to the transport-agnostic bridge and fans out events
// to connected browser tabs over SSE.
type webServer struct {
	b *bridge

	mu      sync.Mutex
	clients map[chan sseEvent]struct{}
}

type sseEvent struct {
	name string
	data any
}

func newWebServer(ctx context.Context) *webServer {
	s := &webServer{b: newBridge(ctx), clients: map[chan sseEvent]struct{}{}}
	s.b.onMessage = func(m MessageView) { s.broadcast(sseEvent{"message", m}) }
	s.b.onPresence = func() { s.broadcast(sseEvent{"presence", nil}) }
	s.b.onVoicePresence = func(v VoicePresence) { s.broadcast(sseEvent{"voice-presence", v}) }
	s.b.onVoiceSignal = func(v VoiceSignal) { s.broadcast(sseEvent{"voice-signal", v}) }
	s.b.onTyping = func(t TypingInfo) { s.broadcast(sseEvent{"typing", t}) }
	s.b.onGuildUpdate = func() { s.broadcast(sseEvent{"guild-updated", nil}) }
	return s
}

func (s *webServer) static() http.Handler {
	sub, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		panic(err) // dist is embedded at build time; absence is a build bug
	}
	return http.FileServer(http.FS(sub))
}

// rpcRequest/rpcResponse are the POST /rpc envelope. args are decoded per method.
type rpcRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type rpcResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *webServer) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// CSRF guard: the RPC surface is powerful (send/kick/ban, profile changes,
	// joining invites). It binds to loopback, but any website the user visits
	// could otherwise drive it with a cross-origin "simple" request (no preflight
	// for text/plain). Reject requests carrying a foreign Origin; same-origin
	// requests from our own page send a matching (loopback) Origin or none, and
	// non-browser clients send none. This also defeats DNS-rebinding, whose
	// Origin stays the attacker's domain.
	if !localOrigin(r.Header.Get("Origin")) {
		http.Error(w, "cross-origin request forbidden", http.StatusForbidden)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, rpcResponse{Error: "bad request"})
		return
	}
	result, err := s.b.Dispatch(req.Method, req.Args)
	resp := rpcResponse{Result: result}
	if err != nil {
		resp.Error = err.Error()
	}
	writeJSON(w, resp)
}

// handleEvents streams bridge events to a browser tab via Server-Sent Events.
func (s *webServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan sseEvent, 16)
	s.addClient(ch)
	defer s.removeClient(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			payload, _ := json.Marshal(ev.data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, payload)
			flusher.Flush()
		}
	}
}

func (s *webServer) addClient(ch chan sseEvent) {
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
}

func (s *webServer) removeClient(ch chan sseEvent) {
	s.mu.Lock()
	delete(s.clients, ch)
	s.mu.Unlock()
}

func (s *webServer) broadcast(ev sseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- ev:
		default: // drop for a slow client rather than block the Service
		}
	}
}

// localOrigin reports whether an Origin header is safe to accept: empty (a
// non-browser client or a same-origin request that sends no Origin) or a
// loopback host. A page on any real website carries its own Origin and is
// rejected.
func localOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
