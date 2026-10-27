// Package ble is Concord's Bluetooth LE link. Phones and laptops in the same
// room, campsite or building pass sealed frames to each other with no
// internet, no tower and no account.
//
// Every device plays both roles at once. As a peripheral it serves one GATT
// service, with a characteristic that centrals write frames to and one that it
// notifies frames on, and it advertises that service's UUID and nothing else.
// As a central it scans for the UUID and connects to what it finds, after a
// short random delay so that two devices that hear each other at the same
// moment rarely both dial. Send puts a frame on every connection: written to
// peers we dialed, notified to peers that dialed us. It is a mesh;
// transport.Manager relays and dedupes.
//
// What crosses the air is a transport frame (MLS ciphertext behind a 12-byte
// bundle header) and nothing else. docs/transports/ble.md says exactly what
// an observer can see.
package ble

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
)

// The Concord GATT service. It is fixed because it is how two strangers'
// devices find each other. It says only "this device runs Concord's BLE link".
const (
	ServiceUUID = "1c317699-815c-4666-9f0b-3452f8763f76"
	// RxUUID is written by centrals. One GATT write is one frame.
	RxUUID = "1c317699-815c-4666-9f0b-3452f8763f77"
	// TxUUID is notified to subscribed centrals. One notification is one
	// frame.
	TxUUID = "1c317699-815c-4666-9f0b-3452f8763f78"
)

const (
	// MaxFrame is the largest frame the link carries. A GATT write or
	// notification holds ATT_MTU-3 bytes. 185 is the smallest MTU the
	// platforms we care about settle on (it is iOS's ceiling; Android and
	// BlueZ negotiate up to 517). So 180 fits in one ATT packet for every peer
	// we accept, with no long-write reassembly.
	MaxFrame = 180
	// MinATTMTU is the negotiated ATT MTU a peer must reach for MaxFrame to
	// fit. Peers below it are refused rather than sent truncated frames.
	MinATTMTU = MaxFrame + 3

	// MaxConnAge is the longest any one connection is kept. An open
	// connection pins both sides' addresses for as long as it lasts. Closing
	// it on the same cadence as the kernel rotates private addresses (15 min)
	// lets the next connection use the new ones.
	MaxConnAge = 15 * time.Minute

	maxPeers     = 7                // outbound connections; controllers manage ~8 in all
	maxTracked   = 256              // devices remembered at once
	maxDialDelay = 3 * time.Second  // random wait before dialing a newly heard device
	evictIdle    = 2 * time.Minute  // with every slot full, a connection this quiet makes room
	staleAfter   = 90 * time.Second // forget an unconnected device not heard this long
	tickEvery    = 5 * time.Second
	writeTimeout = 2 * time.Second
	reconnectGap = 2 * time.Second
	healthyAfter = 30 * time.Second // a connection must carry traffic and last this long to clear backoff
	maxBackoff   = 5 * time.Minute
)

var (
	ErrNotRunning = errors.New("ble: link is not running")
	ErrFrameSize  = errors.New("ble: frame is empty or larger than the link MTU")
)

// Link is the BLE transport.Transport over a Radio.
type Link struct {
	radio Radio
	now   func() time.Time
	// dialDelay picks the wait before dialing a newly heard device; tests
	// pin it.
	dialDelay func() time.Duration

	// stopMu serialises Start and Stop, so that a Stop that returns has
	// really torn everything down even when two callers race it.
	stopMu sync.Mutex

	mu         sync.Mutex
	running    bool
	ctx        context.Context
	cancel     context.CancelFunc
	deliver    transport.Deliver
	peers      map[string]*peer
	subscribed bool
	wg         sync.WaitGroup

	probeMu   sync.Mutex
	probeAt   time.Time
	probeLast transport.Status
}

type peer struct {
	firstSeen  time.Time
	lastSeen   time.Time
	dialAt     time.Time // not before this: the random dial delay
	connected  bool      // a link exists that we did not dial (they dialed us)
	conn       PeerConn
	connAt     time.Time
	connAge    time.Duration // this connection's jittered MaxConnAge
	lastActive time.Time     // last frame either way on conn
	used       bool          // conn has carried at least one frame
	dialing    bool
	failures   int
	retryAt    time.Time
}

// New returns a link over r. Nothing touches the radio until Start.
func New(r Radio) *Link {
	return &Link{
		radio: r, now: time.Now, peers: map[string]*peer{},
		dialDelay: func() time.Duration { return time.Duration(rand.Int64N(int64(maxDialDelay))) },
	}
}

func (l *Link) Name() string  { return "ble" }
func (l *Link) Label() string { return "Bluetooth LE" }

// Caps are the honest numbers: 180-byte frames, and tens of kilobits a
// second once connection intervals, D-Bus round trips and several peers
// sharing one radio are paid for. That is far below the 1–2 Mbit/s PHY rate.
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

// Probe asks the radio at most once every few seconds, because the settings
// page polls.
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

// Start brings the link up: GATT service, advertisement, scanning. It is the
// only path that makes the radio do anything.
func (l *Link) Start(ctx context.Context, deliver transport.Deliver) error {
	l.stopMu.Lock()
	defer l.stopMu.Unlock()
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return nil
	}
	rctx, cancel := context.WithCancel(ctx)
	l.deliver = deliver
	l.ctx, l.cancel = rctx, cancel
	l.peers = map[string]*peer{}
	l.subscribed = false
	// running is set before the radio comes up, so devices it reports while
	// coming up are not ignored.
	l.running = true
	l.mu.Unlock()

	err := l.radio.Up(rctx, Events{
		Inbound:    l.inbound,
		Seen:       l.seen,
		Lost:       l.lost,
		Notified:   l.notified,
		Subscribed: l.setSubscribed,
	})
	if err == nil {
		err = l.radio.Advertise()
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
// service. When it returns, nothing of ours is left on the adapter.
func (l *Link) Stop() error {
	l.stopMu.Lock()
	defer l.stopMu.Unlock()
	return l.teardown()
}

// teardown is Stop's body. The caller holds stopMu.
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
// any radio, the link transmits into whatever air there is. Send fails only
// when there were peers and every one of them refused the frame. Peers are
// written in parallel, each under its own timeout, so one stuck peer cannot
// hold up the rest.
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

	var (
		mu        sync.Mutex
		delivered int
		failed    int
		firstErr  error
	)
	result := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			delivered++
			return
		}
		failed++
		if firstErr == nil {
			firstErr = err
		}
	}
	// Notify always goes to the radio: whether anyone is subscribed is only
	// a hint here, and BlueZ's StartNotify/StopNotify calls can arrive out of
	// order. With nobody subscribed it is a no-op.
	if err := l.radio.Notify(frame); sub {
		result(err)
	}
	var wg sync.WaitGroup
	for _, o := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := o.c.Write(wctx, frame)
			cancel()
			if err != nil {
				l.drop(o.id, o.c)
			} else {
				l.active(o.id)
			}
			result(err)
		}()
	}
	wg.Wait()
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
}

func (l *Link) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Stats{Seen: len(l.peers), Subscribed: l.subscribed}
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
	for {
		select {
		case <-ctx.Done():
			// The owner's context ended without a Stop (app shutdown).
			// Tear down anyway, from outside this goroutine.
			l.mu.Lock()
			running := l.running
			l.mu.Unlock()
			if running {
				go func() { _ = l.Stop() }()
			}
			return
		case <-tick.C:
			l.tick()
		}
	}
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

func (l *Link) notified(id string, frame []byte) {
	if len(frame) == 0 || len(frame) > MaxFrame {
		return
	}
	l.active(id)
	l.inbound(frame)
}

// active records traffic on id's outbound connection.
func (l *Link) active(id string) {
	l.mu.Lock()
	if p := l.peers[id]; p != nil && p.conn != nil {
		p.used, p.lastActive = true, l.now()
	}
	l.mu.Unlock()
}

func (l *Link) setSubscribed(on bool) {
	l.mu.Lock()
	l.subscribed = on
	l.mu.Unlock()
}

func (l *Link) seen(id string, connected bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running {
		return
	}
	now := l.now()
	p := l.peers[id]
	if p == nil {
		if len(l.peers) >= maxTracked && !l.evictStaleLocked() {
			return
		}
		p = &peer{firstSeen: now, dialAt: now.Add(l.dialDelay())}
		l.peers[id] = p
	}
	p.lastSeen = now
	p.connected = connected && p.conn == nil
	l.considerLocked(id, p)
}

// evictStaleLocked forgets the longest-unheard device that has no
// connection, to make room in a full table. Spoofed adverts then cycle
// through the table instead of locking real peers out of it.
func (l *Link) evictStaleLocked() bool {
	var victim string
	var oldest time.Time
	for id, p := range l.peers {
		if p.conn != nil || p.dialing {
			continue
		}
		if victim == "" || p.lastSeen.Before(oldest) {
			victim, oldest = id, p.lastSeen
		}
	}
	if victim == "" {
		return false
	}
	delete(l.peers, victim)
	return true
}

// lost is the radio reporting that id disconnected. A connection that drops
// before it carried a frame and lasted healthyAfter counts as a failed dial,
// so a device that accepts connections and then hangs up backs off like one
// that refuses them. An existing backoff is never shortened.
func (l *Link) lost(id string) {
	l.mu.Lock()
	p := l.peers[id]
	var c PeerConn
	if p != nil {
		now := l.now()
		switch {
		case p.conn != nil && !(p.used && now.Sub(p.connAt) >= healthyAfter):
			p.failures++
			p.retryAt = later(p.retryAt, now.Add(backoff(p.failures)))
		case p.conn != nil:
			p.failures = 0
			p.retryAt = later(p.retryAt, now.Add(reconnectGap))
		default:
			p.retryAt = later(p.retryAt, now.Add(reconnectGap))
		}
		c, p.conn = p.conn, nil
		p.connected = false
	}
	l.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// drop closes a connection that failed a write, if it is still the current
// one, and backs the peer off before it is dialed again.
func (l *Link) drop(id string, c PeerConn) {
	l.mu.Lock()
	p := l.peers[id]
	if p == nil || p.conn != c {
		l.mu.Unlock()
		return
	}
	p.conn = nil
	p.failures++
	p.retryAt = later(p.retryAt, l.now().Add(backoff(p.failures)))
	l.mu.Unlock()
	_ = c.Close()
}

func (l *Link) tick() {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return
	}
	now := l.now()
	var closing []PeerConn
	for id, p := range l.peers {
		if p.conn != nil && now.Sub(p.connAt) >= p.connAge {
			// Retire an old connection so that both sides' current
			// private addresses take over. This is not a failure.
			closing = append(closing, p.conn)
			p.conn, p.failures = nil, 0
			p.retryAt = later(p.retryAt, now.Add(reconnectGap))
			continue
		}
		if p.conn == nil && !p.dialing && now.Sub(p.lastSeen) > staleAfter {
			delete(l.peers, id)
			continue
		}
		l.considerLocked(id, p)
	}
	l.mu.Unlock()
	for _, c := range closing {
		_ = c.Close()
	}
}

// considerLocked dials p if it is due. Every device is dialed after a
// random delay from when it was first heard, never while a link to it
// already exists, and never inside its backoff.
func (l *Link) considerLocked(id string, p *peer) {
	now := l.now()
	if p.conn != nil || p.dialing || p.connected || now.Before(p.retryAt) || now.Before(p.dialAt) ||
		now.Sub(p.lastSeen) > staleAfter {
		return
	}
	if l.busyLocked() >= maxPeers && !l.evictIdleLocked(now) {
		return
	}
	p.dialing = true
	ctx := l.ctx
	l.wg.Add(1)
	go l.dial(ctx, id)
}

func (l *Link) busyLocked() int {
	n := 0
	for _, o := range l.peers {
		if o.conn != nil || o.dialing {
			n++
		}
	}
	return n
}

// evictIdleLocked closes the quietest outbound connection when every slot is
// full, but only if it has been idle for evictIdle. Seven devices that
// accept a connection and then say nothing cannot hold every slot while a
// real peer waits. The evicted peer is backed off.
func (l *Link) evictIdleLocked(now time.Time) bool {
	var victim *peer
	for _, o := range l.peers {
		if o.conn == nil || now.Sub(o.lastActive) < evictIdle {
			continue
		}
		if victim == nil || o.lastActive.Before(victim.lastActive) {
			victim = o
		}
	}
	if victim == nil {
		return false
	}
	c := victim.conn
	victim.conn = nil
	victim.failures++
	victim.retryAt = later(victim.retryAt, now.Add(backoff(victim.failures)))
	go func() { _ = c.Close() }()
	return true
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
	now := l.now()
	if err != nil {
		p.failures++
		p.retryAt = later(p.retryAt, now.Add(backoff(p.failures)))
		l.mu.Unlock()
		return
	}
	p.conn, p.connected, p.used = c, false, false
	p.connAt, p.lastActive = now, now
	p.connAge = jitter(MaxConnAge)
	l.mu.Unlock()
}

// backoff is 10 s, doubling with each failure in a row up to 5 min. failures
// only resets when a connection proves healthy (see lost): a peer that
// connects and soon drops keeps backing off.
func backoff(failures int) time.Duration {
	d := 10 * time.Second
	for i := 1; i < failures && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// jitter spreads d by ±10%, so that connections opened together do not all
// close together.
func jitter(d time.Duration) time.Duration {
	return d - d/10 + time.Duration(rand.Int64N(int64(d/5)+1))
}
