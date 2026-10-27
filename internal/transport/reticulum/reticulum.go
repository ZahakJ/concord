// Package reticulum is Concord's link onto a Reticulum (RNS) mesh: LoRa
// RNodes, serial lines, packet radios, LANs and anything else RNS can route
// over, multi-hop, with no infrastructure required.
//
// RNS itself is a Python stack. Rather than re-implement it in Go, Concord
// runs a small sidecar (concord-rns.py, embedded in this binary) with the
// user's own Python and the `rns` package, and talks to it over a pipe. The
// sidecar registers one destination, concord.bundle, announces it, learns
// every other Concord node that announces the same, and fans each frame out
// to them as an encrypted RNS packet. What crosses the pipe is the Manager's
// bundle frames — MLS ciphertext plus a small header — and nothing else.
//
// Nothing starts until the user enables the link; Probe only looks at files.
// The sidecar refuses to run on amateur-only interfaces (see the script and
// docs/transports/reticulum.md): Concord has only ciphertext to send, and
// amateur rules forbid it.
package reticulum

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
)

//go:embed concord-rns.py
var sidecarScript string

const (
	// MTU sits just under RNS's Packet.ENCRYPTED_MDU (383 bytes in RNS 1.5.7:
	// a 500-byte network MTU minus the largest header, IFAC minimum, the
	// ephemeral key, HMAC and AES block padding). That MDU already accounts
	// for the 16-byte transport header a multi-hop packet gains, so a frame of
	// this size crosses any number of hops as a single packet.
	MTU = 380
	// Bitrate is a deliberately conservative figure for the medium most
	// people will use: an RNode on LoRa at a mid spreading factor runs a few
	// kbit/s on air, of which ~380 of every ~500 bytes is our payload, and a
	// shared channel is never ours alone. On a LAN or TCP interface the real
	// rate is far higher; the Manager only uses this to avoid choking a
	// radio with large messages.
	Bitrate = 1000

	defaultAnnounce = 600 * time.Second
	startTimeout    = 45 * time.Second
	stopGrace       = 3 * time.Second
	sendTimeout     = 10 * time.Second
	queueCap        = 128
)

// Config says where Python, the sidecar and the RNS config live. Every field
// is optional; see loadConfig for the defaults and the env overrides.
type Config struct {
	Python              string `json:"python,omitempty"`
	Script              string `json:"script,omitempty"` // empty: the embedded sidecar
	ConfigDir           string `json:"config,omitempty"`
	AnnounceIntervalSec int    `json:"announceIntervalSec,omitempty"`
}

// loadConfig reads <dataDir>/reticulum.json, then lets CONCORD_RNS_PYTHON,
// CONCORD_RNS_SCRIPT and CONCORD_RNS_CONFIG override it. The RNS config dir
// defaults to <dataDir>/reticulum — never ~/.reticulum, so Concord's RNS
// identity and interfaces stay inside its own data dir.
func loadConfig(dataDir string) Config {
	var c Config
	if raw, err := os.ReadFile(filepath.Join(dataDir, "reticulum.json")); err == nil {
		_ = json.Unmarshal(raw, &c)
	}
	if v := os.Getenv("CONCORD_RNS_PYTHON"); v != "" {
		c.Python = v
	}
	if v := os.Getenv("CONCORD_RNS_SCRIPT"); v != "" {
		c.Script = v
	}
	if v := os.Getenv("CONCORD_RNS_CONFIG"); v != "" {
		c.ConfigDir = v
	}
	if c.ConfigDir == "" {
		c.ConfigDir = filepath.Join(dataDir, "reticulum")
	}
	if c.AnnounceIntervalSec <= 0 {
		c.AnnounceIntervalSec = int(defaultAnnounce / time.Second)
	}
	return c
}

// venvPython is the interpreter inside a virtualenv rooted at dir.
func venvPython(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "Scripts", "python.exe")
	}
	return filepath.Join(dir, "bin", "python")
}

// findPython resolves the interpreter without running it. An explicit choice
// is trusted; otherwise a venv in the RNS config dir, then a pipx-installed
// rns, then python3 on PATH. explicit reports whether the user chose it.
func findPython(c Config) (path string, explicit bool, err error) {
	if c.Python != "" {
		p, err := exec.LookPath(c.Python)
		return p, true, err
	}
	cands := []string{venvPython(filepath.Join(c.ConfigDir, "venv"))}
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, venvPython(filepath.Join(home, ".local", "pipx", "venvs", "rns")),
			venvPython(filepath.Join(home, ".local", "share", "pipx", "venvs", "rns")))
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, false, nil
		}
	}
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, false, nil
		}
	}
	return "", false, errors.New("no python found")
}

// rnsInstalled guesses, from files alone, whether python can import RNS: a
// venv must hold the package; a system python is believed if rnsd (which
// `pip install rns` puts beside it) is around.
func rnsInstalled(python string) bool {
	venv := filepath.Dir(filepath.Dir(python))
	if _, err := os.Stat(filepath.Join(venv, "pyvenv.cfg")); err == nil {
		for _, pat := range []string{"lib/python3*/site-packages/RNS", "Lib/site-packages/RNS"} {
			if m, _ := filepath.Glob(filepath.Join(venv, filepath.FromSlash(pat))); len(m) > 0 {
				return true
			}
		}
		return false
	}
	if _, err := exec.LookPath("rnsd"); err == nil {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", "rnsd")); err == nil {
			return true
		}
	}
	return false
}

// defaultRNSConfig is written only when the config dir has none: a private
// instance (it neither binds nor joins a shared one) on the local network
// only. Radios are the user's to add.
const defaultRNSConfig = `# Concord's Reticulum config. See docs/transports/reticulum.md.
# Amateur-only interfaces are refused: Concord sends only ciphertext.
[reticulum]
  enable_transport = No
  share_instance = No
  panic_on_interface_error = No

[logging]
  loglevel = 4

[interfaces]
  [[Local network]]
    type = AutoInterface
    enabled = Yes
`

// Link is the Reticulum transport.
type Link struct {
	dataDir string

	mu sync.Mutex
	p  *proc
}

// New returns the link for a Concord data dir. It touches nothing.
func New(dataDir string) *Link { return &Link{dataDir: dataDir} }

func (*Link) Name() string  { return "reticulum" }
func (*Link) Label() string { return "Reticulum mesh (RNS)" }

func (*Link) Caps() transport.Caps {
	return transport.Caps{
		MTU:        MTU,
		Bitrate:    Bitrate,
		Latency:    3 * time.Second,
		Encryption: true, // refused on amateur-only interfaces, see package doc
		Local:      true, // RNodes, serial and LANs need no infrastructure
		Mode:       transport.Live,
		Range:      "depends on interfaces: LoRa RNode 1–10 km per hop, LAN, serial; multi-hop via RNS transport nodes",
	}
}

// Probe looks at files only: it never runs Python, never opens a port.
func (l *Link) Probe() transport.Status {
	l.mu.Lock()
	p := l.p
	l.mu.Unlock()
	if p != nil && !p.exited() {
		return transport.Status{State: transport.Available,
			Detail: fmt.Sprintf("running as %s, %d peers", p.hash.Load(), p.peers.Load())}
	}
	c := loadConfig(l.dataDir)
	py, explicit, err := findPython(c)
	if err != nil {
		return transport.Status{State: transport.NoHardware, Detail: "needs Python 3 with rns installed (pipx install rns, or set CONCORD_RNS_PYTHON)"}
	}
	if c.Script != "" {
		if _, err := os.Stat(c.Script); err != nil {
			return transport.Status{State: transport.Failed, Detail: "sidecar script not found: " + c.Script}
		}
	}
	if !explicit && !rnsInstalled(py) {
		return transport.Status{State: transport.NoHardware, Detail: py + " found but rns is not installed (pipx install rns)"}
	}
	d := "python " + py + ", config " + c.ConfigDir
	if p != nil {
		if e := p.lastErr(); e != "" {
			return transport.Status{State: transport.Failed, Detail: "sidecar exited: " + e}
		}
	}
	return transport.Status{State: transport.Available, Detail: d}
}

// Start launches the sidecar and returns once RNS is up and announcing.
func (l *Link) Start(ctx context.Context, deliver transport.Deliver) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.p != nil {
		if !l.p.exited() {
			return errors.New("reticulum: already running")
		}
		l.p = nil
	}
	c := loadConfig(l.dataDir)
	py, _, err := findPython(c)
	if err != nil {
		return fmt.Errorf("reticulum: %w (install rns: pipx install rns)", err)
	}
	if err := os.MkdirAll(c.ConfigDir, 0o700); err != nil {
		return fmt.Errorf("reticulum: %w", err)
	}
	cfgPath := filepath.Join(c.ConfigDir, "config")
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(cfgPath, []byte(defaultRNSConfig), 0o600); err != nil {
			return fmt.Errorf("reticulum: %w", err)
		}
	}
	p, err := spawn(ctx, py, c, deliver)
	if err != nil {
		return err
	}
	l.p = p
	return nil
}

// Stop closes the sidecar's stdin (it exits on EOF), and kills it if it has
// not gone within a few seconds. It returns once the process is reaped and
// every goroutine of this link has ended.
func (l *Link) Stop() error {
	l.mu.Lock()
	p := l.p
	l.p = nil
	l.mu.Unlock()
	if p != nil {
		p.stop()
	}
	return nil
}

var (
	errNotRunning = errors.New("reticulum: not running")
	errTooBig     = errors.New("reticulum: frame exceeds MTU")
	errBusy       = errors.New("reticulum: sidecar not accepting frames")
)

// Send hands one frame to the sidecar, which sends it to every known peer.
func (l *Link) Send(ctx context.Context, frame []byte) error {
	if len(frame) == 0 || len(frame) > MTU {
		return errTooBig
	}
	f, err := encodeFrame(tSend, frame)
	if err != nil {
		return err
	}
	l.mu.Lock()
	p := l.p
	l.mu.Unlock()
	if p == nil {
		return errNotRunning
	}
	return p.enqueue(ctx, f)
}

// Announce asks the sidecar to announce now instead of waiting for the
// periodic one, e.g. after plugging in a radio.
func (l *Link) Announce() error {
	l.mu.Lock()
	p := l.p
	l.mu.Unlock()
	if p == nil {
		return errNotRunning
	}
	f, _ := encodeFrame(tAnnounce, nil)
	return p.enqueue(context.Background(), f)
}

// Peers is how many other Concord nodes the sidecar has heard announce.
func (l *Link) Peers() int {
	l.mu.Lock()
	p := l.p
	l.mu.Unlock()
	if p == nil {
		return 0
	}
	return int(p.peers.Load())
}

// proc is one running sidecar.
type proc struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stdin  io.WriteCloser
	out    chan []byte   // encoded frames for the writer; bounded
	done   chan struct{} // closed once the process is reaped and reader/writer are gone
	peers  atomic.Int32
	hash   atomic.Value // string
	errMu  sync.Mutex
	err    string
}

func spawn(ctx context.Context, python string, c Config, deliver transport.Deliver) (*proc, error) {
	args := []string{"-c", sidecarScript}
	if c.Script != "" {
		args = []string{c.Script}
	}
	args = append(args, "--config", c.ConfigDir, "--announce-interval", strconv.Itoa(c.AnnounceIntervalSec))
	pctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(pctx, python, args...)
	cmd.Dir = c.ConfigDir // `python -c` puts the cwd on sys.path: make it ours
	cmd.Stderr = &lineLogger{}
	cmd.WaitDelay = stopGrace
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("reticulum: start %s: %w", python, err)
	}
	p := &proc{cmd: cmd, cancel: cancel, stdin: stdin, out: make(chan []byte, queueCap), done: make(chan struct{})}
	p.hash.Store("")
	ready := make(chan struct{}, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		p.read(stdout, deliver, ready)
	}()
	go func() {
		<-readerDone
		_ = cmd.Wait()
		cancel()
		close(p.done)
	}()
	go p.write()

	timer := time.NewTimer(startTimeout)
	defer timer.Stop()
	select {
	case <-ready:
		return p, nil
	case <-p.done:
		if e := p.lastErr(); e != "" {
			return nil, errors.New("reticulum: " + e)
		}
		return nil, fmt.Errorf("reticulum: sidecar exited during start (%v)", cmd.ProcessState)
	case <-ctx.Done():
		p.kill()
		return nil, ctx.Err()
	case <-timer.C:
		p.kill()
		return nil, errors.New("reticulum: sidecar did not come up in time")
	}
}

func (p *proc) read(r io.Reader, deliver transport.Deliver, ready chan<- struct{}) {
	for {
		kind, payload, err := readFrame(r)
		if err != nil {
			if err != io.EOF {
				// Garbage or a torn frame: whatever is on the other end is
				// not our sidecar behaving, so it goes.
				p.setErr("bad output from sidecar: " + err.Error())
				p.cancel()
			}
			return
		}
		switch kind {
		case tRecv:
			// Opaque ciphertext; the Manager's reassembler validates it.
			if len(payload) > 0 && len(payload) <= 512 && deliver != nil {
				deliver(payload)
			}
		case tReady:
			p.hash.Store(string(payload))
			select {
			case ready <- struct{}{}:
			default:
			}
		case tError:
			// The sidecar logs it to stderr too; keep it for Probe and Start.
			p.setErr(strings.ToValidUTF8(string(payload), "?"))
		case tPeers:
			if len(payload) == 2 {
				p.peers.Store(int32(binary.BigEndian.Uint16(payload)))
			}
		}
	}
}

func (p *proc) write() {
	for {
		select {
		case f := <-p.out:
			// A failed write means the child is dying; the reader sees EOF
			// and the frames still queued are dropped when done closes.
			_, _ = p.stdin.Write(f)
		case <-p.done:
			return
		}
	}
}

func (p *proc) enqueue(ctx context.Context, f []byte) error {
	if p.exited() { // select below picks randomly when both are ready
		if e := p.lastErr(); e != "" {
			return errors.New("reticulum: sidecar exited: " + e)
		}
		return errNotRunning
	}
	t := time.NewTimer(sendTimeout)
	defer t.Stop()
	select {
	case p.out <- f:
		return nil
	case <-p.done:
		if e := p.lastErr(); e != "" {
			return errors.New("reticulum: sidecar exited: " + e)
		}
		return errNotRunning
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return errBusy
	}
}

func (p *proc) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *proc) kill() {
	p.cancel()
	<-p.done
}

func (p *proc) stop() {
	_ = p.stdin.Close()
	t := time.NewTimer(stopGrace)
	defer t.Stop()
	select {
	case <-p.done:
	case <-t.C:
		p.kill()
	}
}

func (p *proc) setErr(s string) {
	p.errMu.Lock()
	p.err = s
	p.errMu.Unlock()
}

func (p *proc) lastErr() string {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

// lineLogger forwards the sidecar's stderr to the log a line at a time, with
// lines and the pending buffer bounded.
type lineLogger struct {
	buf []byte
}

func (w *lineLogger) Write(b []byte) (int, error) {
	n := len(b)
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			if len(w.buf) < 1024 {
				w.buf = append(w.buf, b[:min(len(b), 1024-len(w.buf))]...)
			}
			break
		}
		line := append(w.buf, b[:i]...)
		w.buf = w.buf[:0]
		b = b[i+1:]
		if len(line) > 300 {
			line = append(line[:300], "…"...)
		}
		log.Printf("reticulum: %s", strings.ToValidUTF8(string(line), "?"))
	}
	return n, nil
}
