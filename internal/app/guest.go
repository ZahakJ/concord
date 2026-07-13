package app

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/domain"
)

// Browser guests: someone with just the meeting link joins from a plain web
// page served by the rendezvous node — no Concord install, no account. The
// rendezvous relays the guest's WebSocket to THIS node over a libp2p stream,
// and this node is the guest's crypto endpoint: it decrypts the meeting for
// them and sends their messages into the MLS group under its own key,
// attributed "👤 Name (guest)". That is the deliberate trust model — the
// HOST vouches for (and can read) the guests they invite; full members keep
// end-to-end encryption among themselves. Guests are scoped hard: one
// meeting's one channel, chat only, rate-limited, dead when the meeting or
// the token expires.

const (
	maxGuestsPerMeeting = 5
	maxGuestNameBytes   = 24
	maxGuestMsgBytes    = 2000
	// A WebRTC offer/answer is far bigger than a chat line (a few KB of SDP,
	// more once video codecs are in it). Frames are capped generously; the chat
	// CONTENT limit above still applies to messages.
	maxGuestFrameBytes = 32 << 10
	// Signaling bypasses the chat rate limit (ICE trickles in bursts) but is
	// bounded on its own: enough for several renegotiations, not enough to be a
	// pipe into the app.
	guestSignalBurst  = 240
	guestSignalRefill = 12 // frames per second, replenished

	guestHistoryCount   = 30
	// Rate limit: a small burst, then one message per second.
	guestBurst      = 5
	guestRefillEach = time.Second
)

// guestToken is one issued guest link, valid for the meeting's lifetime.
type guestToken struct {
	guildID   string
	channelID string
	expires   time.Time
}

// guestSession is one live browser guest. `id` makes it addressable as a voice
// peer ("guest:<id>"): the WebRTC mesh in the app treats a guest exactly like
// any other participant, and RelaySignal routes that prefix down this session's
// stream instead of over libp2p. Media itself is direct browser↔app (P2P,
// DTLS-SRTP) — it never touches the rendezvous.
type guestSession struct {
	id        string
	name      string
	channelID string
	send      chan []byte // JSON lines queued to the guest (dropped when full)

	callMu sync.Mutex
	inCall bool
}

type guestFrame struct {
	Type    string `json:"type"`
	Token   string `json:"token,omitempty"`
	Name    string `json:"name,omitempty"`
	From    string `json:"from,omitempty"`
	Content string `json:"content,omitempty"`
	Sent    string `json:"sent,omitempty"`
	Meeting string `json:"meeting,omitempty"`
	Reason  string `json:"reason,omitempty"`
	// Call frames: "call" carries Action ("join"/"leave"); "signal" carries an
	// opaque WebRTC SDP/ICE blob, relayed verbatim in both directions.
	Action  string          `json:"action,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Channel string          `json:"channel,omitempty"` // the voice room the guest signals about
}

// initGuests wires the stream handler and the message tap. Called from Start.
func (s *Service) initGuests() {
	s.guestMu.Lock()
	if s.guestTokens == nil {
		s.guestTokens = map[string]guestToken{}
		s.guestSessions = map[string][]*guestSession{} // channelID -> sessions
		s.guestByID = map[string]*guestSession{}       // session id -> session
	}
	s.guestMu.Unlock()
	s.host.HandleGuestSessions(func(conn io.ReadWriteCloser, remote peer.ID) {
		s.serveGuest(conn)
	})
	// Forward every message in a guest-attended channel to its guests.
	s.OnMessage(func(m domain.Message) {
		if m.Deleted || (m.Kind != "" && m.Kind != "system" && m.Kind != "guest") {
			return
		}
		s.guestMu.Lock()
		sessions := append([]*guestSession(nil), s.guestSessions[m.ChannelID]...)
		s.guestMu.Unlock()
		if len(sessions) == 0 {
			return
		}
		typ := "msg"
		if m.Kind == "system" {
			typ = "sys" // a room notice, not something the host said
		}
		line, _ := json.Marshal(guestFrame{
			Type: typ, From: s.senderLabel(m), Content: m.Content,
			Sent: m.Sent.UTC().Format(time.RFC3339),
		})
		for _, g := range sessions {
			select {
			case g.send <- line:
			default: // guest too slow: drop rather than block the app
			}
		}
	})
}

// guestPeerID is how a browser guest appears to the voice mesh. The app's
// RelaySignal recognizes this prefix and writes down the guest's stream instead
// of dialing libp2p, so the mesh needs no idea guests exist.
func guestPeerID(id string) string { return "guest:" + id }

// guestFingerprint carries the guest's NAME where a member's fingerprint would
// go, so the call roster can label them without inventing an identity for
// someone who has none. (A guest is not authenticated — that's the whole point
// of a guest.)
func guestFingerprint(name string) string { return "guest:" + name }

func newGuestSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (g *guestSession) callActive() bool {
	g.callMu.Lock()
	defer g.callMu.Unlock()
	return g.inCall
}

// guestJoinCall announces the guest to the app as a voice participant. The mesh
// then offers them a connection exactly like it would any peer.
func (s *Service) guestJoinCall(sess *guestSession) {
	sess.callMu.Lock()
	if sess.inCall {
		sess.callMu.Unlock()
		return
	}
	sess.inCall = true
	sess.callMu.Unlock()
	s.emitVoicePresence(guestPeerID(sess.id), guestFingerprint(sess.name), sess.channelID, "join")
	// Peers announce themselves on a heartbeat and the UI evicts anyone silent
	// for ~9s (that's how a crashed peer disappears). A guest has no gossip, so
	// beat on their behalf until they leave — otherwise they'd vanish from the
	// call roster after nine seconds while still perfectly connected.
	go func() {
		t := time.NewTicker(voiceHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-t.C:
				if !sess.callActive() {
					return
				}
				s.emitVoicePresence(guestPeerID(sess.id), guestFingerprint(sess.name), sess.channelID, "join")
			}
		}
	}()
}

func (s *Service) guestLeaveCall(sess *guestSession) {
	sess.callMu.Lock()
	if !sess.inCall {
		sess.callMu.Unlock()
		return
	}
	sess.inCall = false
	sess.callMu.Unlock()
	s.emitVoicePresence(guestPeerID(sess.id), guestFingerprint(sess.name), sess.channelID, "leave")
}

// relayToGuest delivers a WebRTC signaling blob to a guest session. Called by
// RelaySignal when the destination "peer" is a guest.
func (s *Service) relayToGuest(peerID string, data []byte) error {
	id := strings.TrimPrefix(peerID, "guest:")
	s.guestMu.Lock()
	sess := s.guestByID[id]
	s.guestMu.Unlock()
	if sess == nil {
		return fmt.Errorf("app: no such guest session")
	}
	line, err := json.Marshal(guestFrame{Type: "signal", Data: json.RawMessage(data)})
	if err != nil {
		return err
	}
	select {
	case sess.send <- line:
		return nil
	default:
		return fmt.Errorf("app: guest is not keeping up")
	}
}

func (s *Service) senderLabel(m domain.Message) string {
	// A relayed guest message is signed by the host but spoken by the guest.
	if m.Kind == "guest" && m.Name != "" {
		return m.Name + " (guest)"
	}
	if n := s.ProfileName(accountFingerprintOf(m.Sender)); n != "" {
		return n
	}
	if m.Name != "" {
		return m.Name
	}
	return shortFpr(accountFingerprintOf(m.Sender))
}

// CreateGuestLink issues a browser-guest URL for a meeting. The link points
// at the rendezvous node's HTTPS side; the token and host peer ID ride the
// URL FRAGMENT, which browsers never send to the server — the rendezvous
// learns them only over the guest's TLS WebSocket.
func (s *Service) CreateGuestLink(guildID string) (string, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var kind, channelID string
	var created time.Time
	if ok {
		kind = g.Kind
		created = g.Created
		if len(g.Channels) > 0 {
			channelID = g.Channels[0].ID
		}
	}
	s.mu.RUnlock()
	if !ok || kind != "meeting" || channelID == "" {
		return "", fmt.Errorf("app: guest links are for instant meetings")
	}

	base := s.guestGatewayBase()
	if base == "" {
		return "", fmt.Errorf("app: guest links need a rendezvous server (Settings → Connection)")
	}

	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	s.guestMu.Lock()
	s.guestTokens[token] = guestToken{
		guildID: guildID, channelID: channelID, expires: created.Add(meetingTTL),
	}
	s.guestMu.Unlock()
	return fmt.Sprintf("%s/guest#h=%s&t=%s", base, s.host.PeerID(), token), nil
}

// guestGatewayBase derives the rendezvous HTTPS origin from the first
// dns-named bootstrap address ("" when there is none). CONCORD_GUEST_BASE
// overrides it — for self-hosters whose gateway sits on another host/port,
// and for local testing.
func (s *Service) guestGatewayBase() string {
	if b := strings.TrimRight(os.Getenv("CONCORD_GUEST_BASE"), "/"); b != "" {
		return b
	}
	// Saved settings first, then the env override that can supply bootstrap
	// without ever touching netconfig (CONCORD_BOOTSTRAP) — otherwise an
	// env-configured peer could never mint a guest link.
	addrs := LoadNetConfig(s.dataDir).Bootstrap
	if env := os.Getenv("CONCORD_BOOTSTRAP"); env != "" {
		addrs = append(addrs, strings.Split(env, ",")...)
	}
	for _, b := range addrs {
		fields := strings.Split(b, "/")
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "dns" || fields[i] == "dns4" || fields[i] == "dns6" {
				return "https://" + fields[i+1]
			}
		}
	}
	return ""
}

// serveGuest runs one guest's whole visit on the relayed stream.
func (s *Service) serveGuest(conn io.ReadWriteCloser) {
	defer conn.Close()
	r := bufio.NewReaderSize(conn, 64<<10)

	// Hello: token + chosen display name, within a short deadline-ish budget.
	line, err := r.ReadBytes('\n')
	if err != nil || len(line) > 8<<10 {
		return
	}
	var hello guestFrame
	if json.Unmarshal(line, &hello) != nil || hello.Type != "hello" {
		return
	}
	s.guestMu.Lock()
	tok, ok := s.guestTokens[hello.Token]
	s.guestMu.Unlock()
	if !ok || time.Now().After(tok.expires) {
		writeGuestFrame(conn, guestFrame{Type: "end", Reason: "This meeting link is no longer valid."})
		return
	}
	s.mu.RLock()
	g, alive := s.guilds[tok.guildID]
	meetingName := ""
	if alive {
		meetingName = g.Name
	}
	s.mu.RUnlock()
	if !alive {
		writeGuestFrame(conn, guestFrame{Type: "end", Reason: "This meeting has ended."})
		return
	}

	name := sanitizeGuestName(hello.Name)
	sess := &guestSession{
		id:        newGuestSessionID(),
		name:      name,
		channelID: tok.channelID,
		send:      make(chan []byte, 64),
	}
	s.guestMu.Lock()
	if len(s.guestSessions[tok.channelID]) >= maxGuestsPerMeeting {
		s.guestMu.Unlock()
		writeGuestFrame(conn, guestFrame{Type: "end", Reason: "This meeting is full."})
		return
	}
	s.guestSessions[tok.channelID] = append(s.guestSessions[tok.channelID], sess)
	s.guestByID[sess.id] = sess
	s.guestMu.Unlock()
	defer func() {
		s.guestMu.Lock()
		list := s.guestSessions[tok.channelID]
		for i, gs := range list {
			if gs == sess {
				s.guestSessions[tok.channelID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		delete(s.guestByID, sess.id)
		s.guestMu.Unlock()
		// A guest who drops mid-call must leave the call too, or the app keeps a
		// dead RTCPeerConnection and a ghost in the roster.
		s.guestLeaveCall(sess)
		s.sendSystem(tok.channelID, fmt.Sprintf("👤 %s (guest) left", name))
	}()

	// Welcome + recent history so the guest has context. `Channel` tells the
	// guest which voice room to label its signaling with.
	writeGuestFrame(conn, guestFrame{
		Type: "welcome", Meeting: meetingName, Name: name, Channel: tok.channelID,
	})
	if msgs, err := s.store.Messages(tok.channelID, guestHistoryCount); err == nil {
		for _, m := range msgs {
			if m.Deleted || (m.Kind != "" && m.Kind != "system" && m.Kind != "guest") {
				continue
			}
			typ := "msg"
			if m.Kind == "system" {
				typ = "sys"
			}
			writeGuestFrame(conn, guestFrame{
				Type: typ, From: s.senderLabel(m), Content: m.Content,
				Sent: m.Sent.UTC().Format(time.RFC3339),
			})
		}
	}
	s.sendSystem(tok.channelID, fmt.Sprintf("👤 %s joined as a guest (via your meeting link)", name))

	// Writer: queued frames → stream.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case line := <-sess.send:
				if _, err := conn.Write(append(line, '\n')); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()
	defer close(done)

	// Reader: guest messages, token-bucket rate limited. Signaling gets its own,
	// looser bucket — it's machine traffic, not typing.
	budget := float64(guestBurst)
	last := time.Now()
	sigBudget := float64(guestSignalBurst)
	sigLast := time.Now()
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		if len(line) > maxGuestFrameBytes {
			continue
		}
		var f guestFrame
		if json.Unmarshal(line, &f) != nil {
			continue
		}
		// Call control + WebRTC signaling. These are relayed to the app's voice
		// mesh, which treats this guest as the peer "guest:<id>". They're small
		// and bursty (ICE trickles), so they bypass the chat rate limit but are
		// bounded by the frame-size cap above.
		switch f.Type {
		case "call":
			if f.Action == "join" {
				s.guestJoinCall(sess)
			} else {
				s.guestLeaveCall(sess)
			}
			continue
		case "signal":
			now := time.Now()
			sigBudget += now.Sub(sigLast).Seconds() * guestSignalRefill
			if sigBudget > guestSignalBurst {
				sigBudget = guestSignalBurst
			}
			sigLast = now
			if sigBudget < 1 {
				continue // flooding: drop, don't disconnect (ICE is retry-tolerant)
			}
			sigBudget--
			if len(f.Data) > 0 && sess.callActive() {
				s.emitVoiceSignal(guestPeerID(sess.id), f.Data)
			}
			continue
		case "msg":
		default:
			continue
		}
		content := strings.TrimSpace(f.Content)
		if content == "" {
			continue
		}
		if len(content) > maxGuestMsgBytes {
			content = content[:maxGuestMsgBytes]
		}
		now := time.Now()
		budget += now.Sub(last).Seconds() / guestRefillEach.Seconds()
		if budget > guestBurst {
			budget = guestBurst
		}
		last = now
		if budget < 1 {
			writeGuestFrame(conn, guestFrame{Type: "info", Reason: "Slow down a little 🙂"})
			continue
		}
		budget--
		// The meeting may have been ended/left while the guest was connected.
		s.mu.RLock()
		_, stillAlive := s.guilds[tok.guildID]
		s.mu.RUnlock()
		if !stillAlive {
			writeGuestFrame(conn, guestFrame{Type: "end", Reason: "This meeting has ended."})
			return
		}
		// Relayed under our signature, but authored by THEM: kind "guest" + their
		// name, so clients give them their own bubble instead of tucking their
		// words under the host's like a subheading.
		if _, err := s.sendAs(tok.channelID, content, "guest", "", sess.name); err != nil {
			writeGuestFrame(conn, guestFrame{Type: "info", Reason: "Message didn't send — try again."})
		}
	}
}

func writeGuestFrame(w io.Writer, f guestFrame) {
	line, _ := json.Marshal(f)
	_, _ = w.Write(append(line, '\n'))
}

// sanitizeGuestName bounds a guest's chosen name and strips characters that
// would let them impersonate markup or members.
func sanitizeGuestName(n string) string {
	n = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '`', '*', '_', '|', '@', '#':
			return -1
		}
		return r
	}, strings.TrimSpace(n))
	if n == "" {
		n = "Guest"
	}
	if len(n) > maxGuestNameBytes {
		n = n[:maxGuestNameBytes]
	}
	return n
}
