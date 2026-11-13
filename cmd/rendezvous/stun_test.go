package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

// TestSTUNAnswersBindingWithTheSendersAddress: the responder exists so a call
// can learn its external address from this node instead of a third party. Ask
// it over a real UDP socket and check it names the socket we asked from.
func TestSTUNAnswersBindingWithTheSendersAddress(t *testing.T) {
	srv, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go answerSTUN(srv)

	c, err := net.Dial("udp", srv.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if _, err := c.Write(req.Raw); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("no answer to a Binding request: %v", err)
	}
	res := &stun.Message{Raw: buf[:n]}
	if err := res.Decode(); err != nil {
		t.Fatal(err)
	}
	if res.Type != stun.BindingSuccess || res.TransactionID != req.TransactionID {
		t.Fatalf("answer is %v for transaction %x, want a Binding success for %x", res.Type, res.TransactionID, req.TransactionID)
	}
	var got stun.XORMappedAddress
	if err := got.GetFrom(res); err != nil {
		t.Fatal(err)
	}
	me := c.LocalAddr().(*net.UDPAddr)
	if !got.IP.Equal(me.IP) || got.Port != me.Port {
		t.Fatalf("answer names %v:%d, the request came from %v", got.IP, got.Port, me)
	}

	// Anything that is not a Binding request gets silence, not an error reply:
	// a responder that answers junk is a reflector.
	for _, junk := range [][]byte{
		[]byte("hello"),
		stun.MustBuild(stun.TransactionID, stun.NewType(stun.MethodAllocate, stun.ClassRequest)).Raw,
		stun.MustBuild(stun.TransactionID, stun.BindingSuccess).Raw,
	} {
		if _, err := c.Write(junk); err != nil {
			t.Fatal(err)
		}
	}
	_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if n, err := c.Read(buf); err == nil {
		t.Fatalf("got %d bytes back for something that is not a Binding request", n)
	}
}

// TestSTUNBudgetPerSource: one source address gets its burst and then silence,
// so a forged source cannot be made the target of an endless stream of answers.
func TestSTUNBudgetPerSource(t *testing.T) {
	srv, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go answerSTUN(srv)
	c, err := net.Dial("udp", srv.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const sent = 3 * int(stunBurst)
	for i := 0; i < sent; i++ {
		if _, err := c.Write(stun.MustBuild(stun.TransactionID, stun.BindingRequest).Raw); err != nil {
			t.Fatal(err)
		}
	}
	got := 0
	buf := make([]byte, 1500)
	for {
		_ = c.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		if _, err := c.Read(buf); err != nil {
			break
		}
		got++
	}
	// The bucket refills at stunRate while we send, so allow a couple over.
	if got < 1 || got > int(stunBurst)+3 {
		t.Fatalf("%d answers to %d requests from one address, want about the burst of %d", got, sent, int(stunBurst))
	}
}

// TestTURNConfigNamesNoThirdParty: /turn is where every call gets its ICE
// servers. It used to list a public STUN server run by a third party, which put
// that server past the user's "No third-party call servers" switch. Every
// server it lists must be this node.
func TestTURNConfigNamesNoThirdParty(t *testing.T) {
	servers := func(ts *turnServer) []iceServer {
		w := httptest.NewRecorder()
		ts.handleTURNCreds(w, httptest.NewRequest(http.MethodGet, "/turn", nil))
		var out turnCredsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.IceServers
	}
	check := func(list []iceServer) {
		t.Helper()
		for _, s := range list {
			for _, u := range s.URLs {
				if !strings.HasPrefix(u, "turn:concord.example:") && !strings.HasPrefix(u, "stun:concord.example:") {
					t.Errorf("/turn lists %q, which is not this node", u)
				}
			}
		}
	}

	off := servers(&turnServer{secret: "s", public: "concord.example", port: "3478"})
	check(off)
	if len(off) != 1 || off[0].URLs[0] != "turn:concord.example:3478?transport=tcp" {
		t.Fatalf("without a STUN responder /turn should list the relay alone, got %+v", off)
	}

	// The responder without the relay: /turn lists it alone, and no relay.
	alone := servers(&turnServer{public: "concord.example", stunPort: "3479"})
	check(alone)
	if len(alone) != 1 || alone[0].URLs[0] != "stun:concord.example:3479" || alone[0].Credential != "" {
		t.Fatalf("a node with the responder and no relay should list the responder alone, got %+v", alone)
	}

	on := servers(&turnServer{secret: "s", public: "concord.example", port: "3478", stunPort: "3479"})
	check(on)
	if len(on) != 2 || on[0].URLs[0] != "stun:concord.example:3479" || on[0].Username != "" {
		t.Fatalf("with a STUN responder /turn should list it first and without credentials, got %+v", on)
	}
}

// TestServeSTUNOffByDefault: no port configured, no socket opened.
func TestServeSTUNOffByDefault(t *testing.T) {
	if got := serveSTUN(context.Background(), "", ""); got != "" {
		t.Fatalf("serveSTUN with no port returned %q", got)
	}
	if got := serveSTUN(context.Background(), "", "not-a-port"); got != "" {
		t.Fatalf("serveSTUN with a bad port returned %q", got)
	}
}
