// Package httpapi is the transport shared by ALL front ends: the browser-served
// web build, the native Wails desktop build, and the in-process loopback server
// the mobile shells talk to. Method calls arrive at POST /rpc and live events
// stream over GET /events (SSE).
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/bridge"
)

// sseEvent is one Server-Sent Event fanned out to connected clients.
type sseEvent struct {
	name string
	data any
}

// Server bridges HTTP to the transport-agnostic bridge and fans events out
// over SSE. trusted=true (the native Wails webview, which no external site can
// reach) skips the cross-origin CSRF guard the browser build needs.
type Server struct {
	b       *bridge.Bridge
	trusted bool

	// authToken, when non-empty, is required on every request: as an
	// `Authorization: Bearer <token>` header on /rpc and a `?token=` query
	// parameter on /events (EventSource cannot set headers). Desktop builds
	// leave it empty; the mobile shells set it because loopback on Android is
	// reachable by every other app on the device — the Origin check below is a
	// browser CSRF guard, not authentication.
	authToken string

	// corsOrigins are Origin values (beyond loopback ones) that receive CORS
	// response headers, letting a webview served from its own scheme — Capacitor's
	// capacitor://localhost (iOS) and http://localhost (Android) — read responses.
	corsOrigins map[string]bool

	mu      sync.Mutex
	clients map[chan sseEvent]struct{}
}

// Option configures optional Server behavior.
type Option func(*Server)

// WithAuthToken requires the given bearer token on every request. Used by the
// mobile shells; desktop builds don't pass it.
func WithAuthToken(token string) Option {
	return func(s *Server) { s.authToken = token }
}

// WithCORSOrigins allows the given webview origins to make cross-origin
// requests to the server (Capacitor's capacitor://localhost and
// http://localhost). Origins are matched exactly.
func WithCORSOrigins(origins []string) Option {
	return func(s *Server) {
		s.corsOrigins = make(map[string]bool, len(origins))
		for _, o := range origins {
			s.corsOrigins[o] = true
		}
	}
}

func New(ctx context.Context, trusted bool, opts ...Option) *Server {
	return NewWith(bridge.New(ctx), trusted, opts...)
}

func NewWith(b *bridge.Bridge, trusted bool, opts ...Option) *Server {
	s := &Server{b: b, trusted: trusted, clients: map[chan sseEvent]struct{}{}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Bridge returns the underlying bridge (the owner closes it on shutdown).
func (s *Server) Bridge() *bridge.Bridge { return s.b }

// WireSSE routes bridge events to connected /events (SSE) clients. The browser
// build calls this; the Wails build routes events through the native runtime
// instead, so it doesn't.
func (s *Server) WireSSE() {
	s.b.OnMessage = func(m bridge.MessageView) { s.broadcast(sseEvent{"message", m}) }
	s.b.OnPresence = func() { s.broadcast(sseEvent{"presence", nil}) }
	s.b.OnVoicePresence = func(v bridge.VoicePresence) { s.broadcast(sseEvent{"voice-presence", v}) }
	s.b.OnVoiceSignal = func(v bridge.VoiceSignal) { s.broadcast(sseEvent{"voice-signal", v}) }
	s.b.OnTyping = func(t bridge.TypingInfo) { s.broadcast(sseEvent{"typing", t}) }
	s.b.OnGuildUpdate = func() { s.broadcast(sseEvent{"guild-updated", nil}) }
	s.b.OnGuildInvite = func(inv appsvc.GuildInvite) { s.broadcast(sseEvent{"guild-invite", inv}) }
	s.b.OnReadState = func(r bridge.ReadStateView) { s.broadcast(sseEvent{"read-state", r}) }
	s.b.OnStory = func(u bridge.StoryUpdate) { s.broadcast(sseEvent{"story", u}) }
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

// applyCORS emits CORS headers when the request Origin is an allowed webview
// origin, and reports whether it handled an OPTIONS preflight.
func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request) (preflight bool) {
	origin := r.Header.Get("Origin")
	if origin == "" || !s.corsOrigins[origin] {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// authorized checks the bearer token (or ?token= for SSE). True when no token
// is configured (desktop builds).
func (s *Server) authorized(r *http.Request) bool {
	if s.authToken == "" {
		return true
	}
	supplied := r.URL.Query().Get("token")
	if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		supplied = h[7:]
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(s.authToken)) == 1
}

func (s *Server) HandleRPC(w http.ResponseWriter, r *http.Request) {
	if s.applyCORS(w, r) {
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// CSRF guard (browser build only): the RPC surface is powerful (send/kick/
	// ban, profile changes, joining invites). It binds to loopback, but any
	// website the user visits could otherwise drive it with a cross-origin
	// "simple" request. Reject a foreign Origin; same-origin requests send a
	// loopback Origin or none. Also defeats DNS-rebinding. The native webview is
	// trusted (unreachable from other origins), so it skips this. Allowed CORS
	// origins (the mobile webviews) pass explicitly — they carry a bearer token
	// besides.
	if !s.trusted && !localOrigin(r.Header.Get("Origin")) && !s.corsOrigins[r.Header.Get("Origin")] {
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

// HandleEvents streams bridge events to a client via Server-Sent Events.
func (s *Server) HandleEvents(w http.ResponseWriter, r *http.Request) {
	if s.applyCORS(w, r) {
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// This stream's lifetime IS the client's lifetime, which is what the
	// background-pacing vote needs and cannot get from /rpc: an RPC call is
	// over the moment it answers, so nothing about it says whether the page
	// that made it still exists. A tab that hides reports itself hidden and
	// keeps voting; a tab that is closed — or a laptop whose lid came down —
	// simply stops, and the deferred DropClient takes it out of the vote so it
	// cannot pin the node to the foreground cadence forever. See
	// bridge/visibility.go.
	if id := r.URL.Query().Get("client"); id != "" {
		s.b.AttachClient(id)
		defer s.b.DropClient(id)
	}

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

func (s *Server) addClient(ch chan sseEvent) {
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) removeClient(ch chan sseEvent) {
	s.mu.Lock()
	delete(s.clients, ch)
	s.mu.Unlock()
}

func (s *Server) broadcast(ev sseEvent) {
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
