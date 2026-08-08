package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	cnet "github.com/ZahakJ/concord/internal/net"
)

// The public booking door: /book/<token> serves a self-contained page, and
// /book/api relays each of that page's requests over ONE short libp2p stream
// to the HOST whose peer id the link's fragment carries. Same trust shape as
// the guest gateway: this node stores nothing (no availability, no bookings,
// no names — the HOST computes and keeps everything), it merely pipes a
// request line in and a response line out. When the host is offline the page
// says so honestly, because there is deliberately nothing here to answer
// from.
//
// What this node DOES own is abuse control for the open-web side: visitors
// have no identity, so the only handle to rate-limit a scripted booker by is
// their IP, and that is only visible here.

//go:embed bookpage/*
var bookPage embed.FS

const (
	// One request line to the host and one line back, then the stream closes;
	// the response cap matches the host side's (a slot list or an ICS file).
	bookingDialTimeout = 15 * time.Second
	bookingIOTimeout   = 20 * time.Second
	maxBookingBody     = 8 << 10

	// Per-IP budgets. Browsing slots is cheap and repeated (the page refreshes
	// after a failed pick); actually BOOKING mints a meeting room on the
	// host's disk, so it gets a hard trickle: three quickly, then one per
	// half-minute.
	bookSlotsRefill = 0.5 // requests/second
	bookSlotsBurst  = 12
	bookBookRefill  = 1.0 / 30
	bookBookBurst   = 3

	// Bound the bucket table so a botnet scanning with fresh IPs can't grow it
	// forever; entries idle past bookBucketIdle are reaped opportunistically.
	maxBookBuckets = 8192
	bookBucketIdle = time.Hour
)

type bookBucket struct {
	slots, book float64
	last        time.Time
}

func (b *bookBucket) take(kind string, now time.Time) bool {
	dt := now.Sub(b.last).Seconds()
	b.slots += dt * bookSlotsRefill
	if b.slots > bookSlotsBurst {
		b.slots = bookSlotsBurst
	}
	b.book += dt * bookBookRefill
	if b.book > bookBookBurst {
		b.book = bookBookBurst
	}
	b.last = now
	which := &b.slots
	if kind == "book" {
		which = &b.book
	}
	if *which < 1 {
		return false
	}
	*which--
	return true
}

type bookLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bookBucket
}

func (l *bookLimiter) allow(ip, kind string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*bookBucket{}
	}
	// Reap idle entries before refusing to grow: a full table must not become
	// a denial of service against every NEW visitor.
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
		b = &bookBucket{slots: bookSlotsBurst, book: bookBookBurst, last: now}
		l.buckets[ip] = b
	}
	return b.take(kind, now)
}

// clientIP prefers the first X-Forwarded-For hop (fly.io terminates TLS in
// front of this node) and falls back to the socket address for local runs.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// bookingAPIRequest is what the page posts. The gateway re-marshals a typed
// struct rather than piping the raw body, so unknown fields — a smuggling
// channel to the host, or padding toward the frame cap — die here.
type bookingAPIRequest struct {
	H     string `json:"h"` // host peer id (from the link's fragment)
	Op    string `json:"op"`
	Token string `json:"token"`
	Slot  int64  `json:"slot,omitempty"`
	Name  string `json:"name,omitempty"`
	Note  string `json:"note,omitempty"`
}

// hostBookingRequest is the line relayed to the host: the API request minus
// the routing field.
type hostBookingRequest struct {
	Op    string `json:"op"`
	Token string `json:"token"`
	Slot  int64  `json:"slot,omitempty"`
	Name  string `json:"name,omitempty"`
	Note  string `json:"note,omitempty"`
}

func writeBookingJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// registerBookingGateway mounts the booking page + relay on the guest
// gateway's mux (they share the HTTPS door and its TLS/host setup).
func registerBookingGateway(ctx context.Context, mux *http.ServeMux, h host.Host) {
	limiter := &bookLimiter{}

	// The page. Served for any /book/<token> path — the token is validated by
	// the HOST, not here; this node can't tell a real token from a probe and
	// shouldn't be able to.
	mux.HandleFunc("/book/", func(w http.ResponseWriter, r *http.Request) {
		b, err := bookPage.ReadFile("bookpage/index.html")
		if err != nil {
			http.Error(w, "booking page unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		// Hard CSP, same stance as the guest page: self-contained, nothing
		// fetched from anywhere else, nowhere to exfiltrate to. connect-src
		// 'self' admits exactly the /book/api relay below.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; "+
				"connect-src 'self'; img-src data:;")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = w.Write(b)
	})

	mux.HandleFunc("/book/api", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req bookingAPIRequest
		body := http.MaxBytesReader(w, r.Body, maxBookingBody)
		if err := json.NewDecoder(body).Decode(&req); err != nil {
			writeBookingJSON(w, http.StatusBadRequest, `{"ok":false,"error":"Bad request."}`)
			return
		}
		if req.Op != "slots" && req.Op != "book" {
			writeBookingJSON(w, http.StatusBadRequest, `{"ok":false,"error":"Bad request."}`)
			return
		}
		// Gateway-side caps mirror (loosely) the host's own: the host re-caps
		// everything, these just stop oversized junk travelling at all.
		if len(req.Token) > 128 || len(req.Name) > 400 || len(req.Note) > 4000 {
			writeBookingJSON(w, http.StatusBadRequest, `{"ok":false,"error":"Bad request."}`)
			return
		}
		pid, err := peer.Decode(req.H)
		if err != nil {
			writeBookingJSON(w, http.StatusBadRequest, `{"ok":false,"error":"Bad request."}`)
			return
		}
		if !limiter.allow(clientIP(r), req.Op) {
			writeBookingJSON(w, http.StatusTooManyRequests,
				`{"ok":false,"error":"Too many requests — give it a minute and try again."}`)
			return
		}

		line, err := json.Marshal(hostBookingRequest{
			Op: req.Op, Token: req.Token, Slot: req.Slot, Name: req.Name, Note: req.Note,
		})
		if err != nil {
			writeBookingJSON(w, http.StatusInternalServerError, `{"ok":false,"error":"Bad request."}`)
			return
		}

		dialCtx, cancel := context.WithTimeout(ctx, bookingDialTimeout)
		defer cancel()
		// The host may hold nothing but a relay reservation with us, which
		// NewStream refuses by default — same as the guest relay.
		stream, err := h.NewStream(network.WithAllowLimitedConn(dialCtx, "booking"), pid, cnet.BookingProtocol)
		if err != nil {
			// The honest answer: this node keeps no copy of the calendar, so an
			// offline host means there is nothing to say. `unreachable` lets the
			// page word it properly.
			writeBookingJSON(w, http.StatusOK,
				`{"ok":false,"unreachable":true,"error":"The host's Concord app isn't reachable right now."}`)
			return
		}
		defer stream.Close()
		deadline := time.Now().Add(bookingIOTimeout)
		_ = stream.SetWriteDeadline(deadline)
		if _, err := stream.Write(append(line, '\n')); err != nil {
			writeBookingJSON(w, http.StatusOK,
				`{"ok":false,"unreachable":true,"error":"The host's Concord app isn't reachable right now."}`)
			return
		}
		_ = stream.SetReadDeadline(deadline)
		resp, err := readCappedLine(bufio.NewReaderSize(stream, 4096), cnet.MaxBookingResponse)
		if err != nil || len(resp) == 0 || !json.Valid(resp) {
			writeBookingJSON(w, http.StatusOK,
				`{"ok":false,"unreachable":true,"error":"The host's Concord app isn't reachable right now."}`)
			return
		}
		writeBookingJSON(w, http.StatusOK, string(resp))
	})
}
