package app

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
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

// guestSession is one live browser guest.
type guestSession struct {
	name string
	send chan []byte // JSON lines queued to the guest (dropped when full)
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
}

// initGuests wires the stream handler and the message tap. Called from Start.
func (s *Service) initGuests() {
	s.guestMu.Lock()
	if s.guestTokens == nil {
		s.guestTokens = map[string]guestToken{}
		s.guestSessions = map[string][]*guestSession{} // channelID -> sessions
	}
	s.guestMu.Unlock()
	s.host.HandleGuestSessions(func(conn io.ReadWriteCloser, remote peer.ID) {
		s.serveGuest(conn)
	})
	// Forward every message in a guest-attended channel to its guests.
	s.OnMessage(func(m domain.Message) {
		if m.Deleted || (m.Kind != "" && m.Kind != "system") {
			return
		}
		s.guestMu.Lock()
		sessions := append([]*guestSession(nil), s.guestSessions[m.ChannelID]...)
		s.guestMu.Unlock()
		if len(sessions) == 0 {
			return
		}
		line, _ := json.Marshal(guestFrame{
			Type: "msg", From: s.senderLabel(m), Content: m.Content,
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

func (s *Service) senderLabel(m domain.Message) string {
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
	sess := &guestSession{name: name, send: make(chan []byte, 64)}
	s.guestMu.Lock()
	if len(s.guestSessions[tok.channelID]) >= maxGuestsPerMeeting {
		s.guestMu.Unlock()
		writeGuestFrame(conn, guestFrame{Type: "end", Reason: "This meeting is full."})
		return
	}
	s.guestSessions[tok.channelID] = append(s.guestSessions[tok.channelID], sess)
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
		s.guestMu.Unlock()
		s.sendSystem(tok.channelID, fmt.Sprintf("👤 %s (guest) left", name))
	}()

	// Welcome + recent history so the guest has context.
	writeGuestFrame(conn, guestFrame{Type: "welcome", Meeting: meetingName, Name: name})
	if msgs, err := s.store.Messages(tok.channelID, guestHistoryCount); err == nil {
		for _, m := range msgs {
			if m.Deleted || (m.Kind != "" && m.Kind != "system") {
				continue
			}
			writeGuestFrame(conn, guestFrame{
				Type: "msg", From: s.senderLabel(m), Content: m.Content,
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

	// Reader: guest messages, token-bucket rate limited.
	budget := float64(guestBurst)
	last := time.Now()
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		if len(line) > maxGuestMsgBytes+512 {
			continue
		}
		var f guestFrame
		if json.Unmarshal(line, &f) != nil || f.Type != "msg" {
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
		if _, err := s.SendMessage(tok.channelID, "👤 **"+sess.name+"** · "+content, ""); err != nil {
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
