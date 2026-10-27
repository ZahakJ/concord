// Package ble is Concord's Bluetooth LE link: phones and laptops in the same
// room, campsite or building pass sealed frames to each other with no
// internet, no tower and no account.
//
// Every device is both roles at once. As a peripheral it serves one GATT
// service — a characteristic centrals write frames to, and one it notifies
// frames on — and advertises that service. As a central it scans for the
// service and connects to what it finds. A pair of devices needs only one
// connection between them, so the advertisement carries a random nonce and
// the lower nonce dials (with a fallback if the other side never does). Send
// puts a frame on every connection: written to peers we dialed, notified to
// peers that dialed us. It is a mesh; transport.Manager relays and dedupes.
//
// What crosses the air is a transport frame — MLS ciphertext behind a 12-byte
// bundle header — and nothing else. See docs/transports/ble.md for exactly
// what an observer can see.
package ble

import (
	"bytes"
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
)

// The Concord GATT service. Fixed: it is what lets two strangers' devices find
// each other, and it says only "this device runs Concord's BLE link".
const (
	ServiceUUID = "1c317699-815c-4666-9f0b-3452f8763f76"
	// RxUUID is written by centrals: one GATT write is one frame.
	RxUUID = "1c317699-815c-4666-9f0b-3452f8763f77"
	// TxUUID is notified to subscribed centrals: one notification, one frame.
	TxUUID = "1c317699-815c-4666-9f0b-3452f8763f78"
)

const (
	// MaxFrame is the largest frame the link carries. A GATT write or
	// notification holds ATT_MTU-3 bytes; 185 is the smallest MTU the
	// platforms we care about settle on (iOS's ceiling; Android and BlueZ
	// negotiate up to 517), so 180 fits every peer we accept in one ATT
	// packet with no long-write reassembly.
	MaxFrame = 180
	// MinATTMTU is the negotiated ATT MTU a peer must reach for MaxFrame to
	// fit. Peers below it are refused rather than sent truncated frames.
	MinATTMTU = MaxFrame + 3

	// RotateEvery is how often the advertised nonce is replaced. It matches
	// the kernel's default resolvable-private-address lifetime.
	RotateEvery = 15 * time.Minute

	maxPeers      = 7                // outbound connections; controllers manage ~8 total
	maxTracked    = 256              // devices remembered at once, so spoofed adverts cannot grow memory
	fallbackAfter = 20 * time.Second // dial a higher-nonce peer that never dialed us
	staleAfter    = 90 * time.Second // forget an unconnected device unseen this long
	tickEvery     = 5 * time.Second
	writeTimeout  = 5 * time.Second
	maxBackoff    = 5 * time.Minute
)

var (
	ErrNotRunning = errors.New("ble: link is not running")
	ErrFrameSize  = errors.New("ble: frame is empty or larger than the link MTU")
)

// Link is the BLE transport.Transport over a Radio.
type Link struct {
	radio Radio
	now   func() time.Time

	// stopMu serialises Start and Stop, so a Stop that returns has really
	// torn everything down even when two callers race it.
	stopMu sync.Mutex

	mu         sync.Mutex
	running    bool
	ctx        context.Context
	cancel     context.CancelFunc
	deliver    transport.Deliver
	nonce      nonce
	peers      map[string]*peer
	subscribed bool
	wg         sync.WaitGroup

	probeMu   sync.Mutex
	probeAt   time.Time
	probeLast transport.Status
}

type peer struct {
	nonce     []byte // nil: advertised no nonce
	firstSeen time.Time
	lastSeen  time.Time
	connected bool // a link exists that we did not dial (they dialed us)
	conn      PeerConn
	dialing   bool
	failures  int
	retryAt   time.Time
}

// New returns a link over r. Nothing touches the radio until Start.
func New(r Radio) *Link {
	return &Link{radio: r, now: time.Now, peers: map[string]*peer{}}
}

func (l *Link) Name() string  { return "ble" }
func (l *Link) Label() string { return "Bluetooth LE" }

// Caps are the honest numbers: 180-byte frames, and tens of kilobits a second
// once connection intervals, write round-trips and several peers sharing one
// radio are paid for — not the 1–2 Mbit/s PHY rate.
func (l *Link) Caps() transport.Caps {
	return transport.Caps{
		MTU:        MaxFrame,
		Bitrate:    20_000,
		Latency:    150 * time.Millisecond,
		Encryption: true,
		Local:      true,
		Mode:       transport.Live,
		Range:      "~10–50 m",
	}
}

// Probe asks the radio, at most every few seconds: the settings page polls.
func (l *Link) Probe() transport.Status {
	l.probeMu.Lock()
	defer l.probeMu.Unlock()
	if !l.probeAt.IsZero() && l.now().Sub(l.probeAt) < 3*time.Second {
		return l.probeLast
	}
	l.probeLast = l.radio.Probe()
	l.probeAt = l.now()
	return l.probeLast
}

// Start powers the link up: GATT service, advertisement, scanning. It is the
// only path that makes the radio do anything.
func (l *Link) Start(ctx context.Context, deliver transport.Deliver) error {
	l.stopMu.Lock()
	defer l.stopMu.Unlock()
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return nil
	}
	l.mu.Unlock()

	rctx, cancel := context.WithCancel(ctx)
	ev := Events{
		Inbound:    l.inbound,
		Seen:       l.seen,
		Lost:       l.lost,
		Notified:   func(_ string, f []byte) { l.inbound(f) },
		Subscribed: l.setSubscribed,
	}
	l.mu.Lock()
	l.deliver = deliver
	l.ctx, l.cancel = rctx, cancel
	l.peers = map[string]*peer{}
	l.subscribed = false
	l.nonce = newNonce()
	n := l.nonce
	// running is set before the radio comes up so that devices it reports
	// while coming up are not ignored.
	l.running = true
	l.mu.Unlock()

	err := l.radio.Up(rctx, ev)
	if err == nil {
		err = l.radio.Advertise(encodeAd(n))
	}
	if err == nil {
		err = l.radio.Scan(true)
	}
	if err != nil {
		_ = l.teardown()
		return err
	}
	l.wg.Add(1)
	go l.loop(rctx)
	return nil
}

// Stop ends scanning, advertising and every connection, and removes the GATT
// service. When it returns nothing of ours is left on the adapter.
func (l *Link) Stop() error {
	l.stopMu.Lock()
	defer l.stopMu.Unlock()
	return l.teardown()
}

// teardown is Stop's body; the caller holds stopMu.
func (l *Link) teardown() error {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return nil
	}
	l.running = false
	cancel := l.cancel
	l.mu.Unlock()
	cancel()
	l.wg.Wait() // the loop and every dial in flight

	l.mu.Lock()
	peers := l.peers
	l.peers = map[string]*peer{}
	l.subscribed = false
	l.mu.Unlock()
	for _, p := range peers {
		if p.conn != nil {
			_ = p.conn.Close()
		}
	}
	return l.radio.Down()
}

// Send puts frame on every connection. Nobody in range is not an error: like
// any radio, the link transmits into whatever air there is. It fails only
// when there were peers and every one of them refused the frame.
func (l *Link) Send(ctx context.Context, frame []byte) error {
	if len(frame) == 0 || len(frame) > MaxFrame {
		return ErrFrameSize
	}
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return ErrNotRunning
	}
	type out struct {
		id string
		c  PeerConn
	}
	var conns []out
	for id, p := range l.peers {
		if p.conn != nil {
			conns = append(conns, out{id, p.conn})
		}
	}
	sub := l.subscribed
	l.mu.Unlock()

	var delivered, failed int
	var firstErr error
	fail := func(err error) {
		failed++
		if firstErr == nil {
			firstErr = err
		}
	}
	if sub {
		if err := l.radio.Notify(frame); err != nil {
			fail(err)
		} else {
			delivered++
		}
	}
	for _, o := range conns {
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		err := o.c.Write(wctx, frame)
		cancel()
		if err != nil {
			fail(err)
			l.drop(o.id, o.c)
			continue
		}
		delivered++
	}
	if delivered == 0 && failed > 0 {
		return firstErr
	}
	return nil
}

// Stats is a diagnostic snapshot for concord-blecheck.
type Stats struct {
	Seen       int  // Concord devices currently known
	Dialed     int  // outbound connections up
	Subscribed bool // some central is taking our notifications
	Nonce      []byte
}

func (l *Link) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Stats{Seen: len(l.peers), Subscribed: l.subscribed, Nonce: append([]byte(nil), l.nonce[:]...)}
	for _, p := range l.peers {
		if p.conn != nil {
			s.Dialed++
		}
	}
	return s
}

func (l *Link) loop(ctx context.Context) {
	defer l.wg.Done()
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()
	rot := time.NewTimer(jitter(RotateEvery))
	defer rot.Stop()
	for {
		select {
		case <-ctx.Done():
			// The owner's context ended without a Stop (app shutdown):
			// tear down anyway, from outside this goroutine.
			l.mu.Lock()
			running := l.running
			l.mu.Unlock()
			if running {
				go func() { _ = l.Stop() }()
			}
			return
		case <-tick.C:
			l.tick()
		case <-rot.C:
			if err := l.Rotate(); err != nil {
				log.Printf("ble: rotate advertisement: %v", err)
			}
			rot.Reset(jitter(RotateEvery))
		}
	}
}

// jitter spreads d by ±10% so devices that started together do not rotate in
// step (which would let an observer pair old and new nonces by timing).
func jitter(d time.Duration) time.Duration {
	return d - d/10 + time.Duration(rand.Int64N(int64(d/5)+1))
}

// Rotate replaces the advertised nonce now; the link does this by itself
// every RotateEvery. Existing connections stay up.
func (l *Link) Rotate() error {
	n := newNonce()
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return nil
	}
	l.nonce = n
	l.mu.Unlock()
	return l.radio.Advertise(encodeAd(n))
}

func (l *Link) inbound(frame []byte) {
	if len(frame) == 0 || len(frame) > MaxFrame {
		return
	}
	l.mu.Lock()
	d := l.deliver
	running := l.running
	l.mu.Unlock()
	if running && d != nil {
		d(append([]byte(nil), frame...))
	}
}

func (l *Link) setSubscribed(on bool) {
	l.mu.Lock()
	l.subscribed = on
	l.mu.Unlock()
}

func (l *Link) seen(id string, ad []byte, connected bool) {
	n, ok := decodeAd(ad)
	if !ok {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running {
		return
	}
	now := l.now()
	p := l.peers[id]
	if p == nil {
		if len(l.peers) >= maxTracked {
			return
		}
		p = &peer{firstSeen: now}
		l.peers[id] = p
	}
	if n != nil && !bytes.Equal(n, p.nonce) {
		p.nonce = n
	}
	p.lastSeen = now
	p.connected = connected && p.conn == nil
	l.considerLocked(id, p)
}

func (l *Link) lost(id string) {
	l.mu.Lock()
	p := l.peers[id]
	var c PeerConn
	if p != nil {
		c, p.conn = p.conn, nil
		p.connected = false
		p.retryAt = l.now().Add(2 * time.Second)
	}
	l.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// drop closes a connection that failed a write, if it is still the current
// one, and lets the peer be redialled after a backoff.
func (l *Link) drop(id string, c PeerConn) {
	l.mu.Lock()
	p := l.peers[id]
	if p == nil || p.conn != c {
		l.mu.Unlock()
		return
	}
	p.conn = nil
	p.failures++
	p.retryAt = l.now().Add(backoff(p.failures))
	l.mu.Unlock()
	_ = c.Close()
}

func (l *Link) tick() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running {
		return
	}
	now := l.now()
	for id, p := range l.peers {
		if p.conn == nil && !p.dialing && now.Sub(p.lastSeen) > staleAfter {
			delete(l.peers, id)
			continue
		}
		l.considerLocked(id, p)
	}
}

// considerLocked dials p if this side should own the connection.
func (l *Link) considerLocked(id string, p *peer) {
	now := l.now()
	if p.conn != nil || p.dialing || p.connected || now.Before(p.retryAt) ||
		now.Sub(p.lastSeen) > staleAfter {
		return
	}
	// The lower nonce dials. A peer without a nonce, or one with a higher
	// nonce that has not dialed us in fallbackAfter, gets dialed anyway.
	if p.nonce != nil && bytes.Compare(l.nonce[:], p.nonce) >= 0 && now.Sub(p.firstSeen) < fallbackAfter {
		return
	}
	busy := 0
	for _, o := range l.peers {
		if o.conn != nil || o.dialing {
			busy++
		}
	}
	if busy >= maxPeers {
		return
	}
	p.dialing = true
	ctx := l.ctx
	l.wg.Add(1)
	go l.dial(ctx, id)
}

func (l *Link) dial(ctx context.Context, id string) {
	defer l.wg.Done()
	c, err := l.radio.Connect(ctx, id)
	l.mu.Lock()
	p := l.peers[id]
	if !l.running || ctx.Err() != nil || p == nil {
		if p != nil {
			p.dialing = false
		}
		l.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
		return
	}
	p.dialing = false
	if err != nil {
		p.failures++
		p.retryAt = l.now().Add(backoff(p.failures))
		l.mu.Unlock()
		return
	}
	p.conn, p.failures, p.connected = c, 0, false
	l.mu.Unlock()
}

func backoff(failures int) time.Duration {
	d := 10 * time.Second
	for i := 1; i < failures && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}
