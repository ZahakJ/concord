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
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, rpcResponse{Error: "bad request"})
		return
	}
	result, err := s.dispatch(req.Method, req.Args)
	resp := rpcResponse{Result: result}
	if err != nil {
		resp.Error = err.Error()
	}
	writeJSON(w, resp)
}

// dispatch maps a method name + JSON args to a bridge call, mirroring the
// method names Wails would bind. Explicit (not reflective) for clarity/safety.
func (s *webServer) dispatch(method string, args []json.RawMessage) (any, error) {
	switch method {
	case "GetBootstrap":
		return s.b.GetBootstrap()
	case "SetBootstrap":
		return nil, s.b.SetBootstrap(argStr(args, 0))
	case "SetBootstrapLive":
		return nil, s.b.SetBootstrapLive(argStr(args, 0))
	case "Session":
		return s.b.Session(), nil
	case "Logout":
		return nil, s.b.Logout()
	case "HasIdentity":
		return s.b.HasIdentity()
	case "ResetIdentity":
		return nil, s.b.ResetIdentity()
	case "Login":
		return nil, s.b.Login(argStr(args, 0))
	case "Identity":
		return s.b.Identity()
	case "Guilds":
		return s.b.Guilds()
	case "CreateGuild":
		return s.b.CreateGuild(argStr(args, 0))
	case "InviteCode":
		return s.b.InviteCode(argStr(args, 0))
	case "JoinViaInvite":
		return s.b.JoinViaInvite(argStr(args, 0))
	case "Messages":
		return s.b.Messages(argStr(args, 0))
	case "SendMessage":
		return nil, s.b.SendMessage(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "Members":
		return s.b.Members(argStr(args, 0))
	case "RemoveMember":
		return nil, s.b.RemoveMember(argStr(args, 0), argStr(args, 1))
	case "Contacts":
		return s.b.Contacts()
	case "Verify":
		return nil, s.b.Verify(argStr(args, 0))
	case "JoinVoice":
		return nil, s.b.JoinVoice(argStr(args, 0))
	case "LeaveVoice":
		return nil, s.b.LeaveVoice(argStr(args, 0))
	case "RelaySignal":
		return nil, s.b.RelaySignal(argStr(args, 0), argStr(args, 1))
	case "SendTyping":
		return nil, s.b.SendTyping(argStr(args, 0))
	case "SetDisplayName":
		return nil, s.b.SetDisplayName(argStr(args, 0))
	case "SetProfile":
		return nil, s.b.SetProfile(argStr(args, 0), argStr(args, 1), argStr(args, 2), argStr(args, 3), argStr(args, 4))
	case "VerifyFingerprint":
		return nil, s.b.VerifyFingerprint(argStr(args, 0))
	case "PinMessage":
		return nil, s.b.PinMessage(argStr(args, 0), argStr(args, 1))
	case "SearchMessages":
		return s.b.SearchMessages(argStr(args, 0))
	case "CreateChannel":
		return s.b.CreateChannel(argStr(args, 0), argStr(args, 1))
	case "RenameGuild":
		return nil, s.b.RenameGuild(argStr(args, 0), argStr(args, 1))
	case "LeaveGuild":
		return nil, s.b.LeaveGuild(argStr(args, 0))
	case "DeleteMessage":
		return nil, s.b.DeleteMessage(argStr(args, 0), argStr(args, 1))
	case "EditMessage":
		return nil, s.b.EditMessage(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	case "ToggleReaction":
		return nil, s.b.ToggleReaction(argStr(args, 0), argStr(args, 1), argStr(args, 2))
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// argStr decodes the i-th argument as a string, tolerating a missing arg.
func argStr(args []json.RawMessage, i int) string {
	if i >= len(args) {
		return ""
	}
	var s string
	_ = json.Unmarshal(args[i], &s)
	return s
}
