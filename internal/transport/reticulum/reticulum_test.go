package reticulum

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/transport"
)

func TestFramingRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := [][]byte{nil, []byte("x"), bytes.Repeat([]byte{0xAB}, MTU), bytes.Repeat([]byte{1}, maxPipeFrame)}
	for i, p := range in {
		f, err := encodeFrame(byte(i+1), p)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(f)
	}
	for i, p := range in {
		k, got, err := readFrame(&buf)
		if err != nil || k != byte(i+1) || !bytes.Equal(got, p) {
			t.Fatalf("frame %d: kind %d err %v", i, k, err)
		}
	}
	if _, _, err := readFrame(&buf); err != io.EOF {
		t.Fatalf("want EOF at end, got %v", err)
	}
}

func TestFramingRejectsGarbage(t *testing.T) {
	if _, err := encodeFrame(tSend, make([]byte, maxPipeFrame+1)); err == nil {
		t.Fatal("encoded an oversized frame")
	}
	if _, _, err := readFrame(bytes.NewReader([]byte{tRecv, 0xFF, 0xFF})); err != errPipeFrame {
		t.Fatalf("oversized length: got %v", err)
	}
	if _, _, err := readFrame(bytes.NewReader([]byte{tRecv, 0, 5, 1, 2})); err != io.ErrUnexpectedEOF {
		t.Fatalf("torn payload: got %v", err)
	}
	if _, _, err := readFrame(bytes.NewReader([]byte{tRecv, 0})); err != io.ErrUnexpectedEOF {
		t.Fatalf("torn header: got %v", err)
	}
}

func TestSendRejectsOversizeAndNotRunning(t *testing.T) {
	l := New(t.TempDir())
	if err := l.Send(context.Background(), make([]byte, MTU+1)); err != errTooBig {
		t.Fatalf("got %v", err)
	}
	if err := l.Send(context.Background(), []byte("x")); err != errNotRunning {
		t.Fatalf("got %v", err)
	}
	if c := l.Caps(); !c.Encryption || !c.Local || c.Mode != transport.Live || c.MTU != MTU || c.Paid() {
		t.Fatalf("caps %+v", c)
	}
}

// fakeSidecar makes a Link whose "python" is /bin/sh running script, so the
// process plumbing can be tested without RNS.
func fakeSidecar(t *testing.T, script string) *Link {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.sh")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONCORD_RNS_PYTHON", sh)
	t.Setenv("CONCORD_RNS_SCRIPT", path)
	t.Setenv("CONCORD_RNS_CONFIG", filepath.Join(dir, "rns"))
	return New(dir)
}

func waitUntil(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestGarbageFromSidecarKillsIt(t *testing.T) {
	// READY, then a frame header claiming 65535 bytes: not our sidecar.
	l := fakeSidecar(t, `printf '\202\000\002ab'; printf '\201\377\377'; exec sleep 60`)
	if err := l.Start(context.Background(), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	p := l.p
	l.mu.Unlock()
	waitUntil(t, 5*time.Second, "garbage kill", p.exited)
	if err := l.Send(context.Background(), []byte("x")); err == nil {
		t.Fatal("send to a dead sidecar succeeded")
	}
	if !strings.Contains(p.lastErr(), "bad output") {
		t.Fatalf("lastErr %q", p.lastErr())
	}
	_ = l.Stop()
}

func TestStartReportsSidecarError(t *testing.T) {
	l := fakeSidecar(t, `printf '\203\000\004nope'; exit 4`)
	err := l.Start(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("got %v", err)
	}
}

func TestStopAndCancelEndTheProcess(t *testing.T) {
	// Graceful: sidecar exits when stdin closes.
	l := fakeSidecar(t, `printf '\202\000\000'; cat >/dev/null`)
	if err := l.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Send(context.Background(), []byte("frame")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = l.Stop()
	if time.Since(start) > 2*time.Second {
		t.Fatal("graceful stop took the kill path")
	}

	// Stubborn: ignores stdin, so Stop kills it after the grace period.
	l = fakeSidecar(t, `printf '\202\000\000'; exec sleep 60`)
	if err := l.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	p := l.p
	l.mu.Unlock()
	_ = l.Stop()
	if !p.exited() {
		t.Fatal("Stop returned with the process alive")
	}

	// Context cancel alone kills it.
	l = fakeSidecar(t, `printf '\202\000\000'; exec sleep 60`)
	ctx, cancel := context.WithCancel(context.Background())
	if err := l.Start(ctx, nil); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	p = l.p
	l.mu.Unlock()
	cancel()
	waitUntil(t, 5*time.Second, "cancel kill", p.exited)
	_ = l.Stop()
}

// rnsPython returns an interpreter that can import RNS, or skips.
func rnsPython(t *testing.T) string {
	t.Helper()
	py := os.Getenv("CONCORD_RNS_PYTHON")
	if py == "" {
		py = "python3"
	}
	p, err := exec.LookPath(py)
	if err != nil {
		t.Skip("no python; set CONCORD_RNS_PYTHON to a python with rns installed")
	}
	if err := exec.Command(p, "-c", "import RNS").Run(); err != nil {
		t.Skip("python cannot import RNS; set CONCORD_RNS_PYTHON to a python with rns installed")
	}
	return p
}

// rnsLink makes a Link with its own data dir and the given RNS config. Env
// overrides are cleared so the reticulum.json written here is what counts.
func rnsLink(t *testing.T, py, rnsConfig string) (*Link, string) {
	t.Helper()
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "rns")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config"), []byte(rnsConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Config{Python: py, ConfigDir: cfgDir, AnnounceIntervalSec: 600})
	if err := os.WriteFile(filepath.Join(dir, "reticulum.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return New(dir), cfgDir
}

func clearEnv(t *testing.T) {
	for _, k := range []string{"CONCORD_RNS_PYTHON", "CONCORD_RNS_SCRIPT", "CONCORD_RNS_CONFIG"} {
		t.Setenv(k, "")
	}
}

const rnsHeader = `[reticulum]
  enable_transport = No
  share_instance = No
  panic_on_interface_error = No
[logging]
  loglevel = 3
[interfaces]
`

func TestHamBandsRefused(t *testing.T) {
	py := rnsPython(t)
	clearEnv(t)
	cases := map[string]string{
		"rnode 433 MHz": `  [[LoRa]]
    type = RNodeInterface
    enabled = Yes
    port = /dev/null-nonexistent
    frequency = 433050000
    bandwidth = 125000
    txpower = 7
    spreadingfactor = 8
    codingrate = 5
`,
		"rnode 2m": `  [[LoRa]]
    type = RNodeInterface
    interface_enabled = True
    port = /dev/null-nonexistent
    frequency = 145500000
`,
		"ax25": `  [[TNC]]
    type = AX25KISSInterface
    enabled = yes
    callsign = N0CALL
    port = /dev/null-nonexistent
`,
		"id callsign on ISM": `  [[LoRa]]
    type = RNodeInterface
    enabled = Yes
    port = /dev/null-nonexistent
    frequency = 915000000
    id_callsign = N0CALL
`,
	}
	for name, iface := range cases {
		t.Run(name, func(t *testing.T) {
			l, _ := rnsLink(t, py, rnsHeader+iface)
			err := l.Start(context.Background(), nil)
			if err == nil {
				_ = l.Stop()
				t.Fatal("started on an amateur-only interface")
			}
			if !strings.Contains(err.Error(), "amateur") {
				t.Fatalf("wrong refusal: %v", err)
			}
		})
	}

	// ISM 915 MHz and 868 MHz pass the check; a disabled ham interface is
	// ignored. --check-only stops before touching any hardware.
	ok := rnsHeader + `  [[US]]
    type = RNodeInterface
    enabled = Yes
    port = /dev/null-nonexistent
    frequency = 915000000
  [[EU]]
    type = RNodeInterface
    enabled = Yes
    port = /dev/null-nonexistent
    frequency = 868100000
  [[Old 70cm]]
    type = RNodeInterface
    enabled = No
    frequency = 433000000
`
	_, cfgDir := rnsLink(t, py, ok)
	cmd := exec.Command(py, "-c", sidecarScript, "--config", cfgDir, "--check-only")
	cmd.Dir = cfgDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ISM config refused: %v", err)
	}
	if k, p, err := readFrame(bytes.NewReader(out)); err != nil || k != tReady || string(p) != "check-ok" {
		t.Fatalf("check-only said %x %q %v", k, p, err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

type inbox struct {
	mu  sync.Mutex
	got [][]byte
}

func (i *inbox) deliver(_ string, p []byte) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.got = append(i.got, append([]byte(nil), p...))
}

func (i *inbox) has(want []byte) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, g := range i.got {
		if bytes.Equal(g, want) {
			return true
		}
	}
	return false
}

// TestEndToEndOverRNS runs two real sidecars on this machine, joined by an RNS
// TCP interface on loopback, each under its own transport.Manager, and checks
// a multi-frame ciphertext crosses in both directions.
func TestEndToEndOverRNS(t *testing.T) {
	py := rnsPython(t)
	clearEnv(t)
	port := freePort(t)
	la, _ := rnsLink(t, py, rnsHeader+fmt.Sprintf(`  [[Listen]]
    type = TCPServerInterface
    enabled = Yes
    listen_ip = 127.0.0.1
    listen_port = %d
`, port))
	lb, _ := rnsLink(t, py, rnsHeader+fmt.Sprintf(`  [[Dial]]
    type = TCPClientInterface
    enabled = Yes
    target_host = 127.0.0.1
    target_port = %d
`, port))

	node := func(l *Link) (*transport.Manager, *inbox) {
		in := &inbox{}
		m := transport.NewManager(filepath.Join(t.TempDir(), "transports.json"), in.deliver)
		m.Register(l)
		m.Start(context.Background())
		if err := m.SetEnabled(l.Name(), true); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(m.Close)
		v := m.Links()[0]
		if !v.Running {
			t.Fatalf("link did not start: %s", v.Error)
		}
		m.Carry("chan")
		return m, in
	}
	ma, ina := node(la)
	mb, inb := node(lb)

	// The first announces may beat the TCP connection; nudge until each side
	// has heard the other.
	deadline := time.Now().Add(60 * time.Second)
	for la.Peers() < 1 || lb.Peers() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("peers never met: a=%d b=%d", la.Peers(), lb.Peers())
		}
		_ = la.Announce()
		_ = lb.Announce()
		time.Sleep(500 * time.Millisecond)
	}

	ab := make([]byte, 1000) // three frames at MTU 380
	ba := make([]byte, 200)
	_, _ = rand.Read(ab)
	_, _ = rand.Read(ba)
	ma.Send("chan", ab)
	mb.Send("chan", ba)
	waitUntil(t, 30*time.Second, "a→b delivery", func() bool { return inb.has(ab) })
	waitUntil(t, 30*time.Second, "b→a delivery", func() bool { return ina.has(ba) })

	// Stop really stops: the sidecars are reaped.
	pa, pb := la.p, lb.p
	ma.Close()
	mb.Close()
	for _, p := range []*proc{pa, pb} {
		if !p.exited() {
			t.Fatal("sidecar still running after Close")
		}
	}
	if err := la.Send(context.Background(), []byte("x")); !errors.Is(err, errNotRunning) {
		t.Fatalf("send after stop: %v", err)
	}
}
