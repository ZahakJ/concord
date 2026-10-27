package audio

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
)

// Two audio links joined by a file instead of the air: one link's player
// appends its PCM to the file, the other's recorder tails it. This runs the
// real subprocess plumbing, the Manager's manual outbox and flush, and the
// modem, without a sound card.
func TestAudioLinkCarriesAMessageThroughTheManager(t *testing.T) {
	dir := t.TempDir()
	air := filepath.Join(dir, "air.raw")
	if err := os.WriteFile(air, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	opened := filepath.Join(dir, "mic-opened")
	tx := &Link{Play: []string{"sh", "-c", "cat >> " + air}, Record: []string{"sh", "-c", "touch " + opened + "; exec sleep 3600"}}
	rx := &Link{Record: []string{"sh", "-c", "touch " + opened + "; exec tail -c +1 -f " + air}, Play: []string{"true"}}

	const topic = "guild/chan"
	var mu sync.Mutex
	var got [][]byte
	a := transport.NewManager("", nil)
	b := transport.NewManager("", func(tp string, p []byte) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, p)
	})
	a.Register(tx)
	b.Register(rx)
	for _, m := range []*transport.Manager{a, b} {
		m.Carry(topic)
		m.Start(t.Context())
		_ = m.Links() // the settings page probing must not open anything
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(opened); err == nil {
		t.Fatal("a microphone was opened before the user enabled the link")
	}
	for _, m := range []*transport.Manager{a, b} {
		if err := m.SetEnabled("audio", true); err != nil {
			t.Fatal(err)
		}
	}

	payload := make([]byte, 300) // three audio frames once fragmented
	rand.New(rand.NewSource(1)).Read(payload)
	a.Send(topic, payload)
	if q := a.Links()[0].Queued; q != 3 {
		t.Fatalf("queued %d frames, want 3 (a manual link must wait for the user)", q)
	}
	if n, _, err := a.Flush("audio"); err != nil || n != 3 {
		t.Fatalf("flush: %d frames, %v", n, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	if len(got) != 1 || !bytes.Equal(got[0], payload) {
		mu.Unlock()
		t.Fatalf("delivered %d messages", len(got))
	}
	mu.Unlock()

	// Stop must kill the recorder, not leave tail -f running.
	rx.mu.Lock()
	proc := rx.rec.Process
	rx.mu.Unlock()
	b.Close()
	a.Close()
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("recorder still running after the link was stopped")
	}
}

func TestProbeNeverOpensAndExplainsWhatIsMissing(t *testing.T) {
	none := func(string) (string, error) { return "", os.ErrNotExist }
	have := func(f string) (string, error) { return "/usr/bin/" + f, nil }
	pcm := func(s string) func(string) ([]byte, error) {
		return func(string) ([]byte, error) { return []byte(s), nil }
	}
	cases := []struct {
		l    *Link
		want transport.State
	}{
		{&Link{LookPath: have, ReadFile: pcm("00-00: A : A : playback 1 : capture 1\n")}, transport.Available},
		{&Link{LookPath: have, ReadFile: pcm("00-01: D : D : playback 1\n")}, transport.NoHardware},
		{&Link{LookPath: none, ReadFile: pcm("00-00: A : A : playback 1 : capture 1\n")}, transport.NoHardware},
		{&Link{LookPath: have, ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist }}, transport.NoHardware},
	}
	for i, c := range cases {
		if st := c.l.Probe(); st.State != c.want {
			t.Errorf("case %d: %+v, want %s", i, st, c.want)
		}
		if c.l.rec != nil {
			t.Fatal("Probe started a recorder")
		}
	}
}

// A recorder that dies on its own (microphone unplugged) shows up on the
// settings page instead of leaving a link that looks on but hears nothing.
func TestRecorderDeathIsReported(t *testing.T) {
	have := func(f string) (string, error) { return "/usr/bin/" + f, nil }
	pcm := func(string) ([]byte, error) { return []byte("playback 1 : capture 1"), nil }
	l := &Link{Record: []string{"sh", "-c", "echo device unplugged >&2; exit 1"}, LookPath: have, ReadFile: pcm}
	if err := l.Start(t.Context(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for l.Probe().State != transport.Failed {
		if time.Now().After(deadline) {
			t.Fatal("recorder death not reported")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if d := l.Probe().Detail; d != "microphone stopped: device unplugged" {
		t.Fatalf("detail %q", d)
	}
	_ = l.Stop()
}

// Switching the link off silences it at once, even mid-frame, and a flush in
// progress cannot play the frames after it.
func TestStopCutsPlaybackShort(t *testing.T) {
	l := &Link{Record: []string{"sleep", "3600"}, Play: []string{"sleep", "30"}}
	if err := l.Send(t.Context(), []byte("x")); err == nil {
		t.Fatal("played while the link was off")
	}
	if err := l.Start(t.Context(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() { errc <- l.Send(t.Context(), []byte("hello")) }()
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	if err := l.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("a cut-short frame reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not stop the player")
	}
	t.Logf("player stopped %v after Stop", time.Since(start).Round(time.Millisecond))
	if err := l.Send(t.Context(), []byte("x")); err == nil {
		t.Fatal("played after Stop")
	}
}
