package main

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"github.com/libp2p/go-libp2p/core/network"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	cnet "github.com/zahak/concord/internal/net"
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
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser client (curl/tests)
		}
		host := os.Getenv("CONCORD_PUBLIC_HOST")
		return host == "" || origin == "https://"+host || origin == "http://"+host
	},
}

// serveGuestGateway starts the HTTPS side of the node (fly.io terminates TLS
// in front of it). Disabled when CONCORD_GUEST_PORT is empty.
func serveGuestGateway(ctx context.Context, h host.Host, ts *turnServer) {
	port := os.Getenv("CONCORD_GUEST_PORT")
	if port == "" {
		port = "8080"
	}

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
		relayGuest(ctx, h, w, r)
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
func relayGuest(ctx context.Context, h host.Host, w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("h")
	pid, err := peer.Decode(hostID)
	if err != nil {
		http.Error(w, "bad host id", http.StatusBadRequest)
		return
	}

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
