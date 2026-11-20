package app

import (
	"bufio"
	"bytes"
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// The guest page's half of the guest-leg encryption, in Go, so the host side
// can be driven without a browser.
//
// guestClient is a raw guest: it speaks the real wire (clear v2 hello with the
// link id, gn and an ephemeral key; the host's chal; sealed records) and lets a
// test misbehave at any step. guestPipe wraps one in an adapter that hands a
// test the PLAIN frame protocol (hello with the link secret in Token, then
// msg/call/signal), so the older tests read as they always did. Every byte
// between a guestClient and serveGuest is what the rendezvous relays, and the
// tap records it.

// guestTap is the rendezvous's view of one visit, one buffer per direction so
// lines never interleave.
type guestTap struct {
	mu      sync.Mutex
	toGuest bytes.Buffer
	toHost  bytes.Buffer
}

func (t *guestTap) bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append(append([]byte(nil), t.toHost.Bytes()...), t.toGuest.Bytes()...)
}

// lines returns every complete line relayed in one direction.
func (t *guestTap) lines(toGuest bool) [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.toHost.Bytes()
	if toGuest {
		b = t.toGuest.Bytes()
	}
	var out [][]byte
	for _, l := range bytes.Split(b, []byte("\n")) {
		if len(l) > 0 {
			out = append(out, append([]byte(nil), l...))
		}
	}
	return out
}

// tappedConn records everything that crosses it, both ways.
type tappedConn struct {
	net.Conn
	tap *guestTap
}

func (c tappedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.tap.mu.Lock()
	c.tap.toGuest.Write(p[:n])
	c.tap.mu.Unlock()
	return n, err
}

func (c tappedConn) Write(p []byte) (int, error) {
	c.tap.mu.Lock()
	c.tap.toHost.Write(p)
	c.tap.mu.Unlock()
	return c.Conn.Write(p)
}

type guestClient struct {
	t        *testing.T
	wire     net.Conn
	r        *bufio.Reader
	tap      *guestTap
	hostPeer string

	linkID  string
	k       []byte
	gn      []byte
	gsk     *ecdh.PrivateKey
	hn, hpk []byte
	tx, rx  *guestCipher
}

// dialGuest starts one visit and returns a raw guest on the relayed wire.
func dialGuest(t *testing.T, s *Service) *guestClient {
	t.Helper()
	hostEnd, wireEnd := net.Pipe()
	go s.serveGuest(hostEnd)
	tap := &guestTap{}
	c := &guestClient{t: t, wire: tappedConn{wireEnd, tap}, tap: tap, hostPeer: s.host.PeerID().String()}
	c.r = bufio.NewReader(c.wire)
	t.Cleanup(func() { _ = wireEnd.Close() })
	return c
}

func (c *guestClient) sendLine(line []byte) error {
	_ = c.wire.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.wire.Write(append(append([]byte{}, line...), '\n'))
	return err
}

// helloLine prepares this guest's hello for secret (fresh gn and key).
func (c *guestClient) helloLine(secret []byte) []byte {
	c.linkID = guestLinkID(secret)
	c.k = guestLinkKey(secret)
	c.gn, _ = guestRandom(guestNonceBytes)
	var err error
	if c.gsk, err = guestEphemeral(); err != nil {
		c.t.Fatal(err)
	}
	line, _ := json.Marshal(map[string]any{
		"type": "hello", "v": 2, "link": c.linkID,
		"gn": b64url.EncodeToString(c.gn), "gpk": b64url.EncodeToString(c.gsk.PublicKey().Bytes()),
	})
	return line
}

func (c *guestClient) hello(secret []byte) error { return c.sendLine(c.helloLine(secret)) }

func (c *guestClient) readLine(wait time.Duration) ([]byte, error) {
	_ = c.wire.SetReadDeadline(time.Now().Add(wait))
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return line, err
	}
	return bytes.TrimSuffix(line, []byte("\n")), nil
}

// readChal reads the host's challenge and derives the session keys from it.
func (c *guestClient) readChal() error {
	line, err := c.readLine(10 * time.Second)
	if err != nil {
		return err
	}
	var f struct{ Type, Hn, Hpk string }
	if err := json.Unmarshal(line, &f); err != nil || f.Type != "chal" {
		return fmt.Errorf("want chal, got %q", line)
	}
	if c.hn, err = b64url.DecodeString(f.Hn); err != nil || len(c.hn) != guestNonceBytes {
		return fmt.Errorf("bad hn in %q", line)
	}
	if c.hpk, err = b64url.DecodeString(f.Hpk); err != nil {
		return fmt.Errorf("bad hpk in %q", line)
	}
	pub, err := guestPublicKey(c.hpk)
	if err != nil {
		return err
	}
	z, err := c.gsk.ECDH(pub)
	if err != nil {
		return err
	}
	g2h, h2g := guestSessionKeys(c.k, z, c.linkID, c.hostPeer, c.gn, c.hn, c.gsk.PublicKey().Bytes(), c.hpk)
	c.tx, c.rx = newGuestCipher(g2h), newGuestCipher(h2g)
	return nil
}

func (c *guestClient) send(f any) error {
	pt, _ := json.Marshal(f)
	return c.sendLine(c.tx.seal(pt))
}

func (c *guestClient) join(name, gid string) error {
	return c.send(guestFrame{Type: "join", Name: name, Gid: gid})
}

// recv reads and opens the next record. A clear frame inside the session is an
// error: the host must never send one.
func (c *guestClient) recv(wait time.Duration) (guestFrame, error) {
	line, err := c.readLine(wait)
	if err != nil {
		return guestFrame{}, err
	}
	var outer struct{ Type, C string }
	if json.Unmarshal(line, &outer) != nil || outer.Type != "e" {
		return guestFrame{}, fmt.Errorf("host sent a clear frame in a sealed session: %q", line)
	}
	pt, err := c.rx.open(outer.C)
	if err != nil {
		return guestFrame{}, err
	}
	var f guestFrame
	if err := json.Unmarshal(bytes.TrimRight(pt, " "), &f); err != nil {
		return guestFrame{}, fmt.Errorf("record is not a frame: %q", pt)
	}
	return f, nil
}

// recvUntil reads records until one of the given types arrives.
func (c *guestClient) recvUntil(t *testing.T, stop ...string) []guestFrame {
	t.Helper()
	var got []guestFrame
	for {
		f, err := c.recv(10 * time.Second)
		if err != nil {
			t.Fatalf("after %v: %v", typesOfFrames(got), err)
		}
		got = append(got, f)
		for _, s := range stop {
			if f.Type == s {
				return got
			}
		}
	}
}

// expectClosed asserts the host hangs up without sending another byte.
func (c *guestClient) expectClosed(t *testing.T) {
	t.Helper()
	line, err := c.readLine(10 * time.Second)
	if len(line) > 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("want a silent close, got %q (%v)", line, err)
	}
}

// guestPipe starts one guest visit and returns the test's (plain) end.
func guestPipe(t *testing.T, s *Service) (*bufio.Reader, net.Conn) {
	r, c, _ := guestPipeTap(t, s)
	return r, c
}

func guestPipeTap(t *testing.T, s *Service) (*bufio.Reader, net.Conn, *guestTap) {
	t.Helper()
	gc := dialGuest(t, s)
	testEnd, adapterEnd := net.Pipe()
	go runGuestAdapter(t, adapterEnd, gc)
	t.Cleanup(func() { _ = testEnd.Close() })
	return bufio.NewReader(testEnd), testEnd, gc.tap
}

// runGuestAdapter turns the plain test protocol into the real wire. A host
// that hangs up shows on the test's end as a close with nothing before it.
func runGuestAdapter(t *testing.T, plain net.Conn, gc *guestClient) {
	defer plain.Close()
	defer gc.wire.Close()
	r := bufio.NewReader(plain)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	var hello guestFrame
	if json.Unmarshal(line, &hello) != nil || hello.Type != "hello" {
		t.Errorf("adapter: first test frame must be hello, got %q", line)
		return
	}
	secret, err := b64url.DecodeString(hello.Token)
	if err != nil || len(secret) != guestSecretBytes {
		secret = []byte(hello.Token) // a deliberately bogus link: unknown id
	}
	if gc.hello(secret) != nil || gc.readChal() != nil {
		return
	}
	gid := hello.Gid
	if gid == "" {
		gid = testGid()
	}
	if gc.join(hello.Name, gid) != nil {
		return
	}
	go func() {
		defer plain.Close()
		defer gc.wire.Close()
		for {
			f, err := gc.recv(time.Hour)
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !strings.Contains(err.Error(), "closed") {
					t.Errorf("adapter: %v", err)
				}
				return
			}
			out, _ := json.Marshal(f)
			if _, err := plain.Write(append(out, '\n')); err != nil {
				return
			}
		}
	}()
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		if gc.sendLine(gc.tx.seal(bytes.TrimSpace(line))) != nil {
			return
		}
	}
}

func testGid() string {
	b, _ := guestRandom(12)
	return b64url.EncodeToString(b)
}

// expectGuestClosed asserts the plain end closes with no frame at all: what a
// refused visit looks like now that every failed join is a silent close.
func expectGuestClosed(t *testing.T, r *bufio.Reader, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := r.ReadString('\n')
	if line != "" || !errors.Is(err, io.EOF) {
		t.Fatalf("want the visit closed with nothing said, got %q (%v)", line, err)
	}
}

// linkSecretOf pulls S out of a guest link's fragment.
func linkSecretOf(t *testing.T, url string) string {
	t.Helper()
	i := strings.Index(url, "&k=")
	if i < 0 {
		t.Fatalf("guest link has no secret: %q", url)
	}
	return url[i+3:]
}

func linkSecretBytes(t *testing.T, url string) []byte {
	t.Helper()
	s, err := b64url.DecodeString(linkSecretOf(t, url))
	if err != nil || len(s) != guestSecretBytes {
		t.Fatalf("link secret is not 32 bytes of base64url: %v", err)
	}
	return s
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64url.DecodeString(s)
	if err != nil {
		t.Fatalf("not base64url: %q", s)
	}
	return b
}

// linkIDOf is the id the host files the link under.
func linkIDOf(t *testing.T, url string) string {
	t.Helper()
	return guestLinkID(linkSecretBytes(t, url))
}
