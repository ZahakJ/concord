package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
)

// Link is the audio modem as a Concord transport. It is Manual: frames wait
// in the outbox until the person presses send and the computer plays them, so
// a laptop never starts chirping on its own.
//
// Audio I/O goes through the system's own command-line tools rather than cgo:
// PipeWire's pw-record/pw-play, PulseAudio's parec/pacat, or ALSA's
// arecord/aplay — whichever is installed, in that order — speaking raw signed
// 16-bit mono at 48 kHz on a pipe. The microphone is opened only by Start,
// which the Manager calls only once the user has switched the link on, and
// the recorder process is killed by Stop.
type Link struct {
	// Record and Play override the capture/playback commands (raw s16le
	// mono 48 kHz on stdout/stdin). Empty means pick from what is installed.
	Record, Play []string
	// LookPath and ReadFile are seams for Probe in tests.
	LookPath func(string) (string, error)
	ReadFile func(string) ([]byte, error)

	mu      sync.Mutex
	rec     *exec.Cmd
	recDone chan struct{}
	// mutedUntil is when our own transmission stops echoing into our own
	// microphone; captured audio before then is thrown away (half duplex).
	mutedUntil time.Time
	playing    bool
	player     *exec.Cmd // the frame being played, killed by Stop

	sendMu sync.Mutex // one transmission at a time

	heard atomic.Int64 // samples captured since Start, for diagnostics

	// died is set when the recorder exited without Stop asking it to: an
	// unplugged microphone, a sound server restart.
	died string
}

// Heard is how many microphone samples the running link has taken in.
func (l *Link) Heard() int64 { return l.heard.Load() }

// New returns the audio link with the system's tools.
func New() *Link { return &Link{} }

func (l *Link) Name() string  { return "audio" }
func (l *Link) Label() string { return "Sound (audio modem)" }

// Bitrate is the honest figure: a full 128-byte frame takes 3.8 s on the air
// plus the gap between frames, ≈32 B/s.
func (l *Link) Caps() transport.Caps {
	return transport.Caps{
		MTU:        MaxData,
		Bitrate:    250,
		Latency:    4 * time.Second,
		Encryption: true,
		Local:      true,
		Mode:       transport.Manual,
		Range:      "same room (~1–5 m)",
	}
}

var (
	recorders = [][]string{
		{"pw-record", "--rate", "48000", "--channels", "1", "--format", "s16", "--raw", "--latency", "50ms", "-"},
		{"parec", "--raw", "--rate=48000", "--channels=1", "--format=s16le", "--latency-msec=50"},
		{"arecord", "-q", "-t", "raw", "-f", "S16_LE", "-r", "48000", "-c", "1"},
	}
	players = [][]string{
		{"pw-play", "--rate", "48000", "--channels", "1", "--format", "s16", "--raw", "-"},
		{"pacat", "--playback", "--raw", "--rate=48000", "--channels=1", "--format=s16le"},
		{"aplay", "-q", "-t", "raw", "-f", "S16_LE", "-r", "48000", "-c", "1"},
	}
)

func (l *Link) lookPath(f string) (string, error) {
	if l.LookPath != nil {
		return l.LookPath(f)
	}
	return exec.LookPath(f)
}

func (l *Link) pick(override []string, from [][]string) []string {
	if len(override) > 0 {
		return override
	}
	for _, c := range from {
		if _, err := l.lookPath(c[0]); err == nil {
			return c
		}
	}
	return nil
}

// Probe looks for a capture and a playback device in the kernel's list of
// sound devices and for a tool to drive them. It reads /proc/asound/pcm; it
// never opens a device.
func (l *Link) Probe() transport.Status {
	l.mu.Lock()
	died := l.died
	l.mu.Unlock()
	if died != "" {
		return transport.Status{State: transport.Failed, Detail: died}
	}
	read := l.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	raw, err := read("/proc/asound/pcm")
	if err != nil {
		return transport.Status{State: transport.NoHardware, Detail: "no sound devices"}
	}
	capture := strings.Contains(string(raw), "capture")
	playback := strings.Contains(string(raw), "playback")
	switch {
	case !capture && !playback:
		return transport.Status{State: transport.NoHardware, Detail: "no microphone or speaker"}
	case !capture:
		return transport.Status{State: transport.NoHardware, Detail: "no microphone"}
	case !playback:
		return transport.Status{State: transport.NoHardware, Detail: "no speaker"}
	}
	if l.pick(l.Record, recorders) == nil || l.pick(l.Play, players) == nil {
		return transport.Status{State: transport.NoHardware, Detail: "install pipewire, pulseaudio-utils or alsa-utils"}
	}
	return transport.Status{State: transport.Available}
}

// Start opens the microphone and decodes everything it hears until Stop or
// ctx ends. Without an override it tries each installed recorder in turn: an
// old PipeWire whose pw-record rejects --raw exits at once, and parec or
// arecord takes over. A recorder that died on its own is replaced.
func (l *Link) Start(ctx context.Context, deliver transport.Deliver) error {
	l.mu.Lock()
	if l.rec != nil {
		select {
		case <-l.recDone: // dead: start afresh below
			l.rec, l.recDone = nil, nil
		default:
			l.mu.Unlock()
			return nil
		}
	}
	l.died = ""
	l.mu.Unlock()

	cands := [][]string{l.Record}
	if len(l.Record) == 0 {
		cands = nil
		for _, c := range recorders {
			if _, err := l.lookPath(c[0]); err == nil {
				cands = append(cands, c)
			}
		}
	}
	if len(cands) == 0 {
		return errors.New("audio: no recording tool installed")
	}
	l.heard.Store(0)
	frames := make(chan []byte, 64)
	go func() {
		// Delivery runs off the read loop: slow MLS handling above must
		// not stall the pipe and let the recorder's buffer overrun.
		for f := range frames {
			deliver(f)
		}
	}()
	demod := NewDemodulator(func(f []byte) {
		select {
		case frames <- f:
		default: // 64 frames (≈4 min of sound) undelivered: drop
		}
	})
	var lastErr error
	for i, argv := range cands {
		r, err := l.spawn(ctx, argv, demod)
		if err != nil {
			lastErr = err
			continue
		}
		if i < len(cands)-1 && !r.healthy() {
			lastErr = fmt.Errorf("audio: %s: %s", argv[0], firstLine(r.stderr.String()))
			continue
		}
		l.mu.Lock()
		l.rec, l.recDone = r.cmd, r.done
		l.mu.Unlock()
		go func() {
			<-r.done
			close(frames)
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.rec == r.cmd && ctx.Err() == nil {
				// Still meant to be running: say so on the settings page.
				msg := strings.TrimSpace(r.stderr.String())
				if msg == "" && r.err != nil {
					msg = r.err.Error()
				}
				l.died = "microphone stopped: " + firstLine(msg)
			}
		}()
		return nil
	}
	close(frames)
	return lastErr
}

type recorder struct {
	cmd    *exec.Cmd
	stderr *bytes.Buffer
	first  chan struct{} // closed on the first samples
	done   chan struct{} // closed once the process is reaped
	err    error         // Wait's result, valid after done
}

// healthy waits for a fresh recorder to either produce audio or die.
func (r *recorder) healthy() bool {
	select {
	case <-r.first:
		return true
	case <-r.done:
		return false
	case <-time.After(2 * time.Second):
		return true // running but quiet; give it the benefit of the doubt
	}
}

func (l *Link) spawn(ctx context.Context, argv []string, demod *Demodulator) (*recorder, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	r := &recorder{cmd: cmd, stderr: &bytes.Buffer{}, first: make(chan struct{}), done: make(chan struct{})}
	cmd.Stderr = &limitedWriter{w: r.stderr, n: 4096}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("audio: open microphone: %w", err)
	}
	go func() {
		defer close(r.done)
		buf := make([]byte, 4800) // 50 ms
		var carry []byte
		started := false
		for {
			n, err := out.Read(buf)
			if n > 0 {
				if !started {
					started = true
					close(r.first)
				}
				chunk := append(carry, buf[:n]...)
				even := len(chunk) &^ 1
				l.heard.Add(int64(even / 2))
				if l.muted() {
					// Our own voice: feed silence so the demodulator's
					// clock keeps step with the room's.
					demod.Push(make([]float32, even/2))
				} else {
					demod.PushPCM16(chunk[:even])
				}
				carry = append(carry[:0], chunk[even:]...)
			}
			if err != nil {
				break
			}
		}
		r.err = cmd.Wait()
	}()
	return r, nil
}

func (l *Link) muted() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.playing || time.Now().Before(l.mutedUntil)
}

// Stop kills the recorder and any frame mid-play, and waits for the recorder
// to be reaped. It also clears a "microphone stopped" report, so the user can
// switch the link back on.
func (l *Link) Stop() error {
	l.mu.Lock()
	cmd, done := l.rec, l.recDone
	l.rec, l.recDone = nil, nil
	l.died = ""
	if l.player != nil && l.player.Process != nil {
		_ = l.player.Process.Kill() // Send reaps it
	}
	l.mu.Unlock()
	if cmd == nil {
		return nil
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		return errors.New("audio: recorder did not exit")
	}
	return nil
}

// sendMargin is how long past a frame's own duration a player may take
// (start-up, a sound server waking a suspended sink) before it is killed.
var sendMargin = 5 * time.Second

// Send plays one frame through the speaker and returns when it has finished.
// It refuses while the link is stopped or its microphone has died (half
// duplex: a link that cannot hear has no business talking), Stop or the
// link's context cuts a frame short, and a wedged player is killed after the
// frame's duration plus sendMargin.
func (l *Link) Send(ctx context.Context, frame []byte) error {
	if len(frame) == 0 || len(frame) > MaxData {
		return fmt.Errorf("audio: frame of %d bytes (max %d)", len(frame), MaxData)
	}
	argv := l.pick(l.Play, players)
	if argv == nil {
		return errors.New("audio: no playback tool installed")
	}
	l.sendMu.Lock()
	defer l.sendMu.Unlock()
	// A short lead-in and tail of silence: some outputs wake up from
	// suspend on the first samples and would clip the preamble.
	samples := append(append(make([]float32, SampleRate/10), Modulate(frame)...), make([]float32, SampleRate/10)...)
	pcm := PCM16(samples)
	dur := time.Duration(len(samples)) * time.Second / SampleRate

	ctx, cancel := context.WithTimeout(ctx, dur+sendMargin)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = time.Second // a stuck stdin copy cannot outlive the kill
	cmd.Stdin = bytes.NewReader(pcm)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4096}

	l.mu.Lock()
	switch {
	case l.rec == nil:
		// Switched off (possibly mid-flush): the speaker stays quiet.
		l.mu.Unlock()
		return errors.New("audio: the link is off")
	case l.died != "":
		l.mu.Unlock()
		return errors.New("audio: " + l.died)
	}
	if err := cmd.Start(); err != nil {
		l.mu.Unlock()
		return fmt.Errorf("audio: play: %w", err)
	}
	l.playing, l.player = true, cmd
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.playing, l.player = false, nil
		// The room rings on, and the player's buffer may lag the process.
		l.mutedUntil = time.Now().Add(300 * time.Millisecond)
		l.mu.Unlock()
	}()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("audio: player stuck for %v, killed", (dur + sendMargin).Round(time.Second))
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("audio: play: %v: %s", err, msg)
		}
		return fmt.Errorf("audio: play: %w", err)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "recorder exited"
	}
	return s
}

// limitedWriter keeps the first n bytes of a tool's complaints.
type limitedWriter struct {
	w io.Writer
	n int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.n > 0 {
		k := min(len(p), lw.n)
		_, _ = lw.w.Write(p[:k])
		lw.n -= k
	}
	return len(p), nil
}
