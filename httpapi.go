// httpapi.go is the transport shared by BOTH front ends: the browser-served
// web build and the native Wails desktop build. Method calls arrive at POST
// /rpc and live events stream over GET /events (SSE). The Wails app mounts these
// on its asset server so the frontend talks to the backend over the same HTTP
// surface everywhere — no dependence on Wails' JS bindings (which its binding
// generator can't produce on newer Go toolchains). No build tag: compiled into
// every variant.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
)

// sseEvent is one Server-Sent Event fanned out to connected clients.
type sseEvent struct {
	name string
	data any
}

// apiServer bridges HTTP to the transport-agnostic bridge and fans events out
// over SSE. trusted=true (the native Wails webview, which no external site can
// reach) skips the cross-origin CSRF guard the browser build needs.
type apiServer struct {
	b       *bridge
	trusted bool

	mu      sync.Mutex
	clients map[chan sseEvent]struct{}
}

func newAPIServer(ctx context.Context, trusted bool) *apiServer {
	return newAPIServerWith(newBridge(ctx), trusted)
}

func newAPIServerWith(b *bridge, trusted bool) *apiServer {
	return &apiServer{b: b, trusted: trusted, clients: map[chan sseEvent]struct{}{}}
}

// wireSSE routes bridge events to connected /events (SSE) clients. The browser
// build calls this; the Wails build routes events through the native runtime
// instead, so it doesn't.
func (s *apiServer) wireSSE() {
	s.b.onMessage = func(m MessageView) { s.broadcast(sseEvent{"message", m}) }
	s.b.onPresence = func() { s.broadcast(sseEvent{"presence", nil}) }
	s.b.onVoicePresence = func(v VoicePresence) { s.broadcast(sseEvent{"voice-presence", v}) }
	s.b.onVoiceSignal = func(v VoiceSignal) { s.broadcast(sseEvent{"voice-signal", v}) }
	s.b.onTyping = func(t TypingInfo) { s.broadcast(sseEvent{"typing", t}) }
	s.b.onGuildUpdate = func() { s.broadcast(sseEvent{"guild-updated", nil}) }
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

func (s *apiServer) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// CSRF guard (browser build only): the RPC surface is powerful (send/kick/
	// ban, profile changes, joining invites). It binds to loopback, but any
	// website the user visits could otherwise drive it with a cross-origin
	// "simple" request. Reject a foreign Origin; same-origin requests send a
	// loopback Origin or none. Also defeats DNS-rebinding. The native webview is
	// trusted (unreachable from other origins), so it skips this.
	if !s.trusted && !localOrigin(r.Header.Get("Origin")) {
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

// handleEvents streams bridge events to a client via Server-Sent Events.
func (s *apiServer) handleEvents(w http.ResponseWriter, r *http.Request) {
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

func (s *apiServer) addClient(ch chan sseEvent) {
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
}

func (s *apiServer) removeClient(ch chan sseEvent) {
	s.mu.Lock()
	delete(s.clients, ch)
	s.mu.Unlock()
}

func (s *apiServer) broadcast(ev sseEvent) {
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
