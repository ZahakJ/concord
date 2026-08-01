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
	// The hello frame (token + name) is small; cap it tight.
	maxGuestHelloBytes = 8 << 10
	// Signaling bypasses the chat rate limit (ICE trickles in bursts) but is
	// bounded on its own: enough for several renegotiations, not enough to be a
	// pipe into the app.
	guestSignalBurst  = 240
	guestSignalRefill = 12 // frames per second, replenished

	guestHistoryCount = 30
	// Rate limit: a small burst, then one message per second.
	guestBurst      = 5
	guestRefillEach = time.Second
	// Deadlines on the untrusted, explicitly-relayed stream: a short budget to
	// present a valid hello (pre-auth slowloris), then a generous idle timeout in
	// the call loop (guests sit quietly between ICE trickles / messages).
	//
	// Generous, because a guest sends nothing while they read: someone lurking in
	// the meeting chat is not a stalled connection, and hanging up on them is the
	// "second-class side door" feeling this whole path is meant to lose. The
	// gateway keeps the WebSocket itself alive with ping/pong, so a silent guest
	// is still a guest we can hear from the moment they type.
	guestHelloTimeout = 20 * time.Second
	guestIdleTimeout  = 30 * time.Minute
	// Cap on one write to a guest, so a stalled socket can't wedge whoever is
	// writing to it (see writeLine).
	guestWriteTimeout = 10 * time.Second
	// A guest who is IN A CALL may legitimately send nothing for a long time —
	// ICE settles and then the media path carries everything, invisibly to us.
	// Reaping them on the chat-sized idle timeout would drop people out of a
	// meeting mid-sentence every ten minutes. A dropped socket is still noticed
	// promptly: the gateway resets the stream when the WebSocket dies, which
	// surfaces as a read error regardless of the deadline.
	guestCallIdleTimeout = 3 * time.Hour

	// Knocking (a locked meeting). The wait is bounded so a guest at a door
	// nobody is answering gets told so instead of hanging; it stays comfortably
	// under the gateway's own 10-minute idle timeout on the WebSocket, which
	// would otherwise close first and look like a crash.
	guestKnockTimeout = 5 * time.Minute
	// How often a pending knock is re-announced. The host's knock list only
	// shows knocks for a call they are IN, so a host who joins after the guest
	// arrives must still learn about them; the same tick's write to the guest is
	// how a guest who closed the tab while waiting gets noticed.
	guestKnockRemind = 15 * time.Second
	// How long one "lock" announcement keeps the guest door shut. The front end
	// re-announces a lock every 3s while it is on, so this is a lease: a host who
	// crashes, reloads, or quits stops renewing and the door opens again, instead
	// of leaving a lock only a dead client could lift.
	guestDoorLease = 12 * time.Second
)

// errGuestFrameOversize marks a frame that ran past its cap without a newline —
// treated as hostile and drops the visit.
var errGuestFrameOversize = fmt.Errorf("app: guest frame exceeds cap")

// readGuestLine reads one newline-delimited frame, capping the accumulated bytes
// at max. bufio.Reader.ReadBytes would buffer an entire unbounded, newline-less
// stream before any size check — an OOM lever for any peer that can reach the
// guest handler. Reading a byte at a time off the buffered reader stays cheap
// while bounding the allocation hard.
func readGuestLine(r *bufio.Reader, max int) ([]byte, error) {
	buf := make([]byte, 0, 512)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return buf, err
		}
		if b == '\n' {
			return buf, nil
		}
		if len(buf) >= max {
			return nil, errGuestFrameOversize
		}
		buf = append(buf, b)
	}
}

// setGuestReadDeadline applies a read deadline when the underlying stream
// supports one (libp2p streams do); a no-op otherwise.
func setGuestReadDeadline(conn io.ReadWriteCloser, t time.Time) {
	if d, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = d.SetReadDeadline(t)
	}
}

// guestToken is one issued guest link, valid for the meeting's lifetime.
// Persisted (guestTokensKey): a link the host has already sent to someone must
// keep working after they close and reopen the app — a 7-day link that dies the
// first time Concord restarts is not a 7-day link.
type guestToken struct {
	GuildID   string    `json:"guild"`
	ChannelID string    `json:"channel"`
	Expires   time.Time `json:"expires"`
}

// guestTokensKey is the settings key holding the issued links. The tokens are
// bearer secrets; the store they live in is the same encrypted database as the
// message history and MLS state.
const guestTokensKey = "guest.links"

// guestSession is one live browser guest. `id` makes it addressable as a voice
// peer ("guest:<id>"): the WebRTC mesh in the app treats a guest exactly like
// any other participant, and RelaySignal routes that prefix down this session's
// stream instead of over libp2p. Media itself is direct browser↔app (P2P,
// DTLS-SRTP) — it never touches the rendezvous.
type guestSession struct {
	id        string
	name      string
	fpr       string // guestFingerprint(name, id) — how the host's UI names them
	channelID string
	conn      io.ReadWriteCloser
	send      chan []byte // JSON lines queued to the guest (dropped when full)

	// One mutex over every write to the stream: the queue-draining writer, the
	// door's status pings and an eviction notice can all fire at once, and two
	// interleaved writes would splice two JSON lines into one unparseable frame.
	writeMu   sync.Mutex
	closeOnce sync.Once

	// gate carries the host's verdict on a KNOCKING guest exactly once: "" for
	// admitted, otherwise the refusal to show them. Buffered so the admitting
	// goroutine (an RPC call) never blocks on a guest who has already gone.
	gate     chan string
	gateOnce sync.Once

	// admitted gates EVERYTHING a guest can see or hear. A knocking guest is
	// registered (so they count against the meeting's cap) but inert: no chat
	// fan-out, no history, no roster, no signalling, no call.
	admitMu  sync.Mutex
	admitted bool

	callMu sync.Mutex
	inCall bool
}

// write emits one frame to the guest, newline-terminated. EVERY frame is: the
// gateway forwards whole lines and the guest page parses only complete ones, so
// a frame without its '\n' is a frame the browser waits on forever.
func (g *guestSession) write(f guestFrame) error {
	line, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return g.writeLine(line)
}

func (g *guestSession) writeLine(line []byte) error {
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	// Bounded, because a host clicking "remove this guest" writes from an RPC
	// goroutine: a guest whose socket has stopped draining must not be able to
	// wedge the caller (or hold writeMu against everyone else) indefinitely.
	if d, ok := g.conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = d.SetWriteDeadline(time.Now().Add(guestWriteTimeout))
	}
	_, err := g.conn.Write(append(line, '\n'))
	return err
}

// end tells the guest why their visit is over and closes the stream. Closing is
// what unblocks the read loop, so this is also how another goroutine (a host
// kicking someone) ends a session it does not own.
func (g *guestSession) end(reason string) {
	g.closeOnce.Do(func() {
		if reason != "" {
			_ = g.write(guestFrame{Type: "end", Reason: reason})
		}
		_ = g.conn.Close()
	})
}

// decide delivers the host's door verdict. Idempotent: a host who clicks Admit
// twice, or Admit then Refuse, gets one outcome.
func (g *guestSession) decide(verdict string) {
	g.gateOnce.Do(func() { g.gate <- verdict })
}

func (g *guestSession) isAdmitted() bool {
	g.admitMu.Lock()
	defer g.admitMu.Unlock()
	return g.admitted
}

func (g *guestSession) admit() {
	g.admitMu.Lock()
	g.admitted = true
	g.admitMu.Unlock()
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
		s.guestDoor = map[string]time.Time{}           // channelID -> lock lease
	}
	s.guestMu.Unlock()
	s.loadGuestTokens()
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
			if !g.isAdmitted() {
				continue // knocking at a locked door: they hear nothing said inside
			}
			select {
			case g.send <- line:
			default: // guest too slow: drop rather than block the app
			}
		}
	})
}

// loadGuestTokens restores issued guest links, dropping any whose meeting is
// gone (the startup sweep has run) or whose lifetime has passed.
func (s *Service) loadGuestTokens() {
	raw, err := s.store.GetSetting(guestTokensKey)
	if err != nil || raw == "" {
		return
	}
	var saved map[string]guestToken
	if json.Unmarshal([]byte(raw), &saved) != nil {
		return
	}
	now := time.Now()
	s.mu.RLock()
	alive := make(map[string]bool, len(saved))
	for _, tok := range saved {
		if _, ok := s.guilds[tok.GuildID]; ok {
			alive[tok.GuildID] = true
		}
	}
	s.mu.RUnlock()
	s.guestMu.Lock()
	for token, tok := range saved {
		if alive[tok.GuildID] && now.Before(tok.Expires) {
			s.guestTokens[token] = tok
		}
	}
	s.guestMu.Unlock()
	s.saveGuestTokens()
}

func (s *Service) saveGuestTokens() {
	s.guestMu.Lock()
	out := make(map[string]guestToken, len(s.guestTokens))
	for token, tok := range s.guestTokens {
		out[token] = tok
	}
	s.guestMu.Unlock()
	blob, err := json.Marshal(out)
	if err != nil {
		return
	}
	_ = s.store.SetSetting(guestTokensKey, string(blob))
}

// guestPeerID is how a browser guest appears to the voice mesh. The app's
// RelaySignal recognizes this prefix and writes down the guest's stream instead
// of dialing libp2p, so the mesh needs no idea guests exist.
func guestPeerID(id string) string { return "guest:" + id }

// guestFingerprint carries the guest's NAME where a member's fingerprint would
// go, so the call roster can label them without inventing an identity for
// someone who has none. (A guest is not authenticated — that's the whole point
// of a guest.)
//
// The session id is appended after a '#' because the host's UI keys knocks and
// moderation by FINGERPRINT: two strangers who both type "Alice" must be two
// separate decisions, not one Admit that quietly lets both of them in. The
// separator is safe — sanitizeGuestName strips '#' from anything a guest sends.
func guestFingerprint(name, id string) string { return "guest:" + name + "#" + id }

// guestDoorLocked reports whether arrivals at this meeting must knock. See
// guestDoorLease: a lock is a lease the host renews, not a latch.
func (s *Service) guestDoorLocked(channelID string) bool {
	s.guestMu.Lock()
	defer s.guestMu.Unlock()
	return time.Now().Before(s.guestDoor[channelID])
}

// noteGuestDoor records a lock/unlock the USER of this node performed. Only
// local intent counts: a guest is relayed through this node, reads this node's
// decrypted copy of the meeting and is admitted by this node — so who gets in is
// this host's call, not something a remote member can flip from a distance.
func (s *Service) noteGuestDoor(channelID string, locked bool) {
	s.guestMu.Lock()
	if locked {
		s.guestDoor[channelID] = time.Now().Add(guestDoorLease)
	} else {
		delete(s.guestDoor, channelID)
	}
	s.guestMu.Unlock()
}

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
	s.emitVoicePresence(guestPeerID(sess.id), sess.fpr, sess.channelID, "join", "", "")
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
				s.emitVoicePresence(guestPeerID(sess.id), sess.fpr, sess.channelID, "join", "", "")
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
	s.emitVoicePresence(guestPeerID(sess.id), sess.fpr, sess.channelID, "leave", "", "")
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
	// Belt to the braces of the knock: nothing emits presence for a guest still
	// at the door, so the mesh has no reason to signal them — but signalling is
	// the one frame that would hand a stranger a media path, so refuse it here
	// too rather than trust every future caller to check.
	if !sess.isAdmitted() {
		return fmt.Errorf("app: guest has not been admitted")
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
//
// lifetimeHours picks how long the link (and with it the meeting room) lives,
// from the fixed menu in meetingLifetimes. Zero means "leave the lifetime
// alone" — what an older caller that knows nothing about lifetimes sends, and
// what keeps the historic 24h-from-creation behaviour intact.
//
// Called again for the same meeting it returns the SAME url with a new
// lifetime, rather than a second token: changing your mind about the duration
// must not silently kill the link you already pasted into a calendar invite.
func (s *Service) CreateGuestLink(guildID string, lifetimeHours int) (string, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var kind, channelID string
	if ok {
		kind = g.Kind
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

	if lifetimeHours != 0 {
		d, valid := meetingLifetime(lifetimeHours)
		if !valid {
			return "", fmt.Errorf("app: %dh is not one of the offered link lifetimes", lifetimeHours)
		}
		// From NOW, not from when the meeting started: "this link works for a
		// week" is a promise about the week ahead of whoever is asked to use it.
		s.setMeetingExpiry(guildID, time.Now().Add(d))
	}
	expires := s.meetingExpiry(guildID)

	s.guestMu.Lock()
	token := ""
	for t, tok := range s.guestTokens {
		if tok.GuildID == guildID {
			token = t
			break
		}
	}
	if token == "" {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			s.guestMu.Unlock()
			return "", err
		}
		token = base64.RawURLEncoding.EncodeToString(raw)
	}
	s.guestTokens[token] = guestToken{GuildID: guildID, ChannelID: channelID, Expires: expires}
	s.guestMu.Unlock()
	s.saveGuestTokens()
	return fmt.Sprintf("%s/guest#h=%s&t=%s", base, s.host.PeerID(), token), nil
}

// MeetingExpiry reports when a meeting (and any guest link into it) stops
// working, as a Unix millisecond timestamp; 0 when the guild is not a meeting.
// The UI that mints the link shows this, so the lifetime is never a mystery.
func (s *Service) MeetingExpiry(guildID string) int64 {
	at := s.meetingExpiry(guildID)
	if at.IsZero() {
		return 0
	}
	return at.UnixMilli()
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

	// Hello: token + chosen display name, within a short deadline and a tight cap.
	setGuestReadDeadline(conn, time.Now().Add(guestHelloTimeout))
	line, err := readGuestLine(r, maxGuestHelloBytes)
	if err != nil {
		return
	}
	var hello guestFrame
	if json.Unmarshal(line, &hello) != nil || hello.Type != "hello" {
		return
	}
	s.guestMu.Lock()
	tok, ok := s.guestTokens[hello.Token]
	s.guestMu.Unlock()
	// Two clocks have to agree for a link to be live: the token's own expiry and
	// the meeting's. They are set together, but a lifetime shortened after the
	// link went out (or a room whose expiry passed on a machine that never
	// restarted, so the startup sweep never ran) must close the door too.
	if !ok || time.Now().After(tok.Expires) || time.Now().After(s.meetingExpiry(tok.GuildID)) {
		writeGuestFrame(conn, guestFrame{Type: "end", Reason: "This meeting link is no longer valid."})
		return
	}
	s.mu.RLock()
	g, alive := s.guilds[tok.GuildID]
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
	id := newGuestSessionID()
	sess := &guestSession{
		id:        id,
		name:      name,
		fpr:       guestFingerprint(name, id),
		channelID: tok.ChannelID,
		conn:      conn,
		send:      make(chan []byte, 64),
		gate:      make(chan string, 1),
	}
	s.guestMu.Lock()
	if len(s.guestSessions[tok.ChannelID]) >= maxGuestsPerMeeting {
		s.guestMu.Unlock()
		writeGuestFrame(conn, guestFrame{Type: "end", Reason: "This meeting is full."})
		return
	}
	// Knockers are registered as well as joiners: they hold a socket and a slot,
	// so they must count against the cap and be reachable by the host's verdict.
	s.guestSessions[tok.ChannelID] = append(s.guestSessions[tok.ChannelID], sess)
	s.guestByID[sess.id] = sess
	s.guestMu.Unlock()
	defer func() {
		s.guestMu.Lock()
		list := s.guestSessions[tok.ChannelID]
		for i, gs := range list {
			if gs == sess {
				s.guestSessions[tok.ChannelID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		delete(s.guestByID, sess.id)
		s.guestMu.Unlock()
		// A guest who drops mid-call must leave the call too, or the app keeps a
		// dead RTCPeerConnection and a ghost in the roster.
		s.guestLeaveCall(sess)
		// Someone who never got past the door was never in the room, so no
		// arrival/departure notice belongs in the transcript.
		if sess.isAdmitted() {
			s.sendSystem(tok.ChannelID, fmt.Sprintf("👤 %s (guest) left", name))
		}
	}()

	// Writer: queued frames → stream.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case line := <-sess.send:
				if err := sess.writeLine(line); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()
	defer close(done)

	// The door. A locked meeting turns arrival into a KNOCK: the host sees who is
	// asking and decides. Until they do, this session is inert — it is not in the
	// chat fan-out, gets no history, no roster, no signalling and no call.
	// A room minted by the public booking page ALWAYS knocks: its link went to a
	// stranger on the open web, so auto-admitting on the unlocked default would
	// turn "book a demo" into "walk straight into my client". A guest-opened
	// calendar event knocks BY DEFAULT for the same reason — its link is meant
	// to be forwarded — unless the host explicitly chose an open door.
	if s.guestDoorLocked(tok.ChannelID) || s.isBookingMeeting(tok.GuildID) || s.eventGuestKnocks(tok.GuildID) {
		admitted, reason := s.knockAtDoor(sess, meetingName)
		if !admitted {
			sess.end(reason)
			return
		}
		// Anything they typed at the door was said to a closed door, so drop what
		// has already arrived rather than replaying it into the room.
		//
		// This is best effort BY CONSTRUCTION, and worth being precise about
		// because the loose version of this sentence is the kind that gets
		// trusted later: Buffered() reports only what bufio has already pulled
		// in. Bytes still sitting in the socket or the libp2p stream are read
		// normally a moment later, so a guest who pre-queues a line at the door
		// can have it land just after admission. That is harmless — they are
		// admitted by then and could send the same line a millisecond later — and
		// it is NOT what stops an unadmitted guest being heard. That comes from
		// knockAtDoor running before this loop exists, so no guest verb is parsed
		// at all until they are in.
		if n := r.Buffered(); n > 0 {
			_, _ = r.Discard(n)
		}
	}
	sess.admit()

	// Welcome + recent history so the guest has context. `Channel` tells the
	// guest which voice room to label its signaling with.
	_ = sess.write(guestFrame{
		Type: "welcome", Meeting: meetingName, Name: name, Channel: tok.ChannelID,
	})
	if msgs, err := s.store.Messages(tok.ChannelID, guestHistoryCount); err == nil {
		for _, m := range msgs {
			if m.Deleted || (m.Kind != "" && m.Kind != "system" && m.Kind != "guest") {
				continue
			}
			typ := "msg"
			if m.Kind == "system" {
				typ = "sys"
			}
			_ = sess.write(guestFrame{
				Type: typ, From: s.senderLabel(m), Content: m.Content,
				Sent: m.Sent.UTC().Format(time.RFC3339),
			})
		}
	}
	s.sendSystem(tok.ChannelID, fmt.Sprintf("👤 %s joined as a guest (via your meeting link)", name))

	// Reader: guest messages, token-bucket rate limited. Signaling gets its own,
	// looser bucket — it's machine traffic, not typing.
	budget := float64(guestBurst)
	last := time.Now()
	sigBudget := float64(guestSignalBurst)
	sigLast := time.Now()
	for {
		idle := guestIdleTimeout
		if sess.callActive() {
			idle = guestCallIdleTimeout
		}
		setGuestReadDeadline(conn, time.Now().Add(idle))
		line, err := readGuestLine(r, maxGuestFrameBytes)
		if err != nil {
			return
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
			_ = sess.write(guestFrame{Type: "info", Reason: "Slow down a little 🙂"})
			continue
		}
		budget--
		// The meeting may have been ended/left while the guest was connected.
		s.mu.RLock()
		_, stillAlive := s.guilds[tok.GuildID]
		s.mu.RUnlock()
		if !stillAlive {
			sess.end("This meeting has ended.")
			return
		}
		// Relayed under our signature, but authored by THEM: kind "guest" + their
		// name, so clients give them their own bubble instead of tucking their
		// words under the host's like a subheading.
		if _, err := s.sendAs(tok.ChannelID, content, "guest", "", sess.name); err != nil {
			_ = sess.write(guestFrame{Type: "info", Reason: "Message didn't send — try again."})
		}
	}
}

// knockAtDoor holds a guest at a locked meeting's door until the host decides.
// Returns (true, "") on admission, (false, reason) with something to show them
// otherwise, and (false, "") when the guest gave up and there is nobody left to
// tell.
//
// Nothing the guest sends is read here, and nothing about the meeting is sent
// besides its name (which is what their link was for): the knock carries the
// name they typed and nothing else, because that is the only thing an
// unauthenticated stranger can offer.
func (s *Service) knockAtDoor(sess *guestSession, meetingName string) (bool, string) {
	waiting := guestFrame{
		Type: "waiting", Meeting: meetingName, Name: sess.name,
		Reason: "This meeting is locked. The host has been asked to let you in…",
	}
	if err := sess.write(waiting); err != nil {
		return false, ""
	}
	s.emitVoicePresence(guestPeerID(sess.id), sess.fpr, sess.channelID, "knock", "", "")
	defer func() {
		// Whatever the outcome, the knock is over: clear it from the host's list
		// rather than leaving a stranger apparently waiting forever at a door they
		// walked away from.
		if !sess.isAdmitted() {
			s.emitVoicePresence(guestPeerID(sess.id), sess.fpr, sess.channelID, "unknock", "", "")
		}
	}()

	giveUp := time.NewTimer(guestKnockTimeout)
	defer giveUp.Stop()
	remind := time.NewTicker(guestKnockRemind)
	defer remind.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return false, "This meeting has ended."
		case verdict := <-sess.gate:
			if verdict == "" {
				return true, ""
			}
			return false, verdict
		case <-giveUp.C:
			return false, "Nobody answered — the host may be away. Try the link again later."
		case <-remind.C:
			// Re-announce (the host may only just have joined the call, and their
			// knock list only shows knocks for a call they are in) and, by writing,
			// find out whether the guest is still on the other end at all.
			if err := sess.write(waiting); err != nil {
				return false, ""
			}
			s.emitVoicePresence(guestPeerID(sess.id), sess.fpr, sess.channelID, "knock", "", "")
		}
	}
}

// decideGuest applies a host's call-control action to a browser guest. The
// target is a guest FINGERPRINT ("guest:<name>#<session>"), which is how the
// host's UI names them; it identifies one session, so admitting one "Alice" does
// not admit a second stranger who typed the same name.
//
// This never leaves the node. A guest exists only as a relayed socket held HERE,
// so there is nothing to gossip and nobody else who could act on it.
func (s *Service) decideGuest(channelID, action, target string) {
	s.guestMu.Lock()
	var sess *guestSession
	for _, gs := range s.guestSessions[channelID] {
		if gs.fpr == target {
			sess = gs
			break
		}
	}
	s.guestMu.Unlock()
	if sess == nil {
		return
	}
	switch action {
	case "admit":
		sess.decide("")
	case "refuse":
		sess.decide("The host didn't let you in this time.")
	case "disconnect":
		// One verb, two situations: refusing someone at the door, or removing
		// someone already inside. Both must say so — a guest whose socket simply
		// went quiet would sit there wondering.
		if sess.isAdmitted() {
			sess.end("The host removed you from this meeting.")
			return
		}
		sess.decide("The host didn't let you in this time.")
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
