// Package appbus is the local app-bus the concord-bridge daemon serves: a
// tiny loopback HTTP API that lets the owner's OTHER local apps (trove,
// sentinel, anything) speak Concord without embedding a Concord node.
//
// The bridge process runs a full Concord client with its OWN identity — its
// own keypair, invited into a guild like any member — so every message it
// sends or reads is end-to-end encrypted by the normal MLS path. This
// package is only the last local inch: loopback HTTP guarded by a bearer
// token from a mode-0600 file.
//
// The contract (stable; see cmd/concord-bridge/README.md):
//
//	GET  /api/health                     → {ok, identity, guilds:[...]}
//	POST /api/send {channel, text}       → {ok, message_id}
//	GET  /api/messages?channel=&since=   → {messages:[...], next_cursor}
//
// channel accepts a channel ID, a channel name ("general"), or
// "guild/channel" ("Grey mane/general") when a bare name is ambiguous.
// since is the next_cursor from the previous read ("" = from the start);
// cursors are opaque strings (currently UnixNano), strictly ordered, so
// pollers never miss or re-read.
//
// App-to-app payloads ride as ordinary text messages with a machine-readable
// prefix — first line "APPBUS:<app>:<schema-version>", JSON body after — so
// they stay human-visible in the channel (the humans can watch their
// machines talk) and ignorable-by-convention for other apps.
package appbus

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zahak/concord/internal/domain"
)

// Node is the slice of the Concord client the bus needs — implemented by
// *app.Service; a stub in tests.
type Node interface {
	Fingerprint() string
	DisplayName() string
	Guilds() []domain.Guild
	SendMessage(channelID, content, replyTo string) (domain.Message, error)
	MessagesSince(channelID string, sinceNano int64, limit int) ([]domain.Message, error)
	ProfileName(fingerprint string) string
	// AccountFingerprintOf maps a message's sender credential to the account
	// fingerprint (collapsing linked-device leaves onto one identity).
	AccountFingerprintOf(cred []byte) string
}

// Send rate limiting: a per-channel token bucket. The bridge must never
// flood a guild — these are human-visible channels.
const (
	rateBurst    = 5
	ratePerSec   = 1.0
	readLimitMax = 500
)

// Server is the loopback HTTP handler.
type Server struct {
	node  Node
	token string

	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time // test seam
}

func New(node Node, token string) *Server {
	return &Server{node: node, token: token,
		buckets: map[string]*bucket{}, now: time.Now}
}

// Handler returns the routed http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.auth(s.handleHealth))
	mux.HandleFunc("POST /api/send", s.auth(s.handleSend))
	mux.HandleFunc("GET /api/messages", s.auth(s.handleMessages))
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		supplied := ""
		if strings.HasPrefix(h, "Bearer ") {
			supplied = h[len("Bearer "):]
		}
		if s.token == "" ||
			subtle.ConstantTimeCompare([]byte(supplied), []byte(s.token)) != 1 {
			writeErr(w, http.StatusUnauthorized, "missing or wrong bearer token")
			return
		}
		next(w, r)
	}
}

// -- endpoints ---------------------------------------------------------------

type guildInfo struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Kind     string        `json:"kind,omitempty"`
	Channels []channelInfo `json:"channels"`
}

type channelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) guildInfos() []guildInfo {
	guilds := s.node.Guilds()
	out := make([]guildInfo, 0, len(guilds))
	for _, g := range guilds {
		gi := guildInfo{ID: g.ID, Name: g.Name, Kind: g.Kind}
		for _, c := range g.Channels {
			if c.Type == "" || c.Type == "text" || c.Type == "announcement" {
				gi.Channels = append(gi.Channels, channelInfo{ID: c.ID, Name: c.Name})
			}
		}
		out = append(out, gi)
	}
	return out
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok": true,
		"identity": map[string]string{
			"fingerprint": s.node.Fingerprint(),
			"name":        s.node.DisplayName(),
		},
		"guilds": s.guildInfos(),
	})
}

// resolveChannel maps a channel spec (ID, bare name, or "guild/name") to a
// channel ID. Ambiguous bare names error with the candidates listed.
func (s *Server) resolveChannel(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("channel is required")
	}
	wantGuild, wantChan := "", spec
	if i := strings.Index(spec, "/"); i >= 0 {
		wantGuild, wantChan = strings.TrimSpace(spec[:i]), strings.TrimSpace(spec[i+1:])
	}
	wantChan = strings.TrimPrefix(wantChan, "#")
	var hits []string // "guild/channel" labels for the ambiguity error
	var hitIDs []string
	for _, g := range s.guildInfos() {
		for _, c := range g.Channels {
			if c.ID == spec {
				return c.ID, nil // exact ID always wins
			}
			if strings.EqualFold(c.Name, wantChan) &&
				(wantGuild == "" || strings.EqualFold(g.Name, wantGuild)) {
				hits = append(hits, g.Name+"/"+c.Name)
				hitIDs = append(hitIDs, c.ID)
			}
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no channel matches %q — check /api/health for the guilds this bridge identity is in", spec)
	case 1:
		return hitIDs[0], nil
	default:
		return "", fmt.Errorf("channel %q is ambiguous (%s) — use guild/channel or the id",
			spec, strings.Join(hits, ", "))
	}
}

type sendReq struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "body must be JSON {channel, text}")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}
	chID, err := s.resolveChannel(req.Channel)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allow(chID) {
		writeErr(w, http.StatusTooManyRequests,
			"rate limited — the bridge sends at most 1 msg/s (burst 5) per channel")
		return
	}
	m, err := s.node.SendMessage(chID, req.Text, "")
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "message_id": m.ID})
}

type busMessage struct {
	ID            string `json:"id"`
	Sender        string `json:"sender"`         // fingerprint
	SenderDisplay string `json:"sender_display"` // best-known display name
	TS            string `json:"ts"`             // RFC3339Nano UTC
	Text          string `json:"text"`
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	chID, err := s.resolveChannel(q.Get("channel"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var since int64
	if c := strings.TrimSpace(q.Get("since")); c != "" {
		since, err = strconv.ParseInt(c, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "since must be a cursor from a previous response")
			return
		}
	}
	limit := 200
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= readLimitMax {
			limit = n
		}
	}
	msgs, err := s.node.MessagesSince(chID, since, limit)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]busMessage, 0, len(msgs))
	cursor := since
	for _, m := range msgs {
		if m.Kind != "" || m.Deleted {
			continue
		}
		fpr := s.node.AccountFingerprintOf(m.Sender)
		name := m.Name
		if better := s.node.ProfileName(fpr); better != "" {
			name = better
		}
		out = append(out, busMessage{
			ID:            m.ID,
			Sender:        fpr,
			SenderDisplay: name,
			TS:            m.Sent.UTC().Format(time.RFC3339Nano),
			Text:          m.Content,
		})
		if ns := m.Sent.UnixNano(); ns > cursor {
			cursor = ns
		}
	}
	writeJSON(w, map[string]any{
		"messages":    out,
		"next_cursor": strconv.FormatInt(cursor, 10),
	})
}

// -- rate limiting -----------------------------------------------------------

type bucket struct {
	tokens float64
	last   time.Time
}

func (s *Server) allow(channelID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	b, ok := s.buckets[channelID]
	if !ok {
		b = &bucket{tokens: rateBurst, last: now}
		s.buckets[channelID] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * ratePerSec
	if b.tokens > rateBurst {
		b.tokens = rateBurst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// -- helpers -----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}
