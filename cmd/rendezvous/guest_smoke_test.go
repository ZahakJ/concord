package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/libp2p/go-libp2p"
	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"

	cnet "github.com/zahak/concord/internal/net"
)

// These tests pin the FRAMING CONTRACT between the guest gateway (relayGuest)
// and the guest page (guestpage/index.html). The two halves once drifted:
// the gateway stripped the '\n' it read off the host stream and forwarded the
// bare line, while the page buffers WebSocket data and parses only complete,
// newline-terminated lines — so every guest join hung at "Connecting…" for
// weeks, each half looking correct in isolation. Nothing ever ran them
// against each other; this file is that missing run. The parser below is a
// LITERAL transcription of the page's ws.onmessage loop, so the contract is
// exercised end to end: real gateway handler, real libp2p stream, real
// WebSocket, the page's exact split-and-buffer semantics.

// pageParser is the guest page's receive loop, verbatim:
//
//	rxBuf += String(ev.data);
//	const lines = rxBuf.split("\n");
//	rxBuf = lines.pop() ?? "";
//
// Any change here must mirror guestpage/index.html — the fidelity is the test.
type pageParser struct{ rxBuf string }

func (p *pageParser) feed(data string) []string {
	p.rxBuf += data
	lines := strings.Split(p.rxBuf, "\n")
	p.rxBuf = lines[len(lines)-1]
	var out []string
	for _, l := range lines[:len(lines)-1] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// testFrame mirrors the fields the page actually reads off each line.
type testFrame struct {
	Type    string `json:"type"`
	Token   string `json:"token,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
	Meeting string `json:"meeting,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Data    string `json:"data,omitempty"`
}

// newLoopbackHost builds a libp2p host confined to 127.0.0.1 — the tests must
// never touch a real network or the public rendezvous.
func newLoopbackHost(t *testing.T, listen bool) host.Host {
	t.Helper()
	opts := []libp2p.Option{
		libp2p.Security(noise.ID, noise.New),
		libp2p.DisableRelay(),
	}
	if listen {
		opts = append(opts, libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	} else {
		opts = append(opts, libp2p.NoListenAddrs)
	}
	h, err := libp2p.New(opts...)
	if err != nil {
		t.Fatalf("libp2p host: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

// newGatewayServer serves the real relayGuest handler exactly as main.go
// mounts it, on an httptest listener instead of the env-configured port.
func newGatewayServer(t *testing.T, ctx context.Context, gw host.Host) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/guest/ws", func(w http.ResponseWriter, r *http.Request) {
		relayGuest(ctx, gw, w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func dialGuestWS(t *testing.T, srv *httptest.Server, hostID string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/guest/ws?h=" + hostID
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

// readFrames pulls WebSocket messages through the page parser until wantTypes
// have all arrived (in order) or the deadline passes. Along the way it makes
// THE assertion this file exists for: every text message the gateway emits
// must be newline-terminated, because the page will sit on an unterminated
// line forever.
func readFrames(t *testing.T, ws *websocket.Conn, wantTypes []string) []testFrame {
	t.Helper()
	var p pageParser
	var got []testFrame
	deadline := time.Now().Add(8 * time.Second)
	for len(got) < len(wantTypes) {
		_ = ws.SetReadDeadline(deadline)
		typ, msg, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("after %d frames %v (rxBuf %q — an unterminated line the page would wait on forever): %v",
				len(got), typesOf(got), p.rxBuf, err)
		}
		if typ != websocket.TextMessage {
			t.Fatalf("gateway sent a non-text message (type %d); the page only handles text", typ)
		}
		if len(msg) == 0 || msg[len(msg)-1] != '\n' {
			t.Fatalf("gateway message not newline-terminated: %q — the guest page buffers and splits on '\\n', so this frame would never parse (the exact bug that silently broke guest joins)", msg)
		}
		for _, line := range p.feed(string(msg)) {
			var f testFrame
			if err := json.Unmarshal([]byte(line), &f); err != nil {
				t.Fatalf("frame is not valid JSON once line-split: %q: %v", line, err)
			}
			got = append(got, f)
		}
	}
	if p.rxBuf != "" {
		t.Fatalf("partial line left in the page buffer after all frames: %q", p.rxBuf)
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Fatalf("frame %d: got type %q, want %q (all: %v)", i, got[i].Type, want, typesOf(got))
		}
	}
	return got
}

func typesOf(fs []testFrame) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Type
	}
	return out
}

// TestGuestGatewayFramingContract drives the full relay path: browser-side
// WebSocket client (speaking like the page: JSON without a trailing newline)
// → real relayGuest → real libp2p stream → a fake meeting host speaking the
// host's line protocol (welcome/history/end, as internal/app/guest.go does).
func TestGuestGatewayFramingContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	meetingHost := newLoopbackHost(t, true)
	gw := newLoopbackHost(t, false)
	if err := gw.Connect(ctx, peer.AddrInfo{ID: meetingHost.ID(), Addrs: meetingHost.Addrs()}); err != nil {
		t.Fatalf("connect gateway→host: %v", err)
	}

	// A signaling-sized payload: an SDP offer with video runs to several KB and
	// is exactly the frame that used to arrive split/unterminated.
	bigSDP := strings.Repeat("a=candidate 0123456789 ", 300) // ~7 KB

	// hostGot collects every line the fake meeting host reads off the stream —
	// the guest→host half of the contract.
	hostGot := make(chan testFrame, 16)
	meetingHost.SetStreamHandler(cnet.GuestProtocol, func(s network.Stream) {
		defer s.Close()
		r := bufio.NewReader(s)
		writeLine := func(f testFrame) {
			b, _ := json.Marshal(f)
			_, _ = s.Write(append(b, '\n'))
		}
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			var f testFrame
			if err := json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &f); err != nil {
				t.Errorf("host received a non-JSON line: %q: %v", line, err)
				return
			}
			hostGot <- f
			switch f.Type {
			case "hello":
				// What a real host sends on join: welcome, then history.
				writeLine(testFrame{Type: "welcome", Meeting: "Smoke Test Standup", Name: f.Name})
				writeLine(testFrame{Type: "msg", Content: "backlog line one"})
				writeLine(testFrame{Type: "signal", Data: bigSDP})
			case "msg":
				if f.Content == "bye" {
					writeLine(testFrame{Type: "end", Reason: "This meeting has ended."})
					return
				}
			}
		}
	})

	srv := newGatewayServer(t, ctx, gw)
	ws := dialGuestWS(t, srv, meetingHost.ID().String())

	// The page sends JSON.stringify(...) — NO trailing newline. The gateway
	// must add it, or the host's line reader never completes the hello.
	send := func(v any) {
		b, _ := json.Marshal(v)
		_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := ws.WriteMessage(websocket.TextMessage, b); err != nil {
			t.Fatalf("ws write: %v", err)
		}
	}
	send(map[string]string{"type": "hello", "token": "tok123", "name": "Visitor"})

	frames := readFrames(t, ws, []string{"welcome", "msg", "signal"})
	if frames[0].Meeting != "Smoke Test Standup" {
		t.Errorf("welcome carried meeting %q", frames[0].Meeting)
	}
	if frames[2].Data != bigSDP {
		t.Errorf("multi-KB signal frame corrupted in relay: got %d bytes, want %d", len(frames[2].Data), len(bigSDP))
	}

	// Guest→host: a chat line, plus a client that DOES send its own newline —
	// the gateway must not double it into an empty extra frame.
	send(map[string]string{"type": "msg", "content": "hi from the browser"})
	_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"msg","content":"bye"}`+"\n")); err != nil {
		t.Fatalf("ws write: %v", err)
	}
	end := readFrames(t, ws, []string{"end"})
	if end[0].Reason == "" {
		t.Error("end frame without a reason — the page would show a blank notice")
	}

	wantHost := []testFrame{
		{Type: "hello", Token: "tok123", Name: "Visitor"},
		{Type: "msg", Content: "hi from the browser"},
		{Type: "msg", Content: "bye"},
	}
	for _, want := range wantHost {
		select {
		case got := <-hostGot:
			if got.Type != want.Type || got.Token != want.Token || got.Name != want.Name || got.Content != want.Content {
				t.Errorf("host received %+v, want %+v", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("host never received the %q frame as a complete line", want.Type)
		}
	}
}

// TestGuestGatewayUnreachableHost pins the failure path: when the meeting
// host cannot be dialed, the gateway itself synthesizes the end frame — and
// that frame had the same missing-newline bug, so the page showed eternal
// "Connecting…" instead of the explanation.
func TestGuestGatewayUnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	gw := newLoopbackHost(t, false)
	srv := newGatewayServer(t, ctx, gw)

	// A valid peer ID the gateway has no addresses for: the dial fails fast
	// with "no addresses" rather than waiting out the 20s timeout.
	_, pub, err := p2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ghost, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	ws := dialGuestWS(t, srv, ghost.String())
	frames := readFrames(t, ws, []string{"end"})
	if !strings.Contains(frames[0].Reason, "isn't reachable") {
		t.Errorf("end reason %q should tell the guest the host is unreachable", frames[0].Reason)
	}
}
