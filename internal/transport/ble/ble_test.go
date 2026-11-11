package ble

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type inbox struct {
	mu     sync.Mutex
	frames [][]byte
}

func (b *inbox) add(f []byte) {
	b.mu.Lock()
	b.frames = append(b.frames, f)
	b.mu.Unlock()
}

func (b *inbox) count(f []byte) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, x := range b.frames {
		if bytes.Equal(x, f) {
			n++
		}
	}
	return n
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// node is a Link on fake air with its own clock and inbox.
type node struct {
	l   *Link
	r   *fakeRadio
	clk *clock
	in  *inbox
}

// startNode starts a link that dials a newly heard device after delay.
func startNode(t *testing.T, air *fakeAir, id string, delay time.Duration) *node {
	t.Helper()
	r := air.radio(id)
	l := New(r)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	l.now = clk.now
	l.dialDelay = func() time.Duration { return delay }
	in := &inbox{}
	if err := l.Start(context.Background(), in.add); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Stop() })
	return &node{l, r, clk, in}
}

func (n *node) dials() int {
	n.r.mu.Lock()
	defer n.r.mu.Unlock()
	return len(n.r.conns)
}

const never = time.Hour

// Two devices in range end up with one connection, dialed by whichever
// one's delay ran out first, and frames cross it once in each direction.
func TestPairMakesOneConnectionAndCarriesFramesBothWays(t *testing.T) {
	air := newFakeAir()
	fast := startNode(t, air, "fast", 0)
	slow := startNode(t, air, "slow", 2*time.Second)
	air.announce()
	eventually(t, "fast to dial slow", func() bool { return fast.l.Stats().Dialed == 1 })
	slow.clk.advance(3 * time.Second)
	air.announce() // slow now sees the link exists and must not dial back
	slow.l.tick()
	time.Sleep(20 * time.Millisecond)
	if n := slow.dials(); n != 0 {
		t.Fatalf("second device dialed %d connections, want 0", n)
	}
	if !slow.l.Stats().Subscribed {
		t.Fatal("slow should have a subscribed central")
	}

	f1, f2 := []byte("\xc1frame-fast-to-slow"), []byte("\xc1frame-slow-to-fast")
	if err := fast.l.Send(context.Background(), f1); err != nil {
		t.Fatal(err)
	}
	if err := slow.l.Send(context.Background(), f2); err != nil {
		t.Fatal(err)
	}
	if slow.in.count(f1) != 1 || fast.in.count(f2) != 1 {
		t.Fatalf("slow got %d, fast got %d; want 1 each", slow.in.count(f1), fast.in.count(f2))
	}
	if fast.in.count(f1) != 0 || slow.in.count(f2) != 0 {
		t.Fatal("a frame echoed back to its sender")
	}
}

func TestDialWaitsForItsDelay(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", 3*time.Second)
	startNode(t, air, "p", never)
	air.announce()
	n.l.tick()
	time.Sleep(20 * time.Millisecond)
	if n.dials() != 0 {
		t.Fatal("dialed before the random delay")
	}
	n.clk.advance(3 * time.Second)
	air.announce()
	eventually(t, "dial after delay", func() bool { return n.l.Stats().Dialed == 1 })
	for i := 0; i < 1000; i++ {
		if d := New(nil).dialDelay(); d < 0 || d >= maxDialDelay {
			t.Fatalf("dial delay %v", d)
		}
	}
}

// The reviewer's repro: a peer that accepts every connection and hangs up at
// once must back off 10, 20, 40 s… — not be redialled every 2 s because each
// hang-up resets the retry time.
func TestHangUpPeerBacksOff(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", 0)
	startNode(t, air, "rude", never)
	hangUp := func() {
		eventually(t, "connection", func() bool { return n.l.Stats().Dialed == 1 })
		n.l.lost("rude")
	}
	air.announce()
	hangUp()
	for i, wait := range []time.Duration{backoff(1), backoff(2), backoff(3)} {
		n.clk.advance(wait - time.Second)
		air.announce()
		n.l.tick()
		time.Sleep(20 * time.Millisecond)
		if got := n.dials(); got != i+1 {
			t.Fatalf("after %v: %d dials, want %d (backoff not respected)", wait-time.Second, got, i+1)
		}
		n.clk.advance(time.Second)
		air.announce()
		hangUp()
	}
	if backoff(2) != 20*time.Second || backoff(20) != maxBackoff {
		t.Fatal("backoff schedule changed")
	}
}

// A connection that carried traffic and lasted counts as healthy: its
// failures reset, and losing it allows a prompt redial.
func TestHealthyConnectionClearsBackoff(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", 0)
	startNode(t, air, "p", never)
	air.announce()
	eventually(t, "dial", func() bool { return n.l.Stats().Dialed == 1 })
	n.l.mu.Lock()
	n.l.peers["p"].failures = 3
	n.l.mu.Unlock()
	if err := n.l.Send(context.Background(), []byte{0xc1}); err != nil {
		t.Fatal(err)
	}
	n.clk.advance(healthyAfter)
	n.l.lost("p")
	n.l.mu.Lock()
	f := n.l.peers["p"].failures
	n.l.mu.Unlock()
	if f != 0 {
		t.Fatalf("failures %d after a healthy connection", f)
	}
	n.clk.advance(reconnectGap)
	air.announce()
	eventually(t, "prompt redial", func() bool { return n.l.Stats().Dialed == 1 })
}

func TestLostNeverShortensBackoff(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", never)
	n.l.seen("x", false)
	n.l.mu.Lock()
	far := n.l.now().Add(time.Hour)
	n.l.peers["x"].retryAt = far
	n.l.mu.Unlock()
	n.l.lost("x")
	n.l.mu.Lock()
	got := n.l.peers["x"].retryAt
	n.l.mu.Unlock()
	if !got.Equal(far) {
		t.Fatalf("lost moved retryAt from %v to %v", far, got)
	}
}

func TestSendLimitsAndEmptyAir(t *testing.T) {
	air := newFakeAir()
	l := New(air.radio("x"))
	if err := l.Send(context.Background(), []byte{1}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("send before start: %v", err)
	}
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	if err := l.Send(context.Background(), nil); !errors.Is(err, ErrFrameSize) {
		t.Fatal("empty frame accepted")
	}
	if err := l.Send(context.Background(), make([]byte, MaxFrame+1)); !errors.Is(err, ErrFrameSize) {
		t.Fatal("oversized frame accepted")
	}
	if err := l.Send(context.Background(), make([]byte, MaxFrame)); err != nil {
		t.Fatalf("send into empty air: %v", err)
	}
	if l.Caps().MTU != MaxFrame || !l.Caps().Local || !l.Caps().Encryption || l.Caps().Mode != transport.Live {
		t.Fatalf("caps %+v", l.Caps())
	}
}

// One peer whose writes hang must not stall delivery to the others, and
// Send must return within the per-peer timeout.
func TestStuckPeerDoesNotStallOthers(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", 0)
	good := startNode(t, air, "good", never)
	startNode(t, air, "stuck", never)
	air.announce()
	eventually(t, "two dials", func() bool { return n.l.Stats().Dialed == 2 })
	n.r.mu.Lock()
	for _, c := range n.r.conns {
		if c.to.id == "stuck" {
			c.stuck = true
		}
	}
	n.r.mu.Unlock()
	f := []byte("\xc1to-everyone")
	start := time.Now()
	if err := n.l.Send(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > writeTimeout+time.Second {
		t.Fatalf("Send took %v", el)
	}
	if good.in.count(f) != 1 {
		t.Fatal("good peer did not get the frame")
	}
	if n.l.Stats().Dialed != 1 {
		t.Fatal("stuck peer was not dropped")
	}
}

func TestInboundRejectsOversizeAndEmpty(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", never)
	n.l.inbound(nil)
	n.l.inbound(make([]byte, MaxFrame+1))
	n.l.notified("x", make([]byte, MaxFrame+1))
	n.l.inbound([]byte{0xc1, 1})
	n.in.mu.Lock()
	got := len(n.in.frames)
	n.in.mu.Unlock()
	if got != 1 {
		t.Fatalf("delivered %d frames, want 1", got)
	}
}

func TestFailedWriteDropsPeerAndBacksOff(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", 0)
	startNode(t, air, "p", never)
	air.announce()
	eventually(t, "dial", func() bool { return n.l.Stats().Dialed == 1 })
	n.r.mu.Lock()
	c := n.r.conns[0]
	n.r.mu.Unlock()
	c.mu.Lock()
	c.failNext = errors.New("gatt write failed")
	c.mu.Unlock()
	if err := n.l.Send(context.Background(), []byte{0xc1}); err == nil {
		t.Fatal("send succeeded with every peer failing")
	}
	if n.l.Stats().Dialed != 0 || !c.isClosed() {
		t.Fatal("failed peer was not dropped")
	}
	air.announce()
	n.l.tick()
	time.Sleep(20 * time.Millisecond)
	if n.l.Stats().Dialed != 0 {
		t.Fatal("redialed inside the backoff window")
	}
	n.clk.advance(backoff(1) + time.Second)
	air.announce()
	eventually(t, "redial after backoff", func() bool { return n.l.Stats().Dialed == 1 })
}

func TestUnheardDeviceIsForgotten(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", never)
	n.l.seen("ghost", false)
	if n.l.Stats().Seen != 1 {
		t.Fatal("not tracked")
	}
	n.clk.advance(staleAfter + time.Second)
	n.l.tick()
	if n.l.Stats().Seen != 0 {
		t.Fatal("stale device kept")
	}
}

// A full table makes room by forgetting the longest-unheard device, so that
// a flood of spoofed adverts cannot lock real peers out.
func TestFullTableEvictsStalest(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", never)
	for i := 0; i < maxTracked; i++ {
		n.l.seen(fmt.Sprintf("spoof%d", i), false)
		n.clk.advance(time.Millisecond)
	}
	n.l.seen("real", false)
	n.l.mu.Lock()
	_, real := n.l.peers["real"]
	_, first := n.l.peers["spoof0"]
	size := len(n.l.peers)
	n.l.mu.Unlock()
	if !real || first || size != maxTracked {
		t.Fatalf("real=%v spoof0=%v size=%d", real, first, size)
	}
}

// Seven devices that accept connections and then say nothing cannot hold
// every slot: once they have been idle for evictIdle, a waiting peer takes a
// slot from the quietest.
func TestIdleConnectionsAreEvictedForWaitingPeer(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "hub", 0)
	for i := 0; i < maxPeers; i++ {
		startNode(t, air, fmt.Sprintf("mute%d", i), never)
	}
	air.announce()
	eventually(t, "slots full", func() bool { return n.l.Stats().Dialed == maxPeers })
	startNode(t, air, "real", never)
	air.announce()
	n.l.tick()
	time.Sleep(20 * time.Millisecond)
	if n.dials() != maxPeers {
		t.Fatal("evicted a connection that was not idle yet")
	}
	n.clk.advance(evictIdle)
	air.announce()
	eventually(t, "real peer dialed", func() bool {
		n.l.mu.Lock()
		defer n.l.mu.Unlock()
		p := n.l.peers["real"]
		return p != nil && p.conn != nil
	})
	if d := n.l.Stats().Dialed; d != maxPeers {
		t.Fatalf("dialed %d, cap is %d", d, maxPeers)
	}
}

// No connection outlives MaxConnAge (±10%): an open connection pins both
// sides' addresses.
func TestOldConnectionsAreRetired(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", 0)
	startNode(t, air, "p", never)
	air.announce()
	eventually(t, "dial", func() bool { return n.l.Stats().Dialed == 1 })
	n.r.mu.Lock()
	c := n.r.conns[0]
	n.r.mu.Unlock()
	n.clk.advance(MaxConnAge * 11 / 10)
	n.l.tick()
	if !c.isClosed() {
		t.Fatal("connection kept past MaxConnAge")
	}
	n.clk.advance(reconnectGap)
	air.announce()
	eventually(t, "fresh connection", func() bool { return n.l.Stats().Dialed == 1 })
}

// Stop must really stop: no advertisement, no scanning, every connection
// closed, the radio brought down. The link must also start again cleanly.
func TestStopTearsEverythingDownAndRestarts(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", 0)
	p := startNode(t, air, "p", never)
	air.announce()
	eventually(t, "dial", func() bool { return n.l.Stats().Dialed == 1 })

	if err := n.l.Stop(); err != nil {
		t.Fatal(err)
	}
	n.r.mu.Lock()
	up, adv, scanning, downs := n.r.up, n.r.advertising, n.r.scanning, n.r.downs
	n.r.mu.Unlock()
	if up || adv || scanning || downs != 1 {
		t.Fatalf("after Stop: up=%v adv=%v scanning=%v downs=%d", up, adv, scanning, downs)
	}
	if n.r.openConns() != 0 {
		t.Fatal("a connection survived Stop")
	}
	if p.l.Stats().Subscribed {
		t.Fatal("peer still sees our subscription")
	}
	if err := n.l.Send(context.Background(), []byte{1}); !errors.Is(err, ErrNotRunning) {
		t.Fatal("send after stop")
	}
	if err := n.l.Stop(); err != nil {
		t.Fatal("second Stop:", err)
	}
	n.r.mu.Lock()
	downs = n.r.downs
	n.r.mu.Unlock()
	if downs != 1 {
		t.Fatal("second Stop touched the radio again")
	}

	if err := n.l.Start(context.Background(), n.in.add); err != nil {
		t.Fatal(err)
	}
	air.announce()
	eventually(t, "redial after restart", func() bool { return n.l.Stats().Dialed == 1 })
}

func TestStartFailureLeavesNothingRunning(t *testing.T) {
	air := newFakeAir()
	r := air.radio("x")
	r.failUp = errors.New("adapter is rfkilled")
	l := New(r)
	if err := l.Start(context.Background(), func([]byte) {}); err == nil {
		t.Fatal("start succeeded")
	}
	if err := l.Send(context.Background(), []byte{1}); !errors.Is(err, ErrNotRunning) {
		t.Fatal("link running after failed start")
	}
}

// Cancelling the context the link was started with tears it down even if
// nobody calls Stop.
func TestContextEndStopsLink(t *testing.T) {
	air := newFakeAir()
	r := air.radio("x")
	l := New(r)
	ctx, cancel := context.WithCancel(context.Background())
	if err := l.Start(ctx, func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	cancel()
	eventually(t, "teardown on ctx end", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.downs == 1 && !r.up
	})
}

func TestDialCapRespected(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "hub", 0)
	for i := 0; i < maxPeers+3; i++ {
		startNode(t, air, fmt.Sprintf("p%d", i), never)
	}
	air.announce()
	eventually(t, "dials", func() bool { return n.l.Stats().Dialed == maxPeers })
	time.Sleep(20 * time.Millisecond)
	if d := n.l.Stats().Dialed; d != maxPeers {
		t.Fatalf("dialed %d, cap is %d", d, maxPeers)
	}
}

func TestJitterStaysWithinTenPercent(t *testing.T) {
	for i := 0; i < 1000; i++ {
		d := jitter(MaxConnAge)
		if d < MaxConnAge*9/10 || d > MaxConnAge*11/10 {
			t.Fatalf("jitter %v outside ±10%%", d)
		}
	}
}

// The whole stack over fake BLE: three Managers in a line, A–B–C, where A and
// C cannot hear each other. A message A sends reaches C through B's relay,
// once, as the bytes A sent.
func TestManagersRelayAcrossBLEMesh(t *testing.T) {
	air := newFakeAir()
	air.apart[[2]string{"A", "C"}] = true
	const topic = "concord/guild/test"
	type got struct {
		mu sync.Mutex
		p  [][]byte
	}
	var recv [3]got
	var mgrs [3]*transport.Manager
	for i, id := range []string{"A", "B", "C"} {
		link := New(air.radio(id))
		link.dialDelay = func() time.Duration { return 0 }
		mgrs[i] = transport.NewManager(filepath.Join(t.TempDir(), "t.json"), func(_ string, p []byte) {
			recv[i].mu.Lock()
			recv[i].p = append(recv[i].p, p)
			recv[i].mu.Unlock()
		})
		mgrs[i].Register(link)
		mgrs[i].Start(context.Background())
		if i != 1 {
			mgrs[i].Carry(topic)
		}
		if err := mgrs[i].SetEnabled("ble", true); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(mgrs[i].Close)
	}
	air.announce()
	eventually(t, "A–B and B–C links", func() bool {
		return air.linked("A", "B") && air.linked("B", "C") && !air.linked("A", "C")
	})

	payload := bytes.Repeat([]byte("ciphertext-"), 40) // more than one frame, so it fragments
	mgrs[0].Send(topic, payload)
	eventually(t, "C to receive via B", func() bool {
		recv[2].mu.Lock()
		defer recv[2].mu.Unlock()
		return len(recv[2].p) > 0
	})
	time.Sleep(50 * time.Millisecond)
	recv[2].mu.Lock()
	defer recv[2].mu.Unlock()
	if len(recv[2].p) != 1 || !bytes.Equal(recv[2].p[0], payload) {
		t.Fatalf("C got %d payloads", len(recv[2].p))
	}
}
