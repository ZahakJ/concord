package app

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
)

// Browser guests: someone with just the meeting link joins from a plain web
// page served by the rendezvous node — no Concord install, no account. The
// rendezvous relays the guest's WebSocket to THIS node over a libp2p stream,
// and this node is the guest's only endpoint: it decrypts the meeting for
// them and sends their messages into the MLS group under its own key,
// attributed "👤 Name (guest)". That is the deliberate trust model — the
// HOST vouches for (and can read) the guests they invite; full members keep
// end-to-end encryption among themselves.
//
// The leg between the guest's browser and this node is end-to-end encrypted
// too (guestcrypt.go, docs/dev/guest-e2e.md): after a clear hello and
// challenge every frame is a sealed record, so the rendezvous relaying it
// sees a link id, nonces, ephemeral keys and ciphertext. Guests are scoped
// hard: one meeting's one channel, chat and the call, rate-limited, dead when
// the meeting or the link expires or the host revokes the link.

const (
	maxGuestsPerMeeting = 5
	maxGuestNameBytes   = 24
	maxGuestMsgBytes    = 2000
	// A WebRTC offer/answer is far bigger than a chat line (a few KB of SDP,
	// more once video codecs are in it), and a sealed record adds base64 and
	// padding on top. Frames are capped generously; the chat CONTENT limit
	// above still applies to messages. The gateway caps at the same size.
	maxGuestFrameBytes = 64 << 10
	// The hello frame (link id, nonce, public key) is small; cap it tight.
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
	// complete the handshake (pre-auth slowloris), then a generous idle timeout
	// in the call loop (guests sit quietly between ICE trickles / messages).
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
	// How often a live guest session re-checks the meeting's expiry. The room
	// and its link share one lifetime and the UI promises both stop on the same
	// date, but that was only ever enforced at the DOOR: the sweep that deletes
	// an expired meeting runs at startup, so a host who leaves Concord open for
	// a week never reaches it, and a guest who arrived a minute before the
	// deadline sat in an expired room for as long as they cared to. Sleeping to
	// the expiry and no further would miss a lifetime the host SHORTENS while
	// someone is inside, so the wait is capped and the expiry re-read.
	guestExpiryPoll = 10 * time.Second
	// How long one "lock" announcement keeps the guest door shut. The front end
	// re-announces a lock every 3s while it is on, so this is a lease: a host who
	// crashes, reloads, or quits stops renewing and the door opens again, instead
	// of leaving a lock only a dead client could lift.
	guestDoorLease = 12 * time.Second

	// Limited use: how many distinct guests (gids) one link admits. A booking
	// room's link went to one stranger, who may bring one colleague.
	maxGuestsPerLink        = 5
	maxGuestsPerBookingLink = 2
	// Handshakes that have sent hello but not yet proved the link: bounded per
	// link and per node, and not counted against the five-guest cap, so a flood
	// of half-open visits cannot hold a meeting's seats.
	maxGuestHandshakesPerLink = 8
	maxGuestHandshakes        = 64
	maxGuestGidBytes          = 64
)

// What a guest is told when they are turned away after proving the link.
// Failures before that (wrong key, unknown, expired or revoked link) are a
// silent close, so the operator cannot tell one from another.
const (
	guestEndRevoked = "The host ended guest access to this meeting."
	guestEndRemoved = "The host removed you from this meeting."
	guestEndUsedUp  = "This link has already been used by as many guests as it allows."
	guestEndFull    = "This meeting is full."
	guestEndEnded   = "This meeting has ended."
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

// guestToken is one issued guest link, filed under its link id and valid for
// the meeting's lifetime. Persisted (guestTokensKey): a link the host has
// already sent to someone must keep working after they close and reopen the
// app — a 7-day link that dies the first time Concord restarts is not a 7-day
// link.
type guestToken struct {
	GuildID   string    `json:"guild"`
	ChannelID string    `json:"channel"`
	Expires   time.Time `json:"expires"`
	// S is the link secret (base64url), the one thing in the URL fragment the
	// session keys come from. A saved link without it is from before the guest
	// leg was encrypted and is dropped on load.
	S string `json:"s"`
	// History lets admitted guests read the last guestHistoryCount messages.
	// Off unless the host turns it on for this link.
	History bool `json:"history,omitempty"`
	// MaxGuests distinct gids may use the link; Guests are the ones that have,
	// Banned the ones the host removed.
	MaxGuests int      `json:"maxGuests"`
	Guests    []string `json:"guests,omitempty"`
	Banned    []string `json:"banned,omitempty"`
}

func (t guestToken) maxGuests() int {
	if t.MaxGuests <= 0 {
		return maxGuestsPerLink
	}
	return t.MaxGuests
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// guestTokensKey is the settings key holding the issued links, each with its
// secret S. Settings are NOT encrypted at rest (docs/manual/privacy.md says so
// and names guest links), so a device thief can read S and join that meeting
// as a guest until the link expires or is revoked.
const guestTokensKey = "guest.links"

// guestHandshake is a visit between hello and a proven join. Registered so a
// revoke can cut it off and so the pre-join caps can count it.
type guestHandshake struct {
	conn io.ReadWriteCloser
}

// guestSession is one live browser guest. `id` makes it addressable as a voice
// peer ("guest:<id>"): the WebRTC mesh in the app treats a guest exactly like
// any other participant, and RelaySignal routes that prefix down this session's
// stream instead of over libp2p. Media itself is DTLS-SRTP between the browser
// and this app, direct or through the TURN relay, never through the gateway.
type guestSession struct {
	id        string
	name      string
	fpr       string // guestFingerprint(name, id) — how the host's UI names them
	channelID string
	linkID    string
	gid       string
	conn      io.ReadWriteCloser
	send      chan []byte // JSON lines queued to the guest (dropped when full)

	// One mutex over every write to the stream: the queue-draining writer, the
	// door's status pings and an eviction notice can all fire at once, and two
	// interleaved writes would splice two lines into one unparseable frame. It
	// also orders the sealing counter: a record is sealed and written under the
	// same lock, so counters reach the wire in order.
	writeMu   sync.Mutex
	tx        *guestCipher
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

// write emits one frame to the guest as a sealed record, newline-terminated.
// EVERY frame is: the gateway forwards whole lines and the guest page parses
// only complete ones, so a frame without its '\n' is a frame the browser waits
// on forever.
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
	_, err := g.conn.Write(append(g.tx.seal(line), '\n'))
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

// guestHello is the one clear frame a guest sends.
type guestHello struct {
	Type string `json:"type"`
	V    int    `json:"v"`
	Link string `json:"link"`
	Gn   string `json:"gn"`
	Gpk  string `json:"gpk"`
}

// guestChal is the one clear frame the host sends, to every hello alike.
type guestChal struct {
	Type string `json:"type"`
	Hn   string `json:"hn"`
	Hpk  string `json:"hpk"`
}

// guestFrame is the plaintext of a sealed record, both ways.
type guestFrame struct {
	Type string `json:"type"`
	// Token is not part of the wire: the Go test harness passes the link secret
	// to its adapter in it, the way the page reads it from the fragment.
	Token string `json:"token,omitempty"`
	Name  string `json:"name,omitempty"`
	// Gid is the guest page's random per-link id, sent in the sealed join: it
	// is how a link counts distinct guests and how a removed guest stays out.
	Gid     string `json:"gid,omitempty"`
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

// guestRecordBody returns the ciphertext of a sealed record line, or false
// for anything else. After the challenge nothing else is accepted: a clear
// frame there can only have been injected by whoever relays the stream.
func guestRecordBody(line []byte) (string, bool) {
	var f struct {
		Type string `json:"type"`
		C    string `json:"c"`
	}
	if json.Unmarshal(line, &f) != nil || f.Type != "e" || f.C == "" {
		return "", false
	}
	return f.C, true
}

// initGuests wires the stream handler and the message tap. Called from Start.
func (s *Service) initGuests() {
	s.guestMu.Lock()
	if s.guestTokens == nil {
		s.guestTokens = map[string]guestToken{}               // link id -> link
		s.guestSessions = map[string][]*guestSession{}        // channelID -> sessions
		s.guestByID = map[string]*guestSession{}              // session id -> session
		s.guestDoor = map[string]time.Time{}                  // channelID -> lock lease
		s.guestShakes = map[string]map[*guestHandshake]bool{} // link id -> pre-join visits
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
		if isGuestPresenceNotice(m) {
			return // never crosses the guest leg — see isGuestPresenceNotice
		}
		s.guestMu.Lock()
		sessions := append([]*guestSession(nil), s.guestSessions[m.ChannelID]...)
		s.guestMu.Unlock()
		if len(sessions) == 0 {
			return
		}
		line, _ := json.Marshal(s.guestFrameFor(m))
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

// guestNoticeMark opens every room notice about a guest arriving or leaving.
// One constant shared by the two writers below and by isGuestPresenceNotice,
// because recognising these by their prose is how a later reword silently
// reopens the leak the filter exists to close.
const guestNoticeMark = "👤 "

func guestJoinNotice(name string) string {
	return guestNoticeMark + name + " joined as a guest (via your meeting link)"
}
func guestLeftNotice(name string) string { return guestNoticeMark + name + " (guest) left" }

// isGuestPresenceNotice reports whether a message is one of those notices.
// They are for the MEMBERS: the room tells the people who own it who has walked
// in off a link. None of them crosses the guest leg — not replayed in the
// history a guest is welcomed with, and not fanned out live either.
//
// Replaying them was the leak: whoever opened the link last was handed a
// roll-call of every stranger who had been in the room that day. Filtering only
// the replay left a race — one guest's departure notice lands live on the next
// guest who is already connecting — and that guest never met them either, so
// the line between "presence you are witnessing" and "presence you are being
// told about" is not one the fan-out can draw. Guests still see each other
// perfectly well through the things they choose to do: a message they send is
// attributed to them, and joining the call puts them in the roster.
//
// The cost is that a guest no longer sees the notice of their OWN arrival. The
// room header already says "you: <name>", so nothing is lost but a duplicate.
func isGuestPresenceNotice(m domain.Message) bool {
	return m.Kind == "system" && strings.HasPrefix(m.Content, guestNoticeMark)
}

// guestFrameFor renders one room message as the frame a guest receives. Used
// for the live fan-out and for the history replay, so the two cannot drift.
//
// A system notice goes out with NO author. It carries one anyway before this:
// the very first thing a guest saw was the notice of their OWN arrival, stamped
// `"from":"<the host's display name>"` — so a stranger learned who they were
// talking to before that person had said a word, from a frame the page renders
// authorless regardless. The room notice is the room's, not anyone's.
func (s *Service) guestFrameFor(m domain.Message) guestFrame {
	f := guestFrame{
		Type:    "msg",
		From:    s.senderLabel(m),
		Content: m.Content,
		Sent:    m.Sent.UTC().Format(time.RFC3339),
	}
	if m.Kind == "system" {
		f.Type = "sys"
		f.From = ""
	}
	return f
}

// loadGuestTokens restores issued guest links, dropping any whose meeting is
// gone (the startup sweep has run) or whose lifetime has passed, and every
// link saved before the guest leg was encrypted: those were bearer tokens the
// operator could have read off the wire, and the page that used them is gone.
// Event rooms and booking rooms get fresh links where they load
// (initEventGuests, initBookings).
func (s *Service) loadGuestTokens() {
	raw, err := s.store.GetSetting(guestTokensKey)
	if err != nil || raw == "" {
		return
	}
	var saved map[string]guestToken
	if json.Unmarshal([]byte(raw), &saved) != nil {
		saved = nil
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
	for id, tok := range saved {
		secret, err := b64url.DecodeString(tok.S)
		if err != nil || len(secret) != guestSecretBytes || guestLinkID(secret) != id {
			continue
		}
		if alive[tok.GuildID] && now.Before(tok.Expires) {
			s.guestTokens[id] = tok
		}
	}
	s.guestMu.Unlock()
	s.saveGuestTokens()
}

func (s *Service) saveGuestTokens() {
	s.guestMu.Lock()
	out := make(map[string]guestToken, len(s.guestTokens))
	for id, tok := range s.guestTokens {
		out[id] = tok
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

func newGuestSessionID() (string, error) {
	b, err := guestRandom(8)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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

// guestURL is the link a guest opens: the host's peer id and the secret both
// ride the fragment, which browsers never send to the server.
func (s *Service) guestURL(base, secret string) string {
	return fmt.Sprintf("%s/guest#h=%s&k=%s", base, s.host.PeerID(), secret)
}

// CreateGuestLink issues a browser-guest URL for a meeting. The link points
// at the rendezvous node's HTTPS side; the host peer id and the link secret S
// ride the URL FRAGMENT, which browsers never send to the server, and the
// guest page never puts S on the socket.
//
// lifetimeHours picks how long the link (and with it the meeting room) lives,
// from the fixed menu in meetingLifetimes. Zero means "leave the lifetime
// alone" — what an older caller that knows nothing about lifetimes sends, and
// what keeps the historic 24h-from-creation behaviour intact.
//
// Called again for the same meeting it returns the SAME url with a new
// lifetime, rather than a second link: changing your mind about the duration
// must not silently kill the link you already pasted into a calendar invite.
// After RevokeGuestLink there is no link, so the next call mints a fresh S.
func (s *Service) CreateGuestLink(guildID string, lifetimeHours int) (string, error) {
	return s.createGuestLink(guildID, lifetimeHours, maxGuestsPerLink)
}

func (s *Service) createGuestLink(guildID string, lifetimeHours, maxGuests int) (string, error) {
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
	id, tok, found := s.guestLinkForLocked(guildID)
	if !found {
		raw, err := guestRandom(guestSecretBytes)
		if err != nil {
			s.guestMu.Unlock()
			return "", err
		}
		id = guestLinkID(raw)
		tok = guestToken{GuildID: guildID, ChannelID: channelID, S: b64url.EncodeToString(raw), MaxGuests: maxGuests}
	}
	tok.Expires = expires
	s.guestTokens[id] = tok
	s.guestMu.Unlock()
	s.saveGuestTokens()
	return s.guestURL(base, tok.S), nil
}

// guestLinkForLocked finds the link into a guild. Caller holds guestMu.
func (s *Service) guestLinkForLocked(guildID string) (string, guestToken, bool) {
	for id, tok := range s.guestTokens {
		if tok.GuildID == guildID {
			return id, tok, true
		}
	}
	return "", guestToken{}, false
}

// RevokeGuestLink deletes an instant meeting's guest link and ends every guest
// visit in that meeting at once: in the room, knocking, or still in the
// handshake. The next Copy link mints a new secret. Event rooms are revoked
// from the event (RevokeEventGuestLink, so the event gets the new link) and
// booking rooms by cancelling the booking.
func (s *Service) RevokeGuestLink(guildID string) error {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	kind := ""
	if ok {
		kind = g.Kind
	}
	s.mu.RUnlock()
	if !ok || kind != "meeting" {
		return fmt.Errorf("app: guest links are for instant meetings")
	}
	if s.isBookingMeeting(guildID) {
		return fmt.Errorf("app: a booking's link is revoked by cancelling the booking")
	}
	if s.eventGuestRoom(guildID) {
		return fmt.Errorf("app: this room belongs to an event; revoke its link from the event")
	}
	s.dropGuestTokens(guildID)
	return nil
}

// SetGuestLinkHistory chooses whether guests on a meeting's link are shown the
// last guestHistoryCount messages when they arrive. Off by default.
func (s *Service) SetGuestLinkHistory(guildID string, on bool) error {
	s.guestMu.Lock()
	id, tok, found := s.guestLinkForLocked(guildID)
	if found {
		tok.History = on
		s.guestTokens[id] = tok
	}
	s.guestMu.Unlock()
	if !found {
		return fmt.Errorf("app: this meeting has no guest link")
	}
	s.saveGuestTokens()
	return nil
}

// GuestLinkHistory reports SetGuestLinkHistory's choice (false when the
// meeting has no link yet, which is also what a new link starts with).
func (s *Service) GuestLinkHistory(guildID string) (bool, error) {
	s.guestMu.Lock()
	defer s.guestMu.Unlock()
	_, tok, _ := s.guestLinkForLocked(guildID)
	return tok.History, nil
}

// dropGuestTokens revokes every guest link into a guild immediately and ends
// every visit through them: live and knocking guests get a sealed end (and
// leave the call, which closes the host's peer connection to them), and
// handshakes that have not yet proved the link are cut off silently. The
// links are deleted and the visits collected under guestMu, the lock admission
// takes, so a visit is either already admitted and ended here, or finds its
// link gone when it reaches the door.
func (s *Service) dropGuestTokens(guildID string) {
	channels := map[string]bool{}
	s.mu.RLock()
	if g, ok := s.guilds[guildID]; ok {
		for _, ch := range g.Channels {
			channels[ch.ID] = true
		}
	}
	s.mu.RUnlock()
	var sessions []*guestSession
	s.guestMu.Lock()
	for id, tok := range s.guestTokens {
		if tok.GuildID != guildID {
			continue
		}
		channels[tok.ChannelID] = true
		// Handshakes still before their join are left alone: closing them here
		// would tell an operator parking hellos on a link id that it was live
		// until this instant. They fail silently at the join instead, through
		// the same re-check a dead link gets, within milliseconds for a real
		// guest and at the hello timeout for a parked one.
		delete(s.guestTokens, id)
	}
	for ch := range channels {
		sessions = append(sessions, s.guestSessions[ch]...)
	}
	s.guestMu.Unlock()
	s.saveGuestTokens()
	for _, sess := range sessions {
		sess.decide(guestEndRevoked)
		// Not on this goroutine: the write is bounded but can still take its
		// full timeout on a guest that stopped reading, and the caller is an RPC.
		go sess.end(guestEndRevoked)
	}
}

// claimGuestHandshake registers a visit that has said hello, within the
// pre-join caps. Caller holds guestMu.
func (s *Service) claimGuestHandshakeLocked(linkID string, h *guestHandshake) bool {
	total := 0
	for _, m := range s.guestShakes {
		total += len(m)
	}
	if total >= maxGuestHandshakes || len(s.guestShakes[linkID]) >= maxGuestHandshakesPerLink {
		return false
	}
	if s.guestShakes[linkID] == nil {
		s.guestShakes[linkID] = map[*guestHandshake]bool{}
	}
	s.guestShakes[linkID][h] = true
	return true
}

// releaseGuestHandshakeLocked forgets a handshake; false when a revoke had
// already cut it off. Caller holds guestMu.
func (s *Service) releaseGuestHandshakeLocked(linkID string, h *guestHandshake) bool {
	m := s.guestShakes[linkID]
	if !m[h] {
		return false
	}
	delete(m, h)
	if len(m) == 0 {
		delete(s.guestShakes, linkID)
	}
	return true
}

// liveGuestLinkLocked returns the link if it is present and not past its own
// expiry. Caller holds guestMu (and must not hold s.mu: guestMu is never held
// while s.mu is taken, so the meeting's side is meetingOpen, called apart).
func (s *Service) liveGuestLinkLocked(linkID string) (guestToken, bool) {
	tok, ok := s.guestTokens[linkID]
	if !ok || time.Now().After(tok.Expires) {
		return guestToken{}, false
	}
	return tok, true
}

// meetingOpen reports whether a link's meeting still takes guests, and its
// name. Two clocks have to agree for a link to be live: the link's own expiry
// and the meeting's. They are set together, but a lifetime shortened after the
// link went out (or a room whose expiry passed on a machine that never
// restarted, so the startup sweep never ran) must close the door too.
func (s *Service) meetingOpen(guildID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok || time.Now().After(s.meetingExpiryLocked(guildID)) {
		return "", false
	}
	return g.Name, true
}

// validGuestGid accepts the page's random id: base64url characters, bounded.
func validGuestGid(gid string) bool {
	if gid == "" || len(gid) > maxGuestGidBytes {
		return false
	}
	for _, r := range gid {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
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

// guestHandshakeResult is what a proven join hands the rest of the visit.
type guestHandshakeResult struct {
	linkID string
	join   guestFrame
	rx, tx *guestCipher
	shake  *guestHandshake
}

// guestHandshake runs hello → chal → sealed join (docs/dev/guest-e2e.md).
// Every hello that parses gets a chal of the same shape, live link or not; an
// unknown link is given a random K so the visit proceeds identically and
// fails at the join. On any failure it returns false and the caller closes
// the stream without a word, so a wrong key, an unknown link, an expired one
// and a revoked one look the same on the wire. On success the handshake is
// still registered (the caller releases it under guestMu as it registers the
// session).
func (s *Service) guestHandshake(conn io.ReadWriteCloser, r *bufio.Reader) (guestHandshakeResult, bool) {
	var res guestHandshakeResult
	setGuestReadDeadline(conn, time.Now().Add(guestHelloTimeout))
	line, err := readGuestLine(r, maxGuestHelloBytes)
	if err != nil {
		return res, false
	}
	var hello guestHello
	if json.Unmarshal(line, &hello) != nil || hello.Type != "hello" || hello.V != 2 ||
		hello.Link == "" || len(hello.Link) > 64 {
		return res, false
	}
	gn, err := b64url.DecodeString(hello.Gn)
	if err != nil || len(gn) != guestNonceBytes {
		return res, false
	}
	gpk, err := b64url.DecodeString(hello.Gpk)
	if err != nil {
		return res, false
	}
	guestPub, err := guestPublicKey(gpk)
	if err != nil {
		return res, false
	}

	shake := &guestHandshake{conn: conn}
	s.guestMu.Lock()
	claimed := s.claimGuestHandshakeLocked(hello.Link, shake)
	tok, known := s.guestTokens[hello.Link]
	s.guestMu.Unlock()
	if !claimed {
		return res, false
	}
	release := func() {
		s.guestMu.Lock()
		s.releaseGuestHandshakeLocked(hello.Link, shake)
		s.guestMu.Unlock()
	}

	hn, err := guestRandom(guestNonceBytes)
	if err != nil {
		release()
		return res, false
	}
	hsk, err := guestEphemeral()
	if err != nil {
		release()
		return res, false
	}
	z, err := hsk.ECDH(guestPub)
	if err != nil {
		release()
		return res, false
	}
	var k []byte
	if known {
		if secret, err := b64url.DecodeString(tok.S); err == nil {
			k = guestLinkKey(secret)
		}
	}
	if k == nil {
		// The same work a known link costs (decode aside), so the operator
		// sitting next to the host cannot time the difference.
		dummy, err := guestRandom(guestSecretBytes)
		if err != nil {
			release()
			return res, false
		}
		k = guestLinkKey(dummy)
	}
	hpk := hsk.PublicKey().Bytes()
	chal, _ := json.Marshal(guestChal{Type: "chal", Hn: b64url.EncodeToString(hn), Hpk: b64url.EncodeToString(hpk)})
	if d, ok := conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = d.SetWriteDeadline(time.Now().Add(guestWriteTimeout))
	}
	if _, err := conn.Write(append(chal, '\n')); err != nil {
		release()
		return res, false
	}
	g2h, h2g := guestSessionKeys(k, z, hello.Link, s.host.PeerID().String(), gn, hn, gpk, hpk)
	rx, tx := newGuestCipher(g2h), newGuestCipher(h2g)

	// The join: the first sealed record, inside the same deadline. Clear
	// frames are dropped unread.
	var join guestFrame
	for {
		line, err := readGuestLine(r, maxGuestFrameBytes)
		if err != nil {
			release()
			return res, false
		}
		body, ok := guestRecordBody(line)
		if !ok {
			continue
		}
		pt, err := rx.open(body)
		if err != nil || json.Unmarshal(pt, &join) != nil || join.Type != "join" || !validGuestGid(join.Gid) {
			release()
			return res, false
		}
		break
	}
	if !known {
		release() // cannot happen with a random K, but never let it through
		return res, false
	}
	res = guestHandshakeResult{linkID: hello.Link, join: join, rx: rx, tx: tx, shake: shake}
	return res, true
}

// serveGuest runs one guest's whole visit on the relayed stream.
func (s *Service) serveGuest(conn io.ReadWriteCloser) {
	defer conn.Close()
	r := bufio.NewReaderSize(conn, 64<<10)

	hs, ok := s.guestHandshake(conn, r)
	if !ok {
		return // silently: see guestHandshake
	}
	name := sanitizeGuestName(hs.join.Name)
	id, err := newGuestSessionID()
	if err != nil {
		s.guestMu.Lock()
		s.releaseGuestHandshakeLocked(hs.linkID, hs.shake)
		s.guestMu.Unlock()
		return
	}

	// Re-check the link now that the guest has proved it. The meeting's side
	// first (it takes s.mu, which is never taken under guestMu), then the link
	// and the registration under one hold of guestMu: a revoke that ran during
	// the handshake either removed the handshake (released == false) or the
	// link (live == false), and either way the visit ends here with nothing
	// said.
	s.guestMu.Lock()
	tok, live := s.liveGuestLinkLocked(hs.linkID)
	s.guestMu.Unlock()
	meetingName, open := "", false
	if live {
		meetingName, open = s.meetingOpen(tok.GuildID)
	}
	s.guestMu.Lock()
	released := s.releaseGuestHandshakeLocked(hs.linkID, hs.shake)
	tok, live = s.liveGuestLinkLocked(hs.linkID)
	if !released || !live || !open {
		s.guestMu.Unlock()
		return
	}
	sess := &guestSession{
		id:        id,
		name:      name,
		fpr:       guestFingerprint(name, id),
		channelID: tok.ChannelID,
		linkID:    hs.linkID,
		gid:       hs.join.Gid,
		conn:      conn,
		tx:        hs.tx,
		send:      make(chan []byte, 64),
		gate:      make(chan string, 1),
	}
	// From here the guest holds the link, so a refusal is said (sealed).
	refusal := ""
	switch {
	case hasString(tok.Banned, sess.gid):
		refusal = guestEndRemoved
	case !hasString(tok.Guests, sess.gid) && len(tok.Guests) >= tok.maxGuests():
		// A new guest on a used-up link is turned away before the knock; a
		// returning one is let through to it (a known gid never skips it).
		refusal = guestEndUsedUp
	case len(s.guestSessions[tok.ChannelID]) >= maxGuestsPerMeeting:
		refusal = guestEndFull
	default:
		// Knockers are registered as well as joiners: they hold a socket and a
		// slot, so they must count against the cap and be reachable by the
		// host's verdict and by a revoke.
		s.guestSessions[tok.ChannelID] = append(s.guestSessions[tok.ChannelID], sess)
		s.guestByID[sess.id] = sess
	}
	s.guestMu.Unlock()
	if refusal != "" {
		sess.end(refusal)
		return
	}
	defer func() {
		s.guestMu.Lock()
		list := s.guestSessions[sess.channelID]
		for i, gs := range list {
			if gs == sess {
				s.guestSessions[sess.channelID] = append(list[:i], list[i+1:]...)
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
			s.sendSystem(sess.channelID, guestLeftNotice(name))
		}
	}()

	// Writer: queued frames → stream.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case line := <-sess.send:
				if err := sess.writeLine(line); err != nil {
					// The record's counter is spent whether or not its bytes
					// left, so the stream can never resynchronise: end it.
					sess.end("")
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
	//
	// Nothing is read from the guest while they knock. Whatever they send in
	// that time is read after admission, in order: records carry a counter, so
	// skipping any would break every one after it. The page sends nothing but
	// the join until it is welcomed, and an admitted guest could send the same
	// line a moment later anyway; what keeps a knocking guest unheard is that
	// no guest verb is parsed until they are in.
	if s.guestDoorLocked(tok.ChannelID) || s.isBookingMeeting(tok.GuildID) || s.eventGuestKnocks(tok.GuildID) {
		admitted, reason := s.knockAtDoor(sess, meetingName)
		if !admitted {
			sess.end(reason)
			return
		}
	}
	// A fresh name, not `tok`: on refusal admitGuest returns a zero link, and
	// the deferred cleanup above must still find this session's channel.
	link, reason := s.admitGuest(sess)
	if reason != "" {
		sess.end(reason)
		return
	}

	// Welcome. `Channel` tells the guest which voice room to label its
	// signaling with.
	_ = sess.write(guestFrame{
		Type: "welcome", Meeting: meetingName, Name: name, Channel: link.ChannelID,
	})
	// An event room shows the event's details, rendered from the event record
	// now rather than replayed from the room, so it holds with history off.
	seed, isEvent := s.eventGuestSeedFor(link.GuildID)
	if isEvent {
		_ = sess.write(guestFrame{Type: "sys", Content: seed, Sent: time.Now().UTC().Format(time.RFC3339)})
	}
	// Recent history only when the host chose it for this link.
	if link.History {
		if msgs, err := s.store.Messages(link.ChannelID, guestHistoryCount); err == nil {
			for _, m := range msgs {
				if m.Deleted || (m.Kind != "" && m.Kind != "system" && m.Kind != "guest") {
					continue
				}
				// Who else has been through this room is the members' business, not a
				// visitor's. Dropped here rather than left to the page: a filter that
				// still ships the name and asks the browser not to draw it has already
				// leaked it. Members replaying the same history see every notice.
				if isGuestPresenceNotice(m) {
					continue
				}
				if isEvent && m.Kind == "system" && m.Content == seed {
					continue // already shown above
				}
				_ = sess.write(s.guestFrameFor(m))
			}
		}
	}
	s.sendSystem(link.ChannelID, guestJoinNotice(name))

	// The clock the host set on the link applies to the people already inside,
	// not only to the next arrival — see guestExpiryPoll.
	go s.endGuestAtExpiry(sess, link.GuildID, done)

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
		body, ok := guestRecordBody(line)
		if !ok {
			continue // a clear frame inside a sealed session: injected, ignored
		}
		pt, err := hs.rx.open(body)
		if err != nil {
			return // replayed, reordered, reflected or forged: the session is over
		}
		var f guestFrame
		if json.Unmarshal(pt, &f) != nil {
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
		content = clampBytes(content, maxGuestMsgBytes)
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
		_, stillAlive := s.guilds[link.GuildID]
		s.mu.RUnlock()
		if !stillAlive {
			sess.end(guestEndEnded)
			return
		}
		// Relayed under our signature, but authored by THEM: kind "guest" + their
		// name, so clients give them their own bubble instead of tucking their
		// words under the host's like a subheading.
		if _, err := s.sendAs(tok.ChannelID, content, "guest", "", sess.name, ""); err != nil {
			_ = sess.write(guestFrame{Type: "info", Reason: "Message didn't send — try again."})
		}
	}
}

// admitGuest lets a guest in, re-checking the link under guestMu (the lock a
// revoke takes) so a revoke during the knock wins, and counting a new gid
// against the link. Returns the link as it stands, or a reason to turn the
// guest away.
func (s *Service) admitGuest(sess *guestSession) (guestToken, string) {
	s.guestMu.Lock()
	tok, live := s.liveGuestLinkLocked(sess.linkID)
	s.guestMu.Unlock()
	if !live {
		return tok, guestEndRevoked
	}
	if _, open := s.meetingOpen(tok.GuildID); !open {
		return tok, guestEndEnded
	}
	s.guestMu.Lock()
	tok, live = s.liveGuestLinkLocked(sess.linkID)
	reason := ""
	counted := false
	switch {
	case !live:
		reason = guestEndRevoked
	case hasString(tok.Banned, sess.gid):
		reason = guestEndRemoved
	case !hasString(tok.Guests, sess.gid):
		if len(tok.Guests) >= tok.maxGuests() {
			reason = guestEndUsedUp
		} else {
			tok.Guests = append(append([]string(nil), tok.Guests...), sess.gid)
			s.guestTokens[sess.linkID] = tok
			counted = true
		}
	}
	if reason == "" {
		sess.admitMu.Lock()
		sess.admitted = true
		sess.admitMu.Unlock()
	}
	s.guestMu.Unlock()
	if counted {
		s.saveGuestTokens()
	}
	return tok, reason
}

// banGuestGid keeps a removed guest's browser out of that link for good.
func (s *Service) banGuestGid(linkID, gid string) {
	s.guestMu.Lock()
	tok, ok := s.guestTokens[linkID]
	if ok && !hasString(tok.Banned, gid) {
		tok.Banned = append(append([]string(nil), tok.Banned...), gid)
		s.guestTokens[linkID] = tok
	}
	s.guestMu.Unlock()
	if ok {
		s.saveGuestTokens()
	}
}

// endGuestAtExpiry closes a live guest session when the meeting's lifetime runs
// out, so "this link works until Tuesday" is true of the room and not only of
// the door. Returns when the visit ends (done), when the service stops, or once
// it has said goodbye.
func (s *Service) endGuestAtExpiry(sess *guestSession, guildID string, done <-chan struct{}) {
	for {
		at := s.meetingExpiry(guildID)
		if at.IsZero() {
			// Not a meeting any more — the guild was deleted. The read loop's own
			// aliveness check owns that case; there is no deadline to watch here.
			return
		}
		wait := time.Until(at)
		if wait <= 0 {
			sess.end("This meeting link has expired.")
			return
		}
		if wait > guestExpiryPoll {
			wait = guestExpiryPoll
		}
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-done:
			t.Stop()
			return
		case <-s.ctx.Done():
			t.Stop()
			return
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
			return false, guestEndEnded
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

// decideGuest applies a host's call-control action to a browser guest.
//
// The target names ONE session, and it arrives in either of the two shapes the
// host's UI has for a guest: the FINGERPRINT ("guest:<name>#<session>"), which
// is what the knock list keys by, or the voice PEER ID ("guest:<session>"),
// which is what the call roster keys by — that is the id the mesh handed the
// guest, so it is what a tile knows about the person sitting in it. Both carry
// the session id, so either identifies one visitor and admitting one "Alice"
// still does not admit a second stranger who typed the same name.
//
// Accepting only the fingerprint made the roster's Remove button a no-op: it
// passes the peer id, matched nothing, and returned silently — the host saw a
// "Removed X" toast while X stayed in the call.
//
// This never leaves the node. A guest exists only as a relayed socket held HERE,
// so there is nothing to gossip and nobody else who could act on it.
func (s *Service) decideGuest(channelID, action, target string) {
	s.guestMu.Lock()
	var sess *guestSession
	for _, gs := range s.guestSessions[channelID] {
		if gs.fpr == target || guestPeerID(gs.id) == target {
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
			// Removed means removed: the same browser cannot walk back in on
			// this link. Someone with the link can clear their storage and use
			// a fresh slot; the slot limit and revoke cover that.
			// Every session of that browser on this link goes, not only the
			// tile the host clicked: a second tab is the same guest.
			s.banGuestGid(sess.linkID, sess.gid)
			s.guestMu.Lock()
			var same []*guestSession
			for _, gs := range s.guestSessions[channelID] {
				if gs.linkID == sess.linkID && gs.gid == sess.gid {
					same = append(same, gs)
				}
			}
			s.guestMu.Unlock()
			for _, gs := range same {
				gs.decide(guestEndRemoved)
				go gs.end(guestEndRemoved)
			}
			return
		}
		sess.decide("The host didn't let you in this time.")
	}
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
	n = clampBytes(n, maxGuestNameBytes)
	return n
}
