package audio

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// These run against the machine's real sound system and are skipped unless
// CONCORD_AUDIO_HW=1. They need PipeWire (or PulseAudio) running.
//
//	CONCORD_AUDIO_HW=1 go test -run Hardware -v ./internal/transport/audio
//
// CONCORD_AUDIO_MONITOR names the playback sink's monitor source for the
// loopback test (pactl list short sources | grep monitor).
func hw(t *testing.T) {
	if os.Getenv("CONCORD_AUDIO_HW") != "1" {
		t.Skip("set CONCORD_AUDIO_HW=1 to use the real sound system")
	}
}

// The real playback path (pw-play/pacat/aplay into the default sink) into the
// real capture path (a recorder subprocess) through the sound server's
// monitor of that sink: everything but the air.
func TestHardwareLoopbackThroughSoundServer(t *testing.T) {
	hw(t)
	mon := os.Getenv("CONCORD_AUDIO_MONITOR")
	if mon == "" {
		t.Skip("set CONCORD_AUDIO_MONITOR to the default sink's .monitor source")
	}
	tx := &Link{Record: []string{"sleep", "3600"}} // only rx listens
	rx := &Link{Record: []string{"parec", "-d", mon, "--raw", "--rate=48000", "--channels=1", "--format=s16le", "--latency-msec=50"}}
	var mu sync.Mutex
	var got [][]byte
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := rx.Start(ctx, func(f []byte) { mu.Lock(); got = append(got, f); mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	defer rx.Stop()
	if err := tx.Start(ctx, func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer tx.Stop()
	time.Sleep(500 * time.Millisecond)
	frames := randFrames(42, MaxData, MaxData, 40)
	start := time.Now()
	total := 0
	for _, f := range frames {
		if err := tx.Send(ctx, f); err != nil {
			t.Fatal(err)
		}
		total += len(f)
	}
	el := time.Since(start)
	time.Sleep(time.Second)
	mu.Lock()
	defer mu.Unlock()
	t.Logf("sent %d bytes in %d frames in %v = %.1f B/s wall clock, incl. player start-up", total, len(frames), el.Round(time.Millisecond), float64(total)/el.Seconds())
	if len(got) != len(frames) {
		t.Fatalf("received %d of %d frames (%d samples heard)", len(got), len(frames), rx.Heard())
	}
	for i := range frames {
		if !bytes.Equal(got[i], frames[i]) {
			t.Fatalf("frame %d differs", i)
		}
	}
}

// The real microphone, opened through the default recorder exactly as the
// app opens it: samples flow, nothing is decoded out of the room's sound,
// and Stop leaves no recorder process behind.
func TestHardwareMicrophoneOpensAndCloses(t *testing.T) {
	hw(t)
	l := New()
	if st := l.Probe(); st.State != "available" {
		t.Fatalf("probe: %+v", st)
	}
	n := 0
	if err := l.Start(context.Background(), func([]byte) { n++ }); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	pid := l.rec.Process.Pid
	argv := strings.Join(l.rec.Args, " ")
	l.mu.Unlock()
	time.Sleep(5 * time.Second)
	heard := l.Heard()
	if err := l.Stop(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d samples in 5 s (%.0f Hz), %d frames decoded", argv, heard, float64(heard)/5, n)
	if heard < 4*SampleRate {
		t.Fatalf("only %d samples arrived in 5 s", heard)
	}
	if n != 0 {
		t.Fatalf("decoded %d frames out of room sound", n)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		out, _ := exec.Command("ps", "-o", "pid,stat,cmd", "-p", strconv.Itoa(pid)).CombinedOutput()
		t.Fatalf("recorder %d still alive after Stop:\n%s", pid, out)
	}
}

// The two-device acoustic test (docs/transports/audio.md): run Listen on one
// machine, then Transmit on the other within a metre or two.
//
//	CONCORD_AUDIO_HW=1 go test -run HardwareListen -v -timeout 5m ./internal/transport/audio
//	CONCORD_AUDIO_HW=1 go test -run HardwareTransmit -v ./internal/transport/audio
const acousticFrames = 10

func TestHardwareTransmit(t *testing.T) {
	hw(t)
	if os.Getenv("CONCORD_AUDIO_ROLE") != "tx" {
		t.Skip("set CONCORD_AUDIO_ROLE=tx on the sending machine")
	}
	l := New()
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	start := time.Now()
	for i, f := range randFrames(7, sizes(acousticFrames)...) {
		if err := l.Send(context.Background(), f); err != nil {
			t.Fatal(err)
		}
		t.Logf("played frame %d", i+1)
	}
	t.Logf("%d frames in %v", acousticFrames, time.Since(start).Round(time.Millisecond))
}

func TestHardwareListen(t *testing.T) {
	hw(t)
	if os.Getenv("CONCORD_AUDIO_ROLE") != "rx" {
		t.Skip("set CONCORD_AUDIO_ROLE=rx on the listening machine")
	}
	want := map[string]int{}
	for i, f := range randFrames(7, sizes(acousticFrames)...) {
		want[string(f)] = i + 1
	}
	l := New()
	var mu sync.Mutex
	seen := map[int]bool{}
	var first, last time.Time
	err := l.Start(context.Background(), func(f []byte) {
		mu.Lock()
		defer mu.Unlock()
		if i, ok := want[string(f)]; ok {
			if first.IsZero() {
				first = time.Now()
			}
			last = time.Now()
			seen[i] = true
			t.Logf("frame %d ok", i)
		} else {
			t.Errorf("decoded a frame that was not sent")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Stop()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n == acousticFrames {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("received %d/%d frames; first→last %v", len(seen), acousticFrames, last.Sub(first).Round(time.Millisecond))
	if len(seen) < acousticFrames {
		t.Fail()
	}
}

func sizes(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = MaxData
	}
	return out
}
