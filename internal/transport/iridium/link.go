// Package iridium carries Concord bundles over Iridium Short Burst Data
// through a RockBLOCK 9603 (or any 9602/9603 on a serial port), using the
// modem's AT command set.
//
// Delivery between two Concord users goes satellite → Iridium gateway →
// Rock7's cloud → satellite: a message whose first bytes are "RB" and the
// destination's 7-digit RockBLOCK serial is forwarded by Rock7 to that device,
// provided both are in the same Delivery Group on the Rock7 account. One MO
// per peer, so cost scales with the number of peers configured.
//
// Every frame costs money, so the link reports a non-zero CostMicroUSD and the
// Manager holds frames in the outbox until the user approves the total.
// Nothing is transmitted that the user did not release — including by the
// mailbox checks, which run with an emptied MO buffer.
package iridium

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/serialport"
)

const (
	// rbPrefix is "RB" + 7-digit serial: Rock7's device-to-device routing.
	rbPrefix = 9
	// MTU keeps one MO at exactly 250 bytes with the prefix — 5 credits —
	// and the delivered MT under the 270-byte MT buffer even if Rock7 were
	// to leave the prefix on.
	MTU  = 250 - rbPrefix
	baud = 19200

	// DefaultCreditUSD is the assumed price of one RockBLOCK credit: Ground
	// Control lists $0.20 (50-credit bundle) down to $0.10 (5,000) in
	// late 2026; $0.15 is the middle. Override with creditUSD.
	DefaultCreditUSD = 0.15

	maxDrain    = 8   // MT messages fetched per wake-up
	doneCap     = 256 // frames remembered as delivered to some peers
	ringBackoff = 30 * time.Second
)

// Config is <dataDir>/iridium.json; CONCORD_IRIDIUM overrides the device and
// CONCORD_IRIDIUM_PEERS (comma-separated serials) the peers.
type Config struct {
	Device string `json:"device"`
	// Peers are the RockBLOCK serial numbers (up to 7 digits) of the people
	// to reach. Each must be in this account's Delivery Group.
	Peers []string `json:"peers"`
	// CreditUSD is what one credit costs this user.
	CreditUSD float64 `json:"creditUSD"`
	// PollMinutes checks the mailbox on a timer. Every check bills a credit
	// even when empty (~$22/day at 10 min), so it is off by default and mail
	// is fetched on ring alerts and after every send instead.
	PollMinutes int `json:"pollMinutes"`
	// Attempts per send before giving up (failed sessions are not billed).
	Attempts   int           `json:"attempts"`
	RetryDelay time.Duration `json:"-"`
	// NoRingAlerts disables AT+SBDMTA=1 (ring alerts need the RI line or the
	// unsolicited SBDRING, both on by default on RockBLOCK).
	NoRingAlerts bool `json:"noRingAlerts"`
}

// LoadConfig reads dataDir/iridium.json and the environment.
func LoadConfig(dataDir string) Config {
	var c Config
	if dataDir != "" {
		if raw, err := os.ReadFile(filepath.Join(dataDir, "iridium.json")); err == nil {
			_ = json.Unmarshal(raw, &c)
		}
	}
	if v := strings.TrimSpace(os.Getenv("CONCORD_IRIDIUM")); v != "" {
		c.Device = v
	}
	if v := strings.TrimSpace(os.Getenv("CONCORD_IRIDIUM_PEERS")); v != "" {
		c.Peers = strings.Split(v, ",")
	}
	return c
}

func (c Config) normalize() (Config, error) {
	if c.CreditUSD <= 0 || math.IsNaN(c.CreditUSD) || c.CreditUSD > 10 {
		c.CreditUSD = DefaultCreditUSD
	}
	if c.Attempts <= 0 || c.Attempts > 10 {
		c.Attempts = 3
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = 20 * time.Second
	}
	if c.PollMinutes < 0 {
		c.PollMinutes = 0
	}
	var peers []string
	seen := map[string]bool{}
	var bad error
	for _, p := range c.Peers {
		p = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p), "RB"))
		if p == "" {
			continue
		}
		if len(p) > 7 || strings.Trim(p, "0123456789") != "" {
			bad = fmt.Errorf("iridium: peer %q is not a RockBLOCK serial (up to 7 digits)", p)
			continue
		}
		p = strings.Repeat("0", 7-len(p)) + p
		if !seen[p] {
			seen[p] = true
			peers = append(peers, p)
		}
	}
	c.Peers = peers
	return c, bad
}

// rockblockVendor is FTDI, the USB-serial chip on the RockBLOCK 9603.
var rockblockVendor = "0403"

// Dialer opens the modem port; tests substitute a simulator.
type Dialer func(ctx context.Context) (io.ReadWriteCloser, error)

// Link is the Iridium SBD transport.
type Link struct {
	cfg    Config
	cfgErr error
	dial   Dialer

	op sync.Mutex // one AT conversation at a time: Send, mailbox, ring

	mu      sync.Mutex
	m       *modem
	cancel  context.CancelFunc
	done    chan struct{}
	deliver transport.Deliver
	wake    chan struct{}
	csq     int
	lastErr string
	// done remembers which peers already have a frame, so a Flush retried
	// after a partial failure never bills the same peer twice.
	sentTo   map[[16]byte]map[string]bool
	sentToQ  [][16]byte
	billedMO int // credits spent on MO since start, for the status line
	billedMT int
}

// New returns a link; dial may be nil to use cfg.Device.
func New(cfg Config, dial Dialer) *Link {
	c, err := cfg.normalize()
	return &Link{cfg: c, cfgErr: err, dial: dial, sentTo: map[[16]byte]map[string]bool{}, csq: -1}
}

func (l *Link) Name() string  { return "iridium" }
func (l *Link) Label() string { return "Satellite (Iridium SBD / RockBLOCK)" }

// FrameCostMicroUSD is the most one frame costs the sender: 5 credits per
// peer (a full 250-byte MO). Recipients pay the same again to receive it.
func (l *Link) FrameCostMicroUSD() int64 {
	peers := max(1, len(l.cfg.Peers))
	return int64(math.Round(float64(peers*Credits(MTU+rbPrefix)) * l.cfg.CreditUSD * 1e6))
}

// Caps: ~60 bit/s is one 250-byte frame per ~30 s session; latency is a
// session plus Rock7's forwarding plus the far side's ring-alert session.
func (l *Link) Caps() transport.Caps {
	return transport.Caps{
		MTU:          MTU,
		Bitrate:      60,
		Latency:      90 * time.Second,
		CostMicroUSD: l.FrameCostMicroUSD(),
		Encryption:   true,
		Local:        false,
		Mode:         transport.Live, // paid: the outbox holds every frame for approval
		Range:        "global (needs open sky)",
	}
}

func (l *Link) device() string {
	if l.cfg.Device != "" {
		return l.cfg.Device
	}
	if found := serialport.Find(rockblockVendor); len(found) > 0 {
		return found[0]
	}
	return ""
}

// Probe never opens the port.
func (l *Link) Probe() transport.Status {
	l.mu.Lock()
	running, csq, lastErr := l.m != nil, l.csq, l.lastErr
	l.mu.Unlock()
	if l.cfgErr != nil {
		return transport.Status{State: transport.Failed, Detail: l.cfgErr.Error()}
	}
	if running {
		d := fmt.Sprintf("modem up, %d peer(s)", len(l.cfg.Peers))
		if csq >= 0 {
			d += fmt.Sprintf(", signal %d/5", csq)
		}
		if lastErr != "" {
			d += "; last: " + lastErr
		}
		return transport.Status{State: transport.Available, Detail: d}
	}
	if lastErr != "" {
		return transport.Status{State: transport.Failed, Detail: lastErr}
	}
	if l.dial == nil {
		dev := l.device()
		if dev == "" {
			return transport.Status{State: transport.NoHardware, Detail: "no RockBLOCK on USB; set CONCORD_IRIDIUM or iridium.json"}
		}
		if !serialport.Exists(dev) {
			return transport.Status{State: transport.NoHardware, Detail: dev + " is not present"}
		}
	}
	if len(l.cfg.Peers) == 0 {
		return transport.Status{State: transport.Failed, Detail: "no peer RockBLOCK serials configured (iridium.json \"peers\")"}
	}
	return transport.Status{State: transport.Available, Detail: fmt.Sprintf("RockBLOCK found; %d peer(s); $%.2f/credit", len(l.cfg.Peers), l.cfg.CreditUSD)}
}

// Start opens the modem and initialises it. It does not start a session:
// nothing goes to the satellite, and nothing is billed, until a send is
// approved, a ring alert says mail is waiting, or opt-in polling is due.
func (l *Link) Start(ctx context.Context, deliver transport.Deliver) error {
	if l.cfgErr != nil {
		return l.cfgErr
	}
	if len(l.cfg.Peers) == 0 {
		return errors.New("iridium: configure at least one peer RockBLOCK serial")
	}
	l.mu.Lock()
	if l.m != nil {
		l.mu.Unlock()
		return errors.New("iridium: already started")
	}
	l.mu.Unlock()
	var port io.ReadWriteCloser
	var err error
	if l.dial != nil {
		port, err = l.dial(ctx)
	} else if dev := l.device(); dev == "" {
		err = errors.New("iridium: no RockBLOCK found")
	} else {
		port, err = serialport.Open(dev, baud)
	}
	if err != nil {
		l.setErr(err)
		return err
	}
	m := newModem(port)
	if err := m.init(!l.cfg.NoRingAlerts); err != nil {
		m.close()
		l.setErr(err)
		return err
	}
	csq, _ := m.signal()
	m.busy(false) // from here, an SBDRING between conversations wakes the loop
	lctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	l.mu.Lock()
	l.m, l.cancel, l.done, l.deliver = m, cancel, done, deliver
	l.wake = make(chan struct{}, 1)
	l.csq, l.lastErr = csq, ""
	l.mu.Unlock()
	go l.loop(lctx, m, done)
	return nil
}

// Stop closes the port; an in-flight session is abandoned (the modem finishes
// or aborts it on its own; the MO stays in its buffer only until the next
// Start clears it).
func (l *Link) Stop() error {
	l.mu.Lock()
	m, cancel, done := l.m, l.cancel, l.done
	l.m, l.cancel, l.done = nil, nil, nil
	l.mu.Unlock()
	if m == nil {
		return nil
	}
	cancel()
	m.close()
	<-done
	l.op.Lock() // wait out a Send that was mid-command
	l.op.Unlock()
	return nil
}

// loop fetches mail on ring alerts, after sends that saw mail queued, and on
// the opt-in poll timer.
func (l *Link) loop(ctx context.Context, m *modem, done chan struct{}) {
	defer close(done)
	var tick <-chan time.Time
	if l.cfg.PollMinutes > 0 {
		t := time.NewTicker(time.Duration(l.cfg.PollMinutes) * time.Minute)
		defer t.Stop()
		tick = t.C
	}
	l.mu.Lock()
	wake := l.wake
	l.mu.Unlock()
	for {
		answer := false
		select {
		case <-ctx.Done():
			return
		case <-m.ring:
			answer = true
		case <-wake:
		case <-tick:
		}
		if err := l.mailbox(ctx, m, answer); err != nil && ctx.Err() == nil {
			l.setErr(err)
			if answer {
				// A ring that could not be answered (no sky) rings again;
				// don't spin on it.
				select {
				case <-ctx.Done():
					return
				case <-time.After(ringBackoff):
				}
			}
		}
	}
}

// mailbox runs sessions with an empty MO buffer until the gateway has no
// more mail for us (bounded per wake-up).
func (l *Link) mailbox(ctx context.Context, m *modem, answer bool) error {
	l.op.Lock()
	defer l.op.Unlock()
	m.busy(true)
	defer m.busy(false)
	if err := m.clearMO(); err != nil {
		return err
	}
	for i := 0; i < maxDrain && ctx.Err() == nil; i++ {
		r, err := m.session(answer)
		if err != nil {
			return err
		}
		answer = false
		if r.MTStatus == 2 || !r.MOOK() {
			return errors.New("iridium: mailbox check failed: " + r.MOReason())
		}
		if r.MTStatus == 1 {
			if err := l.fetch(m); err != nil {
				return err
			}
		}
		if r.MTQueued == 0 {
			return nil
		}
	}
	return nil
}

// fetch reads the MT buffer and hands a Concord frame to the Manager.
func (l *Link) fetch(m *modem) error {
	mt, err := m.readMT()
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.billedMT += Credits(len(mt))
	deliver := l.deliver
	l.mu.Unlock()
	if f := unwrapMT(mt); f != nil && deliver != nil {
		deliver(f)
	}
	return nil
}

// unwrapMT strips Rock7's "RB" + serial routing prefix if present and returns
// the frame only if it looks like a Concord bundle or fragment.
func unwrapMT(mt []byte) []byte {
	if len(mt) > rbPrefix && bytes.HasPrefix(mt, []byte("RB")) && isDigits(mt[2:rbPrefix]) {
		mt = mt[rbPrefix:]
	}
	if len(mt) < 2 || (mt[0] != 0xC1 && mt[0] != 0xC2) {
		return nil
	}
	return mt
}

func isDigits(b []byte) bool {
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Send transmits one approved frame to every peer: one SBD session per peer,
// retried on failure (failed sessions are free). The Manager only calls this
// when the user has released the outbox.
func (l *Link) Send(ctx context.Context, frame []byte) error {
	if len(frame) == 0 || len(frame) > MTU {
		return fmt.Errorf("iridium: frame of %d bytes (MTU %d)", len(frame), MTU)
	}
	l.op.Lock()
	defer l.op.Unlock()
	l.mu.Lock()
	m := l.m
	l.mu.Unlock()
	if m == nil {
		return errors.New("iridium: modem not started")
	}
	m.busy(true)
	defer m.busy(false)
	id := frameID(frame)
	var firstErr error
	for _, peer := range l.cfg.Peers {
		if l.alreadySent(id, peer) {
			continue
		}
		if err := l.sendTo(ctx, m, peer, frame); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("iridium: to RB%s: %w", peer, err)
			}
			continue
		}
		l.markSent(id, peer)
	}
	if firstErr != nil {
		l.setErr(firstErr)
	} else {
		l.setErr(nil)
	}
	return firstErr
}

func (l *Link) sendTo(ctx context.Context, m *modem, peer string, frame []byte) (err error) {
	mo := make([]byte, 0, rbPrefix+len(frame))
	mo = append(mo, "RB"+peer...)
	mo = append(mo, frame...)
	if err := m.writeMO(mo); err != nil {
		return err
	}
	// Whatever happens, the MO buffer is emptied afterwards: a message that
	// sits there would go out — and be billed — with the next mailbox check.
	defer func() {
		if cerr := m.clearMO(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	for attempt := 0; attempt < l.cfg.Attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(l.cfg.RetryDelay * time.Duration(attempt)):
			}
		}
		if csq, err := m.signal(); err == nil {
			l.mu.Lock()
			l.csq = csq
			l.mu.Unlock()
		}
		r, serr := m.session(false)
		if serr != nil {
			if errors.Is(serr, errClosed) {
				return serr
			}
			err = serr
			continue
		}
		if r.MTStatus == 1 {
			if ferr := l.fetch(m); ferr != nil {
				log.Printf("iridium: reading mail: %v", ferr)
			}
		}
		if r.MTQueued > 0 {
			l.poke()
		}
		if r.MOOK() {
			l.mu.Lock()
			l.billedMO += Credits(len(mo))
			l.mu.Unlock()
			return nil
		}
		err = errors.New(r.MOReason())
	}
	return err
}

func (l *Link) poke() {
	l.mu.Lock()
	w := l.wake
	l.mu.Unlock()
	if w != nil {
		select {
		case w <- struct{}{}:
		default:
		}
	}
}

func frameID(f []byte) [16]byte {
	s := sha256.Sum256(f)
	var id [16]byte
	copy(id[:], s[:])
	return id
}

func (l *Link) alreadySent(id [16]byte, peer string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sentTo[id][peer]
}

func (l *Link) markSent(id [16]byte, peer string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sentTo[id] == nil {
		l.sentTo[id] = map[string]bool{}
		l.sentToQ = append(l.sentToQ, id)
		if len(l.sentToQ) > doneCap {
			delete(l.sentTo, l.sentToQ[0])
			l.sentToQ = l.sentToQ[1:]
		}
	}
	l.sentTo[id][peer] = true
}

// Billed is the credits this link has spent since it was created: sent, and
// received.
func (l *Link) Billed() (mo, mt int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.billedMO, l.billedMT
}

func (l *Link) setErr(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err == nil {
		l.lastErr = ""
	} else {
		l.lastErr = err.Error()
	}
}
