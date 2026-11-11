package meshtastic_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/meshtastic"
	"github.com/ZahakJ/concord/internal/transport/meshtastic/meshsim"
)

func TestFrameReaderSkipsNoiseAndBadHeaders(t *testing.T) {
	pb := (&meshtastic.ToRadio{WantConfigID: 7}).Marshal()
	f, err := meshtastic.EncodeFrame(pb)
	if err != nil {
		t.Fatal(err)
	}
	var stream []byte
	stream = append(stream, "boot log\r\n"...)
	stream = append(stream, 0x94, 0x94, 0xC3, 0xFF, 0xFF) // START, then a length > 512
	stream = append(stream, 0x94, 0x41)                   // START1 then junk
	stream = append(stream, f...)
	stream = append(stream, "more log"...)
	stream = append(stream, f...)
	fr := meshtastic.NewFrameReader(bytes.NewReader(stream))
	for i := 0; i < 2; i++ {
		got, err := fr.Next()
		if err != nil || !bytes.Equal(got, pb) {
			t.Fatalf("frame %d: %x, %v", i, got, err)
		}
	}
	if _, err := fr.Next(); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
	if fr.Noise == 0 {
		t.Fatal("noise not counted")
	}
	if _, err := meshtastic.EncodeFrame(make([]byte, 513)); err == nil {
		t.Fatal("encoded an oversized frame")
	}
}

func TestTruncatedFrameEndsWithError(t *testing.T) {
	fr := meshtastic.NewFrameReader(bytes.NewReader([]byte{0x94, 0xC3, 0x00, 0x10, 1, 2, 3}))
	if _, err := fr.Next(); err == nil {
		t.Fatal("truncated frame accepted")
	}
}

func TestProtoRoundTrip(t *testing.T) {
	p := &meshtastic.MeshPacket{From: 0xdeadbeef, To: meshtastic.BroadcastAddr, Channel: 2, ID: 99, HopLimit: 3, HopStart: 3,
		WantAck: true, RxSNR: -7.25, RxRSSI: -110, Decoded: true, PortNum: 256, Payload: []byte{0xC1, 1, 2, 3}}
	got, err := meshtastic.UnmarshalMeshPacket(p.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if got.From != p.From || got.To != p.To || got.Channel != 2 || got.ID != 99 || got.HopLimit != 3 || !got.WantAck ||
		got.RxSNR != -7.25 || got.RxRSSI != -110 || !got.Decoded || got.PortNum != 256 || !bytes.Equal(got.Payload, p.Payload) {
		t.Fatalf("round trip: %+v", got)
	}
	tr, err := meshtastic.UnmarshalToRadio((&meshtastic.ToRadio{Packet: p}).Marshal())
	if err != nil || tr.Packet == nil || !bytes.Equal(tr.Packet.Payload, p.Payload) {
		t.Fatalf("ToRadio: %+v %v", tr, err)
	}
	for _, m := range []*meshtastic.FromRadio{
		{ID: 5, HasMyInfo: true, MyNodeNum: 0x1234},
		{NodeInfo: &meshtastic.NodeInfo{Num: 0x1234, LongName: "base", IsLicensed: true}},
		{HasConfigDone: true, ConfigCompleteID: 42},
		{QueueStatus: &meshtastic.QueueStatus{Res: 0, Free: 3, MaxLen: 16, PacketID: 9}},
		{Packet: p},
	} {
		got, err := meshtastic.UnmarshalFromRadio(m.Marshal())
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case m.HasMyInfo && (got.MyNodeNum != 0x1234 || got.ID != 5):
			t.Fatalf("my_info: %+v", got)
		case m.NodeInfo != nil && (got.NodeInfo == nil || *got.NodeInfo != *m.NodeInfo):
			t.Fatalf("node_info: %+v", got.NodeInfo)
		case m.HasConfigDone && got.ConfigCompleteID != 42:
			t.Fatalf("config_complete: %+v", got)
		case m.QueueStatus != nil && (got.QueueStatus == nil || *got.QueueStatus != *m.QueueStatus):
			t.Fatalf("queue: %+v", got.QueueStatus)
		case m.Packet != nil && (got.Packet == nil || got.Packet.ID != 99):
			t.Fatalf("packet: %+v", got.Packet)
		}
	}
}

// Whatever comes off a serial line, the decoders must neither panic nor
// hang.
func FuzzDecoders(f *testing.F) {
	f.Add((&meshtastic.FromRadio{Packet: &meshtastic.MeshPacket{Decoded: true, Payload: []byte("x")}}).Marshal())
	f.Add([]byte{0x94, 0xC3, 0x00, 0x02, 0x12, 0x00})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = meshtastic.UnmarshalFromRadio(b)
		_, _ = meshtastic.UnmarshalToRadio(b)
		fr := meshtastic.NewFrameReader(bytes.NewReader(b))
		for {
			if _, err := fr.Next(); err != nil {
				break
			}
		}
	})
}

func TestGarbageDoesNotPanic(t *testing.T) {
	for i := 0; i < 2000; i++ {
		b := make([]byte, i%300)
		_, _ = rand.Read(b)
		_, _ = meshtastic.UnmarshalFromRadio(b)
		fr := meshtastic.NewFrameReader(bytes.NewReader(b))
		for {
			if _, err := fr.Next(); err != nil {
				break
			}
		}
	}
}

// pipeDial connects a Link to a simulated node in memory.
func pipeDial(n *meshsim.Node) meshtastic.Dialer {
	return func(context.Context) (meshtastic.Conn, error) { return n.Pipe(), nil }
}

func TestHandshakeLearnsNodeNumber(t *testing.T) {
	mesh := meshsim.New(1)
	defer mesh.Close()
	n := mesh.AddNode(0xa1b2c3d4, "alpha")
	n.Noise = true
	l := meshtastic.New(meshtastic.Config{}, pipeDial(n))
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	if st := l.Probe(); st.State != transport.Available || !strings.Contains(st.Detail, "a1b2c3d4") {
		t.Fatalf("probe: %+v", st)
	}
}

func TestLicensedNodeIsRefused(t *testing.T) {
	mesh := meshsim.New(1)
	defer mesh.Close()
	n := mesh.AddNode(7, "ham")
	n.Licensed = true
	l := meshtastic.New(meshtastic.Config{}, pipeDial(n))
	err := l.Start(context.Background(), func([]byte) {})
	if !errors.Is(err, meshtastic.ErrLicensed) {
		t.Fatalf("want ErrLicensed, got %v", err)
	}
	if err := l.Send(context.Background(), []byte{0xC1}); err == nil {
		t.Fatal("sent through a refused node")
	}
}

func TestProbeNeverDials(t *testing.T) {
	dialed := false
	l := meshtastic.New(meshtastic.Config{Device: "tcp://203.0.113.1"}, nil)
	_ = l.Probe()
	l2 := meshtastic.New(meshtastic.Config{}, func(context.Context) (meshtastic.Conn, error) { dialed = true; return nil, io.EOF })
	_ = l2.Probe()
	if dialed {
		t.Fatal("Probe opened the node")
	}
	if st := l.Probe(); st.State != transport.Available || !strings.Contains(st.Detail, "203.0.113.1:4403") {
		t.Fatalf("probe: %+v", st)
	}
}

type inbox struct {
	mu   sync.Mutex
	got  [][]byte
	cond chan struct{}
}

func newInbox() *inbox { return &inbox{cond: make(chan struct{}, 1)} }

func (b *inbox) add(_ string, p []byte) {
	b.mu.Lock()
	b.got = append(b.got, append([]byte(nil), p...))
	b.mu.Unlock()
	select {
	case b.cond <- struct{}{}:
	default:
	}
}

func (b *inbox) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.got)
}

func (b *inbox) waitFor(t *testing.T, n int, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for b.count() < n {
		select {
		case <-b.cond:
		case <-deadline:
			t.Fatalf("got %d messages, want %d", b.count(), n)
		}
	}
}

func manager(t *testing.T, l transport.Transport, in *inbox) *transport.Manager {
	t.Helper()
	m := transport.NewManager("", in.add)
	m.Register(l)
	m.Start(context.Background())
	if err := m.SetEnabled("meshtastic", true); err != nil {
		t.Fatal(err)
	}
	if v := m.Links()[0]; !v.Running {
		t.Fatalf("link not running: %+v", v)
	}
	m.Carry("topic")
	t.Cleanup(m.Close)
	return m
}

// Two Concord managers, each on its own node over the real TCP client path,
// with a relay node between them: a ciphertext larger than one LoRa frame is
// fragmented, flooded two hops and reassembled exactly once.
func TestEndToEndAcrossSimulatedMesh(t *testing.T) {
	mesh := meshsim.New(2)
	defer mesh.Close()
	mesh.Latency, mesh.Jitter = 10*time.Millisecond, 10*time.Millisecond
	a, relay, b := mesh.AddNode(1, "a"), mesh.AddNode(2, "relay"), mesh.AddNode(3, "b")
	mesh.Connect(a, relay)
	mesh.Connect(relay, b) // a and b are out of range of each other
	addrA, err := a.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addrB, err := b.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	inA, inB := newInbox(), newInbox()
	ma := manager(t, meshtastic.New(meshtastic.Config{Device: "tcp://" + addrA}, nil), inA)
	mb := manager(t, meshtastic.New(meshtastic.Config{Device: "tcp:" + addrB}, nil), inB)

	payload := make([]byte, 900) // stands in for an MLS ciphertext: 5 frames
	_, _ = rand.Read(payload)
	ma.Send("topic", payload)
	inB.waitFor(t, 1, 5*time.Second)
	if !bytes.Equal(inB.got[0], payload) {
		t.Fatal("payload changed in transit")
	}
	reply := []byte("short ciphertext stand-in")
	mb.Send("topic", reply)
	inA.waitFor(t, 1, 5*time.Second)
	time.Sleep(200 * time.Millisecond)
	if inA.count() != 1 || inB.count() != 1 {
		t.Fatalf("duplicates: a=%d b=%d", inA.count(), inB.count())
	}
}

// On a lossy channel some bundles lose a fragment and never complete; what
// does arrive must be exact, once, and the link must keep running.
func TestLossyMesh(t *testing.T) {
	mesh := meshsim.New(3)
	defer mesh.Close()
	mesh.Loss = 0.15
	mesh.Latency = 2 * time.Millisecond
	a, b := mesh.AddNode(1, "a"), mesh.AddNode(2, "b")
	inA, inB := newInbox(), newInbox()
	ma := manager(t, meshtastic.New(meshtastic.Config{HopLimit: 1}, pipeDial(a)), inA)
	_ = manager(t, meshtastic.New(meshtastic.Config{HopLimit: 1}, pipeDial(b)), inB)
	want := map[string]bool{}
	for i := 0; i < 30; i++ {
		p := make([]byte, 400) // 3 frames each
		_, _ = rand.Read(p)
		want[string(p)] = true
		ma.Send("topic", p)
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	inB.mu.Lock()
	defer inB.mu.Unlock()
	seen := map[string]bool{}
	for _, g := range inB.got {
		if !want[string(g)] || seen[string(g)] {
			t.Fatal("corrupt or duplicate delivery")
		}
		seen[string(g)] = true
	}
	if len(seen) == 0 || len(seen) == 30 {
		t.Fatalf("delivered %d/30 at 15%% loss; expected some but not all", len(seen))
	}
	if !ma.Links()[0].Running {
		t.Fatal("link stopped under loss")
	}
}

// Stop must disconnect from the node and leave nothing running; the node
// must see the client go.
func TestStopReleasesNode(t *testing.T) {
	mesh := meshsim.New(4)
	defer mesh.Close()
	n := mesh.AddNode(1, "a")
	addr, err := n.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := meshtastic.New(meshtastic.Config{Device: "tcp://" + addr}, nil)
	for i := 0; i < 3; i++ { // enable, disable, enable again
		if err := l.Start(context.Background(), func([]byte) {}); err != nil {
			t.Fatal(err)
		}
		if err := l.Stop(); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Send(context.Background(), []byte{0xC1, 0}); err == nil {
		t.Fatal("sent after Stop")
	}
}

// When the node drops the connection (USB unplugged, node rebooted) the link
// reconnects on its own.
func TestReconnectAfterDrop(t *testing.T) {
	mesh := meshsim.New(5)
	defer mesh.Close()
	n := mesh.AddNode(1, "a")
	var mu sync.Mutex
	var conns []io.ReadWriteCloser
	dial := func(context.Context) (meshtastic.Conn, error) {
		c := n.Pipe()
		mu.Lock()
		conns = append(conns, c)
		mu.Unlock()
		return c, nil
	}
	l := meshtastic.New(meshtastic.Config{}, dial)
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	mu.Lock()
	conns[0].Close()
	mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		k := len(conns)
		mu.Unlock()
		if k >= 2 && l.Send(context.Background(), []byte{0xC1, 1}) == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("did not reconnect")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The node's band decides whether ciphertext is lawful; anything Concord
// cannot positively clear is refused.
func TestRegionChecks(t *testing.T) {
	cases := []struct {
		name     string
		region   uint32
		override float32
		allow433 bool
		hideSelf bool
		ok       bool
	}{
		{"US 915", 1, 0, false, false, true},
		{"EU 868", 3, 0, false, false, true},
		{"unset", 0, 0, false, false, false},
		{"ITU2 70cm amateur", 35, 0, false, false, false},
		{"EU_433 by default", 2, 0, false, false, false},
		{"EU_433 acknowledged", 2, 0, true, false, true},
		{"override into 2 m", 1, 146.52, false, false, false},
		{"override inside ISM", 1, 915.0, false, false, true},
		{"unknown region", 99, 0, false, false, false},
		{"no own NodeInfo", 1, 0, false, true, false},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mesh := meshsim.New(int64(i))
			defer mesh.Close()
			n := mesh.AddNode(uint32(100+i), "n")
			n.Region, n.OverrideMHz, n.HideSelf = c.region, c.override, c.hideSelf
			l := meshtastic.New(meshtastic.Config{Allow433: c.allow433}, pipeDial(n))
			err := l.Start(context.Background(), func([]byte) {})
			if err == nil {
				defer l.Stop()
			}
			if (err == nil) != c.ok {
				t.Fatalf("ok=%v, err=%v", c.ok, err)
			}
			if !c.ok && !errors.Is(err, meshtastic.ErrRegion) && !errors.Is(err, meshtastic.ErrLicensed) {
				t.Fatalf("refused for the wrong reason: %v", err)
			}
		})
	}
}

func TestLoRaConfigRoundTrip(t *testing.T) {
	m := &meshtastic.FromRadio{LoRa: &meshtastic.LoRaConfig{Region: 3, OverrideFrequency: 869.525, TxEnabled: true}}
	got, err := meshtastic.UnmarshalFromRadio(m.Marshal())
	if err != nil || got.LoRa == nil || *got.LoRa != *m.LoRa {
		t.Fatalf("%+v %v", got.LoRa, err)
	}
}
