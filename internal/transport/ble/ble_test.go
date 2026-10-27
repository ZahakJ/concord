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

func startNode(t *testing.T, air *fakeAir, id string, n nonce) *node {
	t.Helper()
	r := air.radio(id)
	l := New(r)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	l.now = clk.now
	in := &inbox{}
	if err := l.Start(context.Background(), in.add); err != nil {
		t.Fatal(err)
	}
	// Pin the nonce so tests decide who dials.
	l.mu.Lock()
	l.nonce = n
	l.mu.Unlock()
	if err := r.Advertise(encodeAd(n)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Stop() })
	return &node{l, r, clk, in}
}

func TestAdvertisementCarriesOnlyVersionAndNonce(t *testing.T) {
	a, b := newNonce(), newNonce()
	if a == b {
		t.Fatal("two fresh nonces were equal")
	}
	ad := encodeAd(a)
	if len(ad) != 5 || ad[0] != adVersion || !bytes.Equal(ad[1:], a[:]) {
		t.Fatalf("ad = %x", ad)
	}
	if n, ok := decodeAd(ad); !ok || !bytes.Equal(n, a[:]) {
		t.Fatal("round trip failed")
	}
	if n, ok := decodeAd(nil); !ok || n != nil {
		t.Fatal("UUID-only advertiser should decode to no nonce")
	}
	for _, bad := range [][]byte{{}, {2, 1, 2, 3, 4}, {1, 2, 3}, {1, 2, 3, 4, 5, 6}} {
		if _, ok := decodeAd(bad); ok {
			t.Errorf("accepted foreign service data %x", bad)
		}
	}
}

// Two devices in range end up with exactly one connection, dialed by the
// lower nonce, and frames cross it once in each direction.
func TestPairMakesOneConnectionAndCarriesFramesBothWays(t *testing.T) {
	air := newFakeAir()
	lo := startNode(t, air, "lo", nonce{0, 0, 0, 1})
	hi := startNode(t, air, "hi", nonce{9, 9, 9, 9})
	air.announce()
	eventually(t, "lo to dial hi", func() bool { return lo.l.Stats().Dialed == 1 })
	air.announce() // hi now sees the link exists and must not dial back
	hi.l.tick()
	time.Sleep(20 * time.Millisecond)
	if n := hi.r.openConns(); n != 0 {
		t.Fatalf("higher nonce dialed %d connections, want 0", n)
	}
	if !hi.l.Stats().Subscribed {
		t.Fatal("hi should have a subscribed central")
	}

	f1, f2 := []byte("\xc1frame-lo-to-hi"), []byte("\xc1frame-hi-to-lo")
	if err := lo.l.Send(context.Background(), f1); err != nil {
		t.Fatal(err)
	}
	if err := hi.l.Send(context.Background(), f2); err != nil {
		t.Fatal(err)
	}
	if hi.in.count(f1) != 1 || lo.in.count(f2) != 1 {
		t.Fatalf("hi got %d, lo got %d; want 1 each", hi.in.count(f1), lo.in.count(f2))
	}
	if lo.in.count(f1) != 0 || hi.in.count(f2) != 0 {
		t.Fatal("a frame echoed back to its sender")
	}
}

// A higher-nonce peer that never dials us (an iOS phone in the background, a
// full controller) is dialed after the fallback window.
func TestHigherNoncePeerIsDialedAfterFallback(t *testing.T) {
	air := newFakeAir()
	hi := startNode(t, air, "hi", nonce{9, 9, 9, 9})
	// A peripheral-only peer with a lower nonce that will never dial.
	shy := air.radio("shy")
	if err := shy.Up(context.Background(), Events{
		Inbound: func([]byte) {}, Seen: func(string, []byte, bool) {}, Lost: func(string) {},
		Notified: func(string, []byte) {}, Subscribed: func(bool) {},
	}); err != nil {
		t.Fatal(err)
	}
	_ = shy.Advertise(encodeAd(nonce{0, 0, 0, 0}))

	air.announce()
	hi.l.tick()
	time.Sleep(20 * time.Millisecond)
	if hi.l.Stats().Dialed != 0 {
		t.Fatal("dialed a lower-nonce peer before the fallback window")
	}
	hi.clk.advance(fallbackAfter + time.Second)
	air.announce()
	eventually(t, "fallback dial", func() bool { return hi.l.Stats().Dialed == 1 })
}

func TestPeerWithoutNonceIsDialed(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", nonce{9, 9, 9, 9})
	n.l.seen("ios", nil, false)
	// "ios" does not exist on the air, so the dial fails and backs off; what
	// matters is that it was attempted at once.
	eventually(t, "dial attempt", func() bool {
		n.l.mu.Lock()
		defer n.l.mu.Unlock()
		p := n.l.peers["ios"]
		return p != nil && p.failures == 1
	})
}

func TestForeignServiceDataIsIgnored(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", nonce{0, 0, 0, 0})
	n.l.seen("stranger", []byte{7, 7}, false)
	if n.l.Stats().Seen != 0 {
		t.Fatal("tracked a device whose service data is not ours")
	}
}

func TestRotationReplacesTheNonce(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", nonce{1, 2, 3, 4})
	before := len(n.r.ads)
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		if err := n.l.Rotate(); err != nil {
			t.Fatal(err)
		}
		n.r.mu.Lock()
		ad := n.r.ad
		n.r.mu.Unlock()
		if len(ad) != adLen {
			t.Fatalf("advertised %d bytes", len(ad))
		}
		if seen[string(ad)] {
			t.Fatal("a rotation repeated an advertisement")
		}
		seen[string(ad)] = true
		if !bytes.Equal(ad[1:], n.l.Stats().Nonce) {
			t.Fatal("advertised nonce is not the link's nonce")
		}
	}
	if len(n.r.ads) != before+5 {
		t.Fatalf("expected 5 new advertisements, got %d", len(n.r.ads)-before)
	}
	for i := 0; i < 1000; i++ {
		d := jitter(RotateEvery)
		if d < RotateEvery*9/10 || d > RotateEvery*11/10 {
			t.Fatalf("jitter %v outside ±10%%", d)
		}
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

func TestInboundRejectsOversizeAndEmpty(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", nonce{})
	n.l.inbound(nil)
	n.l.inbound(make([]byte, MaxFrame+1))
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
	lo := startNode(t, air, "lo", nonce{0, 0, 0, 1})
	startNode(t, air, "hi", nonce{9, 9, 9, 9})
	air.announce()
	eventually(t, "dial", func() bool { return lo.l.Stats().Dialed == 1 })
	lo.r.mu.Lock()
	c := lo.r.conns[0]
	lo.r.mu.Unlock()
	c.mu.Lock()
	c.failNext = errors.New("gatt write failed")
	c.mu.Unlock()
	if err := lo.l.Send(context.Background(), []byte{0xc1}); err == nil {
		t.Fatal("send succeeded with every peer failing")
	}
	if lo.l.Stats().Dialed != 0 || !c.isClosed() {
		t.Fatal("failed peer was not dropped")
	}
	air.announce()
	lo.l.tick()
	time.Sleep(20 * time.Millisecond)
	if lo.l.Stats().Dialed != 0 {
		t.Fatal("redialed inside the backoff window")
	}
	lo.clk.advance(backoff(1) + time.Second)
	air.announce()
	eventually(t, "redial after backoff", func() bool { return lo.l.Stats().Dialed == 1 })
}

func TestLostPeerIsForgottenWhenStale(t *testing.T) {
	air := newFakeAir()
	n := startNode(t, air, "n", nonce{9, 9, 9, 9})
	n.l.seen("ghost", encodeAd(nonce{0, 0, 0, 0}), false) // lower: it dials, we wait
	if n.l.Stats().Seen != 1 {
		t.Fatal("not tracked")
	}
	n.clk.advance(staleAfter + time.Second)
	n.l.tick()
	if n.l.Stats().Seen != 0 {
		t.Fatal("stale device kept")
	}
}

// Stop must really stop: no advertisement, no scanning, every connection
// closed, the radio brought down — and the link must start again cleanly.
func TestStopTearsEverythingDownAndRestarts(t *testing.T) {
	air := newFakeAir()
	lo := startNode(t, air, "lo", nonce{0, 0, 0, 1})
	hi := startNode(t, air, "hi", nonce{9, 9, 9, 9})
	air.announce()
	eventually(t, "dial", func() bool { return lo.l.Stats().Dialed == 1 })

	if err := lo.l.Stop(); err != nil {
		t.Fatal(err)
	}
	lo.r.mu.Lock()
	up, ad, scanning, downs := lo.r.up, lo.r.ad, lo.r.scanning, lo.r.downs
	lo.r.mu.Unlock()
	if up || ad != nil || scanning || downs != 1 {
		t.Fatalf("after Stop: up=%v ad=%x scanning=%v downs=%d", up, ad, scanning, downs)
	}
	if lo.r.openConns() != 0 {
		t.Fatal("a connection survived Stop")
	}
	if hi.l.Stats().Subscribed {
		t.Fatal("peer still sees our subscription")
	}
	if err := lo.l.Send(context.Background(), []byte{1}); !errors.Is(err, ErrNotRunning) {
		t.Fatal("send after stop")
	}
	if err := lo.l.Stop(); err != nil {
		t.Fatal("second Stop:", err)
	}
	lo.r.mu.Lock()
	downs = lo.r.downs
	lo.r.mu.Unlock()
	if downs != 1 {
		t.Fatal("second Stop touched the radio again")
	}

	if err := lo.l.Start(context.Background(), lo.in.add); err != nil {
		t.Fatal(err)
	}
	lo.l.mu.Lock()
	lo.l.nonce = nonce{0, 0, 0, 1}
	lo.l.mu.Unlock()
	air.announce()
	eventually(t, "redial after restart", func() bool { return lo.l.Stats().Dialed == 1 })
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
	n := startNode(t, air, "hub", nonce{0, 0, 0, 0})
	for i := 0; i < maxPeers+3; i++ {
		startNode(t, air, fmt.Sprintf("p%d", i), nonce{9, 9, 9, byte(i)})
	}
	air.announce()
	eventually(t, "dials", func() bool { return n.l.Stats().Dialed == maxPeers })
	time.Sleep(20 * time.Millisecond)
	if d := n.l.Stats().Dialed; d != maxPeers {
		t.Fatalf("dialed %d, cap is %d", d, maxPeers)
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
	var links [3]*Link
	var mgrs [3]*transport.Manager
	for i, id := range []string{"A", "B", "C"} {
		links[i] = New(air.radio(id))
		mgrs[i] = transport.NewManager(filepath.Join(t.TempDir(), "t.json"), func(_ string, p []byte) {
			recv[i].mu.Lock()
			recv[i].p = append(recv[i].p, p)
			recv[i].mu.Unlock()
		})
		mgrs[i].Register(links[i])
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

	payload := bytes.Repeat([]byte("ciphertext-"), 40) // > one frame: fragments
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
