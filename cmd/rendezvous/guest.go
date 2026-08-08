package main

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	cnet "github.com/ZahakJ/concord/internal/net"
)

// The guest gateway: the ONE piece of Concord that speaks plain HTTPS to the
// open web. Someone with a meeting link opens /guest in any browser, the page
// upgrades to a WebSocket, and this node pipes that socket byte-for-byte into
// a libp2p stream to the MEETING HOST (whose PeerID the guest's link carries).
// The host validates the token and runs the whole guest session (see
// internal/app/guest.go) — this node is a dumb pipe: it does not read the
// meeting, cannot decrypt member-to-member traffic, and keeps no state.
//
// It is still a relaxation of the trust model versus full Concord peers, which
// is why guests are (a) meeting-scoped, (b) chat-only, and (c) labelled as
// guests in the room. The guest's traffic is TLS to here, then Noise to the
// host — never plaintext on the wire, but this node COULD read the guest's
// leg. That is the honest cost of "no install".

//go:embed guestpage/*
var guestPage embed.FS

// readCappedLine reads one newline-delimited frame from the host stream, bounding
// the accumulated bytes at max. A plain ReadBytes('\n') would buffer an unbounded
// newline-less stream from a misbehaving/compromised host and OOM the gateway.
func readCappedLine(r *bufio.Reader, max int) ([]byte, error) {
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
			return nil, fmt.Errorf("rendezvous: host frame exceeds cap")
		}
		buf = append(buf, b)
	}
}

// ipBucket is one client IP's allowance at one door.
type ipBucket struct {
	tokens float64
	last   time.Time
}

// ipLimiter is booking.go's bookLimiter with the budget left to the caller.
// Same table, same reaping, same reasoning — an anonymous caller's IP is the
// only handle this node has on them — but the guest socket and the TURN
// credential endpoint cost the node different things, so each names its own
// rate at the call site instead of baking two fixed budgets into the type.
// It borrows booking.go's table bounds because the failure it guards against
// is identical: a botnet on fresh IPs growing the map until the node dies.
//
// The zero value is usable, like bookLimiter's.
type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
}

func (l *ipLimiter) allow(ip string, rate, burst float64) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*ipBucket{}
	}
	// Reap before refusing to grow: a full table must not become a denial of
	// service against every NEW visitor.
	if len(l.buckets) >= maxBookBuckets {
		for k, b := range l.buckets {
			if now.Sub(b.last) > bookBucketIdle {
				delete(l.buckets, k)
			}
		}
		if len(l.buckets) >= maxBookBuckets {
			return false
		}
	}
	b := l.buckets[ip]
	if b == nil {
		b = &ipBucket{tokens: burst, last: now}
		l.buckets[ip] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * rate
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

// guestGate is the abuse control for /guest/ws. It is a value the caller owns
// rather than package state so the gateway's limits are visible where the
// gateway is built (and so a test can drive a fresh one).
//
// WHY THIS EXISTS AT ALL. /guest/ws is unauthenticated by design — the whole
// point is that someone with a link needs no account — and it makes this node
// DIAL a peer id the caller chose and then hold a stream open for the length
// of a meeting. Unbounded, that is a free dialer and a free socket farm: a
// loop can pin every dial worker in the swarm, and a slow drip of sockets
// nobody ever closes exhausts file descriptors while looking like traffic.
type guestGate struct {
	ips      ipLimiter
	sessions atomic.Int64
}

const (
	// Per IP. A real guest opens ONE socket and reopens it after a network
	// flap, so a small burst with a slow refill is generous for a person and
	// hostile to a loop.
	guestConnRate  = 0.2 // sustained new sockets per second per IP
	guestConnBurst = 10.0
	// Globally, for when the IPs are not the same IP. Each live session is a
	// goroutine pair, a libp2p stream and a browser socket; this bounds the
	// gateway's total exposure however the load is spread. Sized far above any
	// plausible real meeting and far below what would hurt the node.
	maxGuestSessions = 512
)

const (
	guestIdleTimeout = 10 * time.Minute
	// How often the gateway pings the browser. A guest in a call can be silent
	// for many minutes — ICE settles and the media then flows directly between
	// browser and app, where this node sees nothing — so an idle READ deadline on
	// its own hangs up on healthy meetings. Ping/pong is answered by the browser
	// itself, below the page's JavaScript, so this keeps ANY version of the guest
	// page alive, including one already sitting in someone's cached tab.
	guestPingEvery = 45 * time.Second
	// Big enough for a WebRTC offer/answer (SDP with video runs to several KB),
	// not big enough to be a pipe: the gateway is a dumb relay of bytes it
	// cannot read.
	guestMaxFrameSize = 32 << 10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// The page is served from this same origin; a guest link opened anywhere
	// else has no business driving someone's meeting.
	CheckOrigin: func(r *http.Request) bool {
		return guestOriginAllowed(r.Header.Get("Origin"), os.Getenv("CONCORD_PUBLIC_HOST"))
	},
}

// guestOriginAllowed decides whether a browser at origin may open the relay.
//
// A deployment that has been told its public name (CONCORD_PUBLIC_HOST) is a
// deployment that knows exactly one origin its own page is served from, so
// nothing else is admitted — including a MISSING Origin. Every real guest is a
// browser and browsers always send it; accepting its absence was a hole a
// scripted client walked straight through while every browser-borne attack was
// being carefully turned away. (Origin is not authentication — anyone with a
// socket can claim any value — so this only stops OTHER PAGES driving a
// visitor's meeting. The rate limit is what handles the scripted case.)
//
// With no public host configured the check stays open, because a local dev run
// is reached as localhost, as a LAN IP, and through whatever tunnel someone is
// testing with, and there is nothing here to compare against.
func guestOriginAllowed(origin, publicHost string) bool {
	if publicHost == "" {
		return true
	}
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	// Host, not the whole origin string: the scheme is whatever terminates TLS
	// in front of this node, and hostnames are case-insensitive.
	return strings.EqualFold(u.Host, publicHost)
}

// serveGuestGateway starts the HTTPS side of the node (fly.io terminates TLS
// in front of it). Disabled when CONCORD_GUEST_PORT is empty.
func serveGuestGateway(ctx context.Context, h host.Host, ts *turnServer) {
	port := os.Getenv("CONCORD_GUEST_PORT")
	if port == "" {
		port = "8080"
	}

	gate := &guestGate{}

	mux := http.NewServeMux()
	// The guest page itself (the meeting link points here; the token rides the
	// URL fragment, which the browser never sends to us).
	mux.HandleFunc("/guest", func(w http.ResponseWriter, r *http.Request) {
		b, err := guestPage.ReadFile("guestpage/index.html")
		if err != nil {
			http.Error(w, "guest page unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		// Hard CSP: the page is self-contained; it must not be able to load or
		// exfiltrate anything anywhere. media-src admits the guest's own camera
		// and the host's inbound tracks (MediaStreams attached via srcObject) —
		// nothing is fetched, so this widens no exfiltration path.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; "+
				"connect-src 'self' wss:; img-src data:; media-src mediastream: blob:;")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = w.Write(b)
	})
	// The relay socket: ?h=<host peer id>, then the guest's own frames.
	mux.HandleFunc("/guest/ws", func(w http.ResponseWriter, r *http.Request) {
		relayGuest(ctx, h, gate, w, r)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	// Public booking page + its request relay (see booking.go). Shares this
	// door because it IS the same door: plain HTTPS in, libp2p to the host out.
	registerBookingGateway(ctx, mux, h)
	// TURN credentials for IP-private calls. Present only when TURN is enabled;
	// returns fresh time-windowed creds (see turn.go).
	if ts != nil {
		mux.HandleFunc("/turn", ts.handleTURNCreds)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() {
		fmt.Println("Guest gateway listening on :" + port + " (/guest)")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "guest gateway:", err)
		}
	}()
}

// relayGuest pipes one browser WebSocket to one libp2p stream on the host.
func relayGuest(ctx context.Context, h host.Host, gate *guestGate, w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("h")
	pid, err := peer.Decode(hostID)
	if err != nil {
		http.Error(w, "bad host id", http.StatusBadRequest)
		return
	}
	if !gate.ips.allow(clientIP(r), guestConnRate, guestConnBurst) {
		http.Error(w, "too many connection attempts — give it a minute", http.StatusTooManyRequests)
		return
	}
	// Claimed BEFORE the upgrade so a flood is refused with a plain HTTP status
	// — cheaper than a hijacked connection, and something a load balancer can
	// see. Released by the defer, which covers every return below including the
	// failed dial.
	if n := gate.sessions.Add(1); n > maxGuestSessions {
		gate.sessions.Add(-1)
		http.Error(w, "the guest gateway is at capacity — try again shortly", http.StatusServiceUnavailable)
		return
	}
	defer gate.sessions.Add(-1)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(guestMaxFrameSize)

	// Dial the meeting host. We may not know its addresses directly — but the
	// host holds a relay reservation with US, so a circuit through this very
	// node reaches it.
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// The host we are relaying a guest to may itself only be reachable over a
	// relayed connection, which NewStream refuses by default.
	stream, err := h.NewStream(network.WithAllowLimitedConn(dialCtx, "guest"), pid, cnet.GuestProtocol)
	if err != nil {
		// Newline-terminated like every other frame — the guest page only
		// parses complete lines.
		_ = ws.WriteMessage(websocket.TextMessage,
			[]byte(`{"type":"end","reason":"The meeting host isn't reachable right now — ask them to keep Concord open."}`+"\n"))
		return
	}
	defer stream.Close()

	var once sync.Once
	shut := func() {
		once.Do(func() {
			_ = stream.Reset()
			_ = ws.Close()
		})
	}

	// All WebSocket writes go through here: gorilla allows exactly one concurrent
	// writer, and the relay goroutine below now shares the socket with the
	// keepalive pinger.
	var wmu sync.Mutex
	writeWS := func(typ int, payload []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		_ = ws.SetWriteDeadline(time.Now().Add(20 * time.Second))
		return ws.WriteMessage(typ, payload)
	}
	// A pong is proof the browser is still there, so it renews the read deadline
	// that guest FRAMES would otherwise have to renew.
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(guestIdleTimeout))
	})
	pinger := make(chan struct{})
	defer close(pinger)
	go func() {
		t := time.NewTicker(guestPingEvery)
		defer t.Stop()
		for {
			select {
			case <-pinger:
				return
			case <-t.C:
				if writeWS(websocket.PingMessage, nil) != nil {
					return
				}
			}
		}
	}()

	// host → guest. The host protocol is line-delimited JSON, so forward whole
	// LINES — never raw chunks. A chat line fits in one read; a WebRTC offer is
	// several KB of SDP and would otherwise be split across two WebSocket
	// messages, leaving the browser with two unparseable halves (which is
	// exactly what silently broke guest calls).
	go func() {
		defer shut()
		r := bufio.NewReaderSize(stream, guestMaxFrameSize)
		for {
			line, err := readCappedLine(r, guestMaxFrameSize)
			if len(line) > 0 {
				// The '\n' readCappedLine consumed goes back on: the guest page
				// buffers WebSocket data and parses only complete, newline-
				// terminated lines (frames can coalesce or split in transit).
				// Forwarding the line bare means its parser waits forever for a
				// delimiter that never comes — no welcome, no signaling, nothing.
				if werr := writeWS(websocket.TextMessage, append(line, '\n')); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// guest → host
	defer shut()
	for {
		_ = ws.SetReadDeadline(time.Now().Add(guestIdleTimeout))
		typ, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if typ != websocket.TextMessage {
			continue
		}
		if len(msg) == 0 || msg[len(msg)-1] != '\n' {
			msg = append(msg, '\n') // the host protocol is line-delimited JSON
		}
		if _, err := stream.Write(msg); err != nil {
			return
		}
	}
}
