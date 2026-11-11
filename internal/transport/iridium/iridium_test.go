package iridium

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/iridium/sbdsim"
)

func TestChecksumAndCredits(t *testing.T) {
	if got := Checksum([]byte("hello")); got != [2]byte{0x02, 0x14} {
		t.Fatalf("checksum %x", got)
	}
	big := bytes.Repeat([]byte{0xFF}, 300) // wraps the 16-bit sum
	s := uint16(0)
	for range big {
		s += 0xFF
	}
	if got := Checksum(big); got != [2]byte{byte(s >> 8), byte(s)} {
		t.Fatalf("wrapped checksum %x", got)
	}
	for n, want := range map[int]int{0: 0, 1: 1, 50: 1, 51: 2, 250: 5, 340: 7} {
		if Credits(n) != want {
			t.Fatalf("Credits(%d)=%d want %d", n, Credits(n), want)
		}
	}
}

func TestParseSBDIX(t *testing.T) {
	r, err := ParseSBDIX("+SBDIX: 0, 12, 1, 5, 23, 2")
	if err != nil || r != (SBDIX{0, 12, 1, 5, 23, 2}) || !r.MOOK() {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = ParseSBDIX("+SBDIX:32,0,2,0,0,0")
	if err != nil || r.MOOK() || !strings.Contains(r.MOReason(), "satellite") {
		t.Fatalf("%+v %v", r, err)
	}
	for _, bad := range []string{"", "+SBDIX:", "+SBDIX: 1,2,3", "+SBDIX: a,b,c,d,e,f", "+SBDIX: 0,0,1,0,999,0", "+SBDIX: -1,0,0,0,0,0", "OK"} {
		if _, err := ParseSBDIX(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestUnwrapMT(t *testing.T) {
	f := []byte{0xC1, 3, 0, 1}
	if !bytes.Equal(unwrapMT(append([]byte("RB0012345"), f...)), f) || !bytes.Equal(unwrapMT(f), f) {
		t.Fatal("unwrap")
	}
	if unwrapMT([]byte("hello from Rock7 HQ")) != nil || unwrapMT(nil) != nil {
		t.Fatal("accepted a non-Concord MT")
	}
}

// The line parser must survive whatever a flaky serial line produces.
func TestModemSurvivesGarbage(t *testing.T) {
	a, b := net.Pipe()
	m := newModem(a)
	defer m.close()
	go func() {
		junk := make([]byte, 20000)
		_, _ = rand.Read(junk)
		_, _ = b.Write(junk)
		_, _ = b.Write([]byte("\r\nSBDRING\r\n+CSQ:4\r\nOK\r\n"))
	}()
	// Read directly (not via cmd, which would discard the backlog first).
	deadline := time.Now().Add(2 * time.Second)
	var got []string
	for {
		s, err := m.readLine(deadline)
		if err != nil {
			t.Fatalf("lines %q: %v", got, err)
		}
		got = append(got, s)
		if s == "OK" {
			break
		}
	}
	if len(got) < 2 || got[len(got)-2] != "+CSQ:4" {
		t.Fatalf("lost the response in the noise: %q", got[max(0, len(got)-3):])
	}
	select {
	case <-m.ring:
	default:
		t.Fatal("ring alert not noticed")
	}
	m.mu.Lock()
	n := len(m.buf)
	m.mu.Unlock()
	if n > rxCap {
		t.Fatal("buffer unbounded")
	}
	b.Close()
}

func simLink(t *testing.T, m *sbdsim.Modem, peers ...string) *Link {
	t.Helper()
	l := New(Config{Peers: peers, RetryDelay: 5 * time.Millisecond}, func(context.Context) (io.ReadWriteCloser, error) { return m.Pipe(), nil })
	return l
}

type inbox struct {
	mu  sync.Mutex
	got [][]byte
}

func (b *inbox) add(_ string, p []byte) {
	b.mu.Lock()
	b.got = append(b.got, append([]byte(nil), p...))
	b.mu.Unlock()
}

func (b *inbox) count() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.got) }

func waitUntil(t *testing.T, d time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func manager(t *testing.T, l *Link, in *inbox) *transport.Manager {
	t.Helper()
	m := transport.NewManager("", in.add)
	m.Register(l)
	m.Start(context.Background())
	if err := m.SetEnabled("iridium", true); err != nil {
		t.Fatal(err)
	}
	if v := m.Links()[0]; !v.Running {
		t.Fatalf("not running: %+v", v)
	}
	m.Carry("topic")
	t.Cleanup(m.Close)
	return m
}

// Two Concord managers on two RockBLOCKs in one delivery group: a message
// waits in the outbox with its price shown, nothing is spent until the user
// flushes, and then it arrives intact via ring alert on the far side.
func TestEndToEndThroughConstellation(t *testing.T) {
	sky := sbdsim.New(1)
	ma, mb := sky.AddModem(11111), sky.AddModem(22222)
	la, lb := simLink(t, ma, "22222"), simLink(t, mb, "0011111")
	inA, inB := &inbox{}, &inbox{}
	mgrA := manager(t, la, inA)
	_ = manager(t, lb, inB)

	payload := make([]byte, 600) // 3 frames at MTU 241
	_, _ = rand.Read(payload)
	mgrA.Send("topic", payload)
	time.Sleep(100 * time.Millisecond)

	v := mgrA.Links()[0]
	frames := transport.FrameCount(len(payload), MTU)
	if v.Queued != frames || v.QueuedCost != int64(frames)*la.FrameCostMicroUSD() {
		t.Fatalf("outbox %d frames costing %d; want %d frames", v.Queued, v.QueuedCost, frames)
	}
	if ma.Sessions() != 0 || ma.Credits() != 0 {
		t.Fatalf("spent before approval: %d sessions, %d credits", ma.Sessions(), ma.Credits())
	}
	n, cost, err := mgrA.Flush("iridium", 100)
	if err != nil || n != frames {
		t.Fatalf("flush: %d frames, %v", n, err)
	}
	// Billed at most what was shown.
	if got := int64(ma.Credits()) * int64(DefaultCreditUSD*1e6); got > cost {
		t.Fatalf("billed $%d µ, approved $%d µ", got, cost)
	}
	waitUntil(t, 5*time.Second, func() bool { return inB.count() == 1 })
	if !bytes.Equal(inB.got[0], payload) {
		t.Fatal("payload changed")
	}
	// The sender's MO buffer was cleared after each send, so nothing was
	// sent twice even though the modem keeps it after a successful session.
	if s := ma.Sent(); len(s) != frames {
		t.Fatalf("%d MOs reached the gateway, want %d", len(s), frames)
	}
	for _, mo := range ma.Sent() {
		if !bytes.HasPrefix(mo, []byte("RB0022222")) || len(mo) > 250 {
			t.Fatalf("bad MO %q…", mo[:9])
		}
	}
	// B fetched its mail on ring alerts only: one MT each, and no empty
	// (billed) mailbox checks.
	if want := 0; mb.Credits() > frames*Credits(MTU)+want {
		t.Fatalf("receiver billed %d credits for %d frames", mb.Credits(), frames)
	}
}

// Sessions fail when the sky is blocked; failed sessions are free, the frame
// stays queued, and a later flush delivers it without double billing.
func TestFailedSessionsRetryAndRequeue(t *testing.T) {
	sky := sbdsim.New(2)
	sky.Signal = 0
	ma, mb := sky.AddModem(1), sky.AddModem(2)
	la := simLink(t, ma, "2")
	inA, inB := &inbox{}, &inbox{}
	mgrA := manager(t, la, inA)
	_ = manager(t, simLink(t, mb, "1"), inB)
	mgrA.Send("topic", []byte("ciphertext stand-in"))
	time.Sleep(50 * time.Millisecond)
	if _, _, err := mgrA.Flush("iridium", 100); err == nil || !strings.Contains(err.Error(), "no satellite") {
		t.Fatalf("want no-satellite error, got %v", err)
	}
	if ma.Credits() != 0 || ma.Sessions() != 3 {
		t.Fatalf("failed sessions: %d credits, %d sessions (want 0, 3 attempts)", ma.Credits(), ma.Sessions())
	}
	if v := mgrA.Links()[0]; v.Queued != 1 {
		t.Fatalf("frame not requeued: %+v", v)
	}
	sky.Signal = 4
	if n, _, err := mgrA.Flush("iridium", 100); err != nil || n != 1 {
		t.Fatalf("second flush %d %v", n, err)
	}
	waitUntil(t, 5*time.Second, func() bool { return inB.count() == 1 })
	if ma.Credits() != 1 || len(ma.Sent()) != 1 {
		t.Fatalf("billed %d credits for %d MOs, want 1/1", ma.Credits(), len(ma.Sent()))
	}
}

// With two peers, a frame that reached one of them before the session to the
// other failed is not sent (or billed) to the first one again on retry.
func TestPartialFanOutNotRebilled(t *testing.T) {
	sky := sbdsim.New(3)
	ma := sky.AddModem(1)
	sky.AddModem(2)
	sky.AddModem(3)
	l := simLink(t, ma, "2", "3")
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	frame := []byte{0xC1, 3, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	id := frameID(frame)
	l.markSent(id, "0000002") // as if peer 2 already got it last time
	if err := l.Send(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	sent := ma.Sent()
	if len(sent) != 1 || !bytes.HasPrefix(sent[0], []byte("RB0000003")) {
		t.Fatalf("sent %q", sent)
	}
	if l.FrameCostMicroUSD() != 2*5*150000 {
		t.Fatalf("cost per frame %d", l.FrameCostMicroUSD())
	}
}

func TestNoPeersRefusesStart(t *testing.T) {
	sky := sbdsim.New(4)
	m := sky.AddModem(1)
	l := simLink(t, m)
	if err := l.Start(context.Background(), func([]byte) {}); err == nil {
		t.Fatal("started with nobody to send to")
	}
	if st := l.Probe(); st.State != transport.Failed {
		t.Fatalf("probe %+v", st)
	}
	if New(Config{Peers: []string{"12345678"}}, nil).Probe().State != transport.Failed {
		t.Fatal("bad serial accepted")
	}
	if New(Config{Peers: []string{"1"}, CreditUSD: -5}, nil).Caps().CostMicroUSD <= 0 {
		t.Fatal("link became free")
	}
}

func TestProbeDoesNotOpen(t *testing.T) {
	opened := false
	l := New(Config{Peers: []string{"1"}}, func(context.Context) (io.ReadWriteCloser, error) { opened = true; return nil, io.EOF })
	_ = l.Probe()
	_ = l.Caps()
	if opened {
		t.Fatal("probe opened the modem")
	}
}

// Stop must end the reader and loop goroutines and close the port, even
// while a session is in progress.
func TestStopDuringSession(t *testing.T) {
	sky := sbdsim.New(5)
	sky.SessionDelay = 300 * time.Millisecond
	ma := sky.AddModem(1)
	sky.AddModem(2)
	l := simLink(t, ma, "2")
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() { errc <- l.Send(context.Background(), []byte{0xC1, 0, 0, 0}) }()
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() { _ = l.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop hung")
	}
	<-errc
	if err := l.Send(context.Background(), []byte{0xC1, 0}); err == nil {
		t.Fatal("sent after Stop")
	}
}

// Opt-in polling: an MT that arrives without a ring alert is still fetched.
func TestPolling(t *testing.T) {
	sky := sbdsim.New(6)
	m := sky.AddModem(1)
	in := make(chan []byte, 1)
	l := New(Config{Peers: []string{"2"}, NoRingAlerts: true}, func(context.Context) (io.ReadWriteCloser, error) { return m.Pipe(), nil })
	if err := l.Start(context.Background(), func(f []byte) { in <- f }); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	m.Queue([]byte{0xC1, 1, 2, 3})
	l.poke() // stands in for the poll timer
	select {
	case f := <-in:
		if !bytes.Equal(f, []byte{0xC1, 1, 2, 3}) {
			t.Fatalf("%x", f)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("poll did not fetch mail")
	}
}

func shortTimeouts(t *testing.T) {
	old, oldRing := sbdTimeout, ringBackoff
	sbdTimeout, ringBackoff = 300*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { sbdTimeout, ringBackoff = old, oldRing })
}

// A session that completes in the modem but whose answer is lost (timeout)
// or garbled must be confirmed with AT+SBDSX, not retried: billed once,
// delivered once.
func TestLostSessionResultBilledOnce(t *testing.T) {
	shortTimeouts(t)
	for _, mode := range []string{"lost", "garbled"} {
		t.Run(mode, func(t *testing.T) {
			sky := sbdsim.New(7)
			ma, mb := sky.AddModem(1), sky.AddModem(2)
			if mode == "lost" {
				sky.LoseResults = 1
			} else {
				sky.GarbleResults = 1
			}
			l := simLink(t, ma, "2")
			if err := l.Start(context.Background(), func([]byte) {}); err != nil {
				t.Fatal(err)
			}
			defer l.Stop()
			got := make(chan []byte, 4)
			lb := simLink(t, mb, "1")
			if err := lb.Start(context.Background(), func(f []byte) { got <- f }); err != nil {
				t.Fatal(err)
			}
			defer lb.Stop()
			frame := []byte{0xC1, 3, 0, 1, 9, 9, 9, 9, 9, 9, 9, 9, 42}
			if err := l.Send(context.Background(), frame); err != nil {
				t.Fatalf("send: %v", err)
			}
			if err := l.Send(context.Background(), frame); err != nil { // a Manager retry
				t.Fatal(err)
			}
			if n := len(ma.Sent()); n != 1 || ma.Credits() != 1 {
				t.Fatalf("%d MOs, %d credits; want 1/1", n, ma.Credits())
			}
			select {
			case f := <-got:
				if !bytes.Equal(f, frame) {
					t.Fatal("frame changed")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("not delivered")
			}
		})
	}
}

// Stop in the middle of a session: the modem finishes it anyway. The next
// Start must learn that from MOMSN and never send that frame again.
func TestStopMidSessionReconciledOnStart(t *testing.T) {
	shortTimeouts(t)
	sky := sbdsim.New(8)
	sky.SessionDelay = 200 * time.Millisecond
	ma := sky.AddModem(1)
	sky.AddModem(2)
	l := simLink(t, ma, "2")
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	frame := []byte{0xC1, 3, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 7}
	errc := make(chan error, 1)
	go func() { errc <- l.Send(context.Background(), frame) }()
	time.Sleep(80 * time.Millisecond)
	_ = l.Stop()
	if err := <-errc; err == nil {
		t.Fatal("send reported success with its answer lost")
	}
	waitUntil(t, 2*time.Second, func() bool { return len(ma.Sent()) == 1 }) // the modem finished it
	if !strings.Contains(l.Probe().Detail, "awaiting modem confirmation") {
		t.Fatalf("pending not shown: %s", l.Probe().Detail)
	}
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	if err := l.Send(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if n := len(ma.Sent()); n != 1 || ma.Credits() != 1 {
		t.Fatalf("%d MOs, %d credits after restart; want 1/1", n, ma.Credits())
	}
}

// Rings are answered only within the daily receive budget.
func TestReceiveBudget(t *testing.T) {
	shortTimeouts(t)
	sky := sbdsim.New(9)
	m := sky.AddModem(1)
	var mu sync.Mutex
	n := 0
	l := New(Config{Peers: []string{"2"}, ReceiveCreditsPerDay: 3, RetryDelay: time.Millisecond},
		func(context.Context) (io.ReadWriteCloser, error) { return m.Pipe(), nil })
	if err := l.Start(context.Background(), func([]byte) { mu.Lock(); n++; mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	big := append([]byte{0xC1}, bytes.Repeat([]byte{1}, 99)...) // 2 credits each
	for i := 0; i < 5; i++ {
		m.Queue(big)
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if c := m.Credits(); c > 3+2 { // the last session may overshoot by one message
		t.Fatalf("receive spent %d credits, cap 3", c)
	}
	if d := l.Probe().Detail; !strings.Contains(d, "/3 credits") {
		t.Fatalf("budget not shown: %s", d)
	}
}

// A ring heard during a send whose session fetched the mail and emptied the
// mailbox is an echo: answering it would buy an empty check.
func TestRingAfterEmptyQueueIsSwallowed(t *testing.T) {
	sky := sbdsim.New(10)
	ma := sky.AddModem(1)
	sky.AddModem(2)
	l := simLink(t, ma, "2")
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	if err := l.Send(context.Background(), []byte{0xC1, 0, 0, 0, 5}); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	m := l.m
	l.mu.Unlock()
	// Mail for us arrives and rings while the send is in its session.
	sky.SessionDelay = 100 * time.Millisecond
	errc := make(chan error, 1)
	go func() { errc <- l.Send(context.Background(), []byte{0xC1, 0, 0, 0, 6}) }()
	time.Sleep(30 * time.Millisecond)
	ma.Queue([]byte{0xC1, 9, 9, 9})
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	before := ma.Sessions()
	_ = m
	time.Sleep(200 * time.Millisecond)
	if ma.Sessions() != before {
		t.Fatal("answered a ring for mail the send already fetched")
	}
}

// Rings that cannot be answered (no sky) give up after a few tries instead of
// retrying forever.
func TestUnanswerableRingsGiveUp(t *testing.T) {
	shortTimeouts(t)
	sky := sbdsim.New(11)
	ma := sky.AddModem(1)
	l := simLink(t, ma, "2")
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	sky.Signal = 0
	l.mu.Lock()
	m := l.m
	l.mu.Unlock()
	for i := 0; i < 10; i++ {
		m.mu.Lock()
		m.ringAt = time.Now()
		m.mu.Unlock()
		select {
		case m.ring <- struct{}{}:
		default:
		}
		time.Sleep(40 * time.Millisecond)
	}
	if s := ma.Sessions(); s > ringGiveUp {
		t.Fatalf("%d sessions for unanswerable rings, want at most %d", s, ringGiveUp)
	}
	if !strings.Contains(l.Probe().Detail, "ignoring rings") {
		t.Fatalf("mute not reported: %s", l.Probe().Detail)
	}
}
