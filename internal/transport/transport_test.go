package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var loraCaps = Caps{MTU: 200, Bitrate: 1000, Encryption: true, Local: true, Mode: Live}

func TestFramesRoundTripAtTinyMTU(t *testing.T) {
	for _, mtu := range []int{40, 200, 340, 512} {
		payload := make([]byte, 1500)
		_, _ = rand.Read(payload)
		b := Bundle{Hops: 3, Day: 20000, Tag: TopicTag("t", 20000), Payload: payload}
		frames, err := Frames(b, mtu)
		if err != nil {
			t.Fatalf("mtu %d: %v", mtu, err)
		}
		if len(frames) != FrameCount(len(payload), mtu) {
			t.Fatalf("mtu %d: %d frames, FrameCount says %d", mtu, len(frames), FrameCount(len(payload), mtu))
		}
		r := NewReassembler(4, time.Minute)
		var whole []byte
		// Out of order and with a duplicate, as a lossy mesh delivers them.
		order := append([][]byte{frames[len(frames)-1]}, frames...)
		for _, f := range order {
			if len(f) > mtu {
				t.Fatalf("mtu %d: frame of %d bytes", mtu, len(f))
			}
			if w, err := r.Add(f); err != nil {
				t.Fatal(err)
			} else if w != nil {
				whole = w
			}
		}
		got, err := Unmarshal(whole)
		if err != nil || !bytes.Equal(got.Payload, payload) || got.Tag != b.Tag || got.Hops != 3 {
			t.Fatalf("mtu %d: round trip mismatch (%v)", mtu, err)
		}
	}
}

func TestSmallBundleIsOneFrameWithoutFragmentHeader(t *testing.T) {
	frames, err := Frames(Bundle{Payload: []byte("hi")}, 200)
	if err != nil || len(frames) != 1 || frames[0][0] != kindBundle || len(frames[0]) != bundleHeader+2 {
		t.Fatalf("got %d frames, err %v", len(frames), err)
	}
}

func TestTooLargeForLink(t *testing.T) {
	if _, err := Frames(Bundle{Payload: make([]byte, 255*193+1)}, 200); err != ErrTooLarge {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestReassemblerBounded(t *testing.T) {
	r := NewReassembler(2, time.Minute)
	for i := 0; i < 10; i++ {
		f := []byte{kindFragment, byte(i), 0, 0, 0, 0, 2, 'x'}
		if _, err := r.Add(f); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.sets) > 2 {
		t.Fatalf("holding %d sets, cap 2", len(r.sets))
	}
	for _, bad := range [][]byte{{}, {0x00, 1}, {kindFragment, 1, 2, 3, 4, 5, 2}, {kindFragment, 1, 2, 3, 4, 3, 2, 'x'}} {
		if _, err := r.Add(bad); err == nil {
			t.Fatalf("accepted malformed %x", bad)
		}
	}
}

func TestTagRotatesDaily(t *testing.T) {
	if TopicTag("a", 1) == TopicTag("a", 2) || TopicTag("a", 1) == TopicTag("b", 1) {
		t.Fatal("tags must differ by day and topic")
	}
}

type inbox struct {
	mu  sync.Mutex
	got map[string][][]byte
}

func (i *inbox) deliver(topic string, p []byte) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.got == nil {
		i.got = map[string][][]byte{}
	}
	i.got[topic] = append(i.got[topic], p)
}

func (i *inbox) count(topic string) int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.got[topic])
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func node(t *testing.T, links ...Transport) (*Manager, *inbox) {
	in := &inbox{}
	m := NewManager(filepath.Join(t.TempDir(), "transports.json"), in.deliver)
	for _, l := range links {
		m.Register(l)
		if err := m.SetEnabled(l.Name(), true); err != nil {
			t.Fatal(err)
		}
	}
	m.Start(context.Background())
	t.Cleanup(m.Close)
	return m, in
}

func TestDeliveryAndDedupeAcrossTwoLinks(t *testing.T) {
	ble, lora := NewAir(), NewAir()
	a, _ := node(t, NewMemLink(ble, "ble", "BLE", Caps{MTU: 512, Bitrate: 100000, Encryption: true, Local: true, Mode: Live}),
		NewMemLink(lora, "lora", "LoRa", loraCaps))
	b, in := node(t, NewMemLink(ble, "ble", "BLE", Caps{MTU: 512, Bitrate: 100000, Encryption: true, Local: true, Mode: Live}),
		NewMemLink(lora, "lora", "LoRa", loraCaps))
	a.Carry("chan")
	b.Carry("chan")
	ct := bytes.Repeat([]byte{7}, 600)
	a.Send("chan", ct)
	waitFor(t, "delivery", func() bool { return in.count("chan") >= 1 })
	time.Sleep(50 * time.Millisecond)
	if n := in.count("chan"); n != 1 {
		t.Fatalf("delivered %d times over two links, want once", n)
	}
	// The same ciphertext later arriving by gossipsub is not new either.
	if b.Observe(ct) {
		t.Fatal("gossipsub copy of an already-delivered message counted as new")
	}
}

func TestUncarriedTopicIsNeitherSentNorDelivered(t *testing.T) {
	air := NewAir()
	a, _ := node(t, NewMemLink(air, "ble", "BLE", loraCaps))
	_, in := node(t, NewMemLink(air, "ble", "BLE", loraCaps))
	a.Send("typing", []byte("x"))
	time.Sleep(50 * time.Millisecond)
	if in.count("typing") != 0 {
		t.Fatal("uncarried topic crossed the air")
	}
}

func TestMultiHopRelayThroughNonMember(t *testing.T) {
	// a and c are out of range of each other; b sits between and is not in
	// the channel. b must carry the ciphertext without being able to use it.
	left, right := NewAir(), NewAir()
	a, _ := node(t, NewMemLink(left, "ble", "BLE", loraCaps))
	b, bin := node(t, NewMemLink(left, "ble", "BLE", loraCaps), NewMemLink(right, "ble2", "BLE", loraCaps))
	c, cin := node(t, NewMemLink(right, "ble", "BLE", loraCaps))
	a.Carry("chan")
	c.Carry("chan")
	_ = b
	a.Send("chan", []byte("ciphertext"))
	waitFor(t, "relayed delivery", func() bool { return cin.count("chan") == 1 })
	if bin.count("chan") != 0 {
		t.Fatal("relay delivered a topic it does not carry")
	}
}

func TestRelayOffStopsForwarding(t *testing.T) {
	left, right := NewAir(), NewAir()
	a, _ := node(t, NewMemLink(left, "ble", "BLE", loraCaps))
	b, _ := node(t, NewMemLink(left, "ble", "BLE", loraCaps), NewMemLink(right, "ble2", "BLE", loraCaps))
	c, cin := node(t, NewMemLink(right, "ble", "BLE", loraCaps))
	_ = b.SetRelay(false)
	a.Carry("chan")
	c.Carry("chan")
	a.Send("chan", []byte("ciphertext"))
	time.Sleep(80 * time.Millisecond)
	if cin.count("chan") != 0 {
		t.Fatal("relay disabled but message crossed")
	}
}

func TestHopLimit(t *testing.T) {
	// A chain of DefaultHops+2 airs: the message reaches DefaultHops relays
	// past the first hop and no further.
	n := DefaultHops + 3
	airs := make([]*Air, n)
	for i := range airs {
		airs[i] = NewAir()
	}
	src, _ := node(t, NewMemLink(airs[0], "l", "l", loraCaps))
	src.Carry("chan")
	ins := make([]*inbox, n)
	for i := 1; i < n; i++ {
		links := []Transport{NewMemLink(airs[i-1], "l", "l", loraCaps)}
		if i+1 < n {
			links = append(links, NewMemLink(airs[i], "r", "r", loraCaps))
		}
		m, in := node(t, links...)
		m.Carry("chan")
		ins[i] = in
	}
	src.Send("chan", []byte("ct"))
	waitFor(t, "last reachable hop", func() bool { return ins[DefaultHops+1].count("chan") == 1 })
	time.Sleep(80 * time.Millisecond)
	if ins[DefaultHops+2].count("chan") != 0 {
		t.Fatal("bundle travelled past its hop limit")
	}
}

func TestPaidLinkQueuesUntilApproved(t *testing.T) {
	air := NewAir()
	sat := Caps{MTU: 340, Bitrate: 300, CostMicroUSD: 40000, Encryption: true, Mode: Live}
	a, _ := node(t, NewMemLink(air, "sbd", "Iridium", sat))
	_, in := node(t, NewMemLink(air, "sbd", "Iridium", sat))
	a.Carry("chan")
	in.mu.Lock()
	in.mu.Unlock()
	a.Send("chan", make([]byte, 500))
	time.Sleep(50 * time.Millisecond)
	if in.count("chan") != 0 {
		t.Fatal("paid link sent without approval")
	}
	v := a.Links()[0]
	if v.Queued != 2 || v.QueuedCost != 80000 {
		t.Fatalf("queued %d frames costing %d, want 2 / 80000", v.Queued, v.QueuedCost)
	}
}

func TestPaidLinkFlushDelivers(t *testing.T) {
	air := NewAir()
	sat := Caps{MTU: 340, Bitrate: 300, CostMicroUSD: 40000, Encryption: true, Mode: Live}
	a, _ := node(t, NewMemLink(air, "sbd", "Iridium", sat))
	b, in := node(t, NewMemLink(air, "sbd", "Iridium", sat))
	a.Carry("chan")
	b.Carry("chan")
	a.Send("chan", make([]byte, 500))
	n, cost, err := a.Flush("sbd")
	if err != nil || n != 2 || cost != 80000 {
		t.Fatalf("flush: %d frames, %d µ$, %v", n, cost, err)
	}
	waitFor(t, "delivery after approval", func() bool { return in.count("chan") == 1 })
}

func TestCiphertextNeverOnPlaintextOnlyBands(t *testing.T) {
	air := NewAir()
	ham := Caps{MTU: 256, Bitrate: 1200, Encryption: false, Local: true, Mode: Live}
	a, _ := node(t, NewMemLink(air, "ax25", "Ham", ham))
	_, in := node(t, NewMemLink(air, "ax25", "Ham", ham))
	a.Carry("chan")
	a.Send("chan", []byte("ct"))
	time.Sleep(50 * time.Millisecond)
	if in.count("chan") != 0 || a.Links()[0].Queued != 0 {
		t.Fatal("ciphertext went out on a band that forbids it")
	}
}

func TestOfflineModeKeepsOnlyLocalLinks(t *testing.T) {
	air1, air2 := NewAir(), NewAir()
	sat := Caps{MTU: 340, Bitrate: 300, Encryption: true, Mode: Live} // free for the test, not Local
	a, _ := node(t, NewMemLink(air1, "ble", "BLE", loraCaps), NewMemLink(air2, "sat", "Sat", sat))
	if err := a.SetOffline(true); err != nil {
		t.Fatal(err)
	}
	for _, v := range a.Links() {
		if v.Name == "sat" && v.Running {
			t.Fatal("non-local link still running in offline mode")
		}
		if v.Name == "ble" && !v.Running {
			t.Fatal("local link stopped by offline mode")
		}
	}
}

func TestLinksStartOffAndSettingsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transports.json")
	m := NewManager(path, nil)
	m.Register(NewMemLink(NewAir(), "ble", "BLE", loraCaps))
	m.Start(context.Background())
	if m.Links()[0].Running {
		t.Fatal("a link started without the user enabling it")
	}
	_ = m.SetEnabled("ble", true)
	_ = m.SetOffline(true)
	m.Close()
	again := NewManager(path, nil)
	if s := again.Settings(); !s.Enabled["ble"] || !s.Offline {
		t.Fatalf("settings not persisted: %+v", s)
	}
}

func TestSlowLiveLinkSkippedForLongMessages(t *testing.T) {
	air := NewAir()
	audio := Caps{MTU: 64, Bitrate: 64, Encryption: true, Local: true, Mode: Live} // 8 B/s
	a, _ := node(t, NewMemLink(air, "slow", "slow", audio))
	_, in := node(t, NewMemLink(air, "slow", "slow", audio))
	a.Carry("chan")
	a.Send("chan", make([]byte, 2000))
	time.Sleep(50 * time.Millisecond)
	if in.count("chan") != 0 {
		t.Fatal("a 4-minute transmission went out automatically")
	}
}

func TestStaleBundleDropped(t *testing.T) {
	m, in := node(t)
	m.Carry("chan")
	old := Today(time.Now()) - MaxAgeDays - 1
	f := Bundle{Hops: 1, Day: old, Tag: TopicTag("chan", old), Payload: []byte("x")}.Marshal()
	m.Receive("import", f)
	if in.count("chan") != 0 {
		t.Fatal("stale bundle delivered")
	}
	today := Today(time.Now())
	m.Receive("import", Bundle{Day: today, Tag: TopicTag("chan", today), Payload: []byte("y")}.Marshal())
	if in.count("chan") != 1 {
		t.Fatal("fresh imported bundle not delivered")
	}
}

func TestFilePackRoundTrip(t *testing.T) {
	src, _ := node(t, FileLink{})
	dst, in := node(t)
	src.Carry("chan")
	dst.Carry("chan")
	src.Send("chan", bytes.Repeat([]byte{1}, 900))
	src.Send("chan", []byte("second"))
	frames, err := src.Export("file")
	if err != nil || len(frames) != 2 {
		t.Fatalf("export: %d frames, %v", len(frames), err)
	}
	got, err := DecodePack(EncodePack(frames))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		dst.Receive("file", f)
	}
	if in.count("chan") != 2 {
		t.Fatalf("imported %d messages, want 2", in.count("chan"))
	}
	for _, bad := range [][]byte{nil, []byte("nope"), append(append([]byte{}, packMagic...), 0x05, 1)} {
		if _, err := DecodePack(bad); err == nil {
			t.Fatalf("accepted bad pack %q", bad)
		}
	}
}
