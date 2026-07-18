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
//	GET  /api/health                          → {ok, identity, guilds:[...]}
//	POST /api/send {channel, text, kind}      → {ok, message_id}
//	GET  /api/messages?channel=&since=&kind=  → {messages:[...], next_cursor}
//
// channel accepts a channel ID, a channel name ("general"), or
// "guild/channel" ("Grey mane/general") when a bare name is ambiguous.
// since is the next_cursor from the previous read ("" = from the start);
// cursors are opaque strings (currently UnixNano), strictly ordered, so
// pollers never miss or re-read.
//
// # Two planes, one transport
//
// This bus originally sent app payloads as ordinary chat messages, on the
// theory that leaving them visible let "the humans watch their machines talk".
// That was wrong, and the evidence was sentinel filling a guild's #general
// with APPBUS lines nobody wanted to read. Machine data is not conversation,
// and putting it in a conversation view degrades the conversation.
//
// So payloads now ride a separate DATA PLANE: kind "app" (domain.KindApp)
// instead of "chat". Everything else is identical — same bridge identity, same
// MLS group, same end-to-end encryption, same store, same cursor feed. Only
// the rendering contract differs: no client shows app-kind messages in a
// channel, and they never mark it unread or ping anyone.
//
//   - POST /api/send takes an optional "kind": "chat" (default) or "app".
//     Callers pushing machine payloads should send kind "app".
//   - GET /api/messages reports "kind" per message, and takes an optional
//     ?kind=app or ?kind=chat filter. With no filter it returns BOTH, which is
//     exactly what it returned before — existing pollers are unaffected.
//
// # Legacy producers keep working
//
// Payloads still carry the "APPBUS:<app>:<schema-version>" first line, and a
// message with that prefix is treated as app-kind even when it arrived with no
// kind field at all (domain.Message.IsApp). App-bus producers live in other
// repos, on other release cadences, on other machines; requiring a lockstep
// upgrade would mean either breaking them or leaving their traffic in the
// human channel until the last one shipped. So the prefix remains the
// compatibility contract, the kind field is the explicit one, and either is
// enough to keep machine data out of the conversation.
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
	// SendAppMessage publishes on the data plane (domain.KindApp): same
	// encryption and same channel, but never rendered as conversation.
	SendAppMessage(channelID, content, replyTo string) (domain.Message, error)
	MessagesSince(channelID string, sinceNano int64, limit int) ([]domain.Message, error)
	ProfileName(fingerprint string) string
	// AccountFingerprintOf maps a message's sender credential to the account
	// fingerprint (collapsing linked-device leaves onto one identity).
	AccountFingerprintOf(cred []byte) string
}

// Send rate limiting: a per-channel, per-plane token bucket.
//
// The chat limit is about human attention: a message that renders in a human
// conversation must never arrive faster than a human would send one, so the
// bridge is held to 1/s (burst 5) exactly as before.
//
// The app plane is not rendered anywhere and costs nobody's attention, so that
// budget is the wrong one — it was the reason machine payloads felt like spam
// AND the reason a busy sentinel could stall on the limiter. It gets its own,
// looser bucket, still bounded so a wedged producer can't fill the store or
// saturate the gossip topic. The two planes are limited independently: app
// chatter can never starve a human message of its budget.
const (
	rateBurst     = 5
	ratePerSec    = 1.0
	appRateBurst  = 60
	appRatePerSec = 20.0
	readLimitMax  = 500
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

// Capabilities advertised by /api/health.
//
// This exists because unknown fields degrade SILENTLY on this API: an older
// bridge handed ?kind=app on a read, or "kind":"app" in a send body, ignores it
// and returns 200. For a read that is harmless. For a send it is not — the
// payload lands in the human channel as ordinary chat and the sender cannot
// tell that from success.
//
// So support has to be detectable BEFORE sending, not inferred from a response
// that looks identical either way. A producer should call /api/health, look for
// "data_plane" in capabilities, and refuse to push machine payloads into a
// human channel through a bridge that would render them.
const CapDataPlane = "data_plane"

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok": true,
		"identity": map[string]string{
			"fingerprint": s.node.Fingerprint(),
			"name":        s.node.DisplayName(),
		},
		"guilds":       s.guildInfos(),
		"capabilities": []string{CapDataPlane},
		// The same signal as a bare boolean, for callers that would rather test
		// a field than scan a list. An older bridge has neither.
		CapDataPlane: true,
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
	// Kind selects the plane: "chat" (default, renders in the conversation) or
	// "app" (machine payload, never rendered). Absent means chat so existing
	// callers keep their exact current behaviour.
	Kind string `json:"kind"`
}

// planeOf maps a request's kind field to a plane. Empty and "chat" are the
// human plane; "app" is the data plane; anything else is a client bug worth
// reporting rather than silently guessing at.
func planeOf(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "chat":
		return domain.KindChat, nil
	case "app":
		return domain.KindApp, nil
	}
	return "", fmt.Errorf("kind must be \"chat\" or \"app\", got %q", kind)
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "body must be JSON {channel, text, kind?}")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeErr(w, http.StatusBadRequest, "text is required")
		return
	}
	kind, err := planeOf(req.Kind)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// A legacy producer that never learned about kinds still marks its payloads
	// with the APPBUS: prefix — honor that as the data plane, so its traffic
	// stops landing in the conversation without the producer changing at all.
	if kind == domain.KindChat && strings.HasPrefix(req.Text, domain.AppBusPrefix) {
		kind = domain.KindApp
	}
	chID, err := s.resolveChannel(req.Channel)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.allow(chID, kind) {
		if kind == domain.KindApp {
			writeErr(w, http.StatusTooManyRequests,
				"rate limited — the app plane accepts at most 20 msg/s (burst 60) per channel")
		} else {
			writeErr(w, http.StatusTooManyRequests,
				"rate limited — the bridge sends at most 1 msg/s (burst 5) per channel")
		}
		return
	}
	var m domain.Message
	if kind == domain.KindApp {
		m, err = s.node.SendAppMessage(chID, req.Text, "")
	} else {
		m, err = s.node.SendMessage(chID, req.Text, "")
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "message_id": m.ID, "kind": kindLabel(kind)})
}

// kindLabel renders a plane for the wire, where chat is spelled out rather
// than sent as the empty string that represents it internally.
func kindLabel(kind string) string {
	if kind == domain.KindApp {
		return "app"
	}
	return "chat"
}

type busMessage struct {
	ID            string `json:"id"`
	Sender        string `json:"sender"`         // fingerprint
	SenderDisplay string `json:"sender_display"` // best-known display name
	TS            string `json:"ts"`             // RFC3339Nano UTC
	Text          string `json:"text"`
	// Kind is "chat" or "app". Legacy APPBUS:-prefixed messages report "app"
	// even though they were stored before the kind field existed.
	Kind string `json:"kind"`
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
	// Optional plane filter. Absent means "both", which is byte-for-byte what
	// this endpoint returned before the planes were split — a poller written
	// against the old contract sees no change.
	var wantKind string
	filtered := false
	if k := strings.TrimSpace(q.Get("kind")); k != "" {
		wantKind, err = planeOf(k)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		filtered = true
	}
	msgs, err := s.node.MessagesSince(chID, since, limit)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]busMessage, 0, len(msgs))
	cursor := since
	for _, m := range msgs {
		// The cursor advances past EVERY row this page covered, including ones
		// the filter drops. Advancing only past emitted rows would make a
		// ?kind=app poller re-read the same chat messages forever — and stall
		// outright once a page filled with them.
		if ns := m.Sent.UnixNano(); ns > cursor {
			cursor = ns
		}
		// System/guest/call notices are still not bus traffic; app payloads now
		// are. EffectiveKind folds legacy APPBUS: messages onto KindApp.
		kind := m.EffectiveKind()
		if m.Deleted || (kind != domain.KindChat && kind != domain.KindApp) {
			continue
		}
		if filtered && kind != wantKind {
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
			Kind:          kindLabel(kind),
		})
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

// allow spends one token from the (channel, plane) bucket. Keying on the plane
// as well as the channel is what keeps a chatty sentinel from ever consuming
// the budget a human message needs.
func (s *Server) allow(channelID, kind string) bool {
	burst, perSec := float64(rateBurst), ratePerSec
	if kind == domain.KindApp {
		burst, perSec = appRateBurst, appRatePerSec
	}
	key := kindLabel(kind) + "\x00" + channelID

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	b, ok := s.buckets[key]
	if !ok {
		b = &bucket{tokens: burst, last: now}
		s.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * perSec
	if b.tokens > burst {
		b.tokens = burst
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
