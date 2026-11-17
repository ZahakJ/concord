package mailbox

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

// TestCallerBefore061GetsTheAnswer plays the mailbox caller that 0.60.x
// ships: it writes its request, half-closes, and gives up if the half-close
// fails. It is held between the two until the node has answered, the order
// load produces. The node must still be reading when the half-close arrives.
func TestCallerBefore061GetsTheAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	for _, tc := range []struct{ name, addr string }{
		{"quic", "/ip4/127.0.0.1/udp/0/quic-v1"},
		{"tcp", "/ip4/127.0.0.1/tcp/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			node, err := libp2p.New(libp2p.ListenAddrStrings(tc.addr))
			if err != nil {
				t.Fatalf("node: %v", err)
			}
			defer node.Close()
			caller, err := libp2p.New(libp2p.ListenAddrStrings(tc.addr))
			if err != nil {
				t.Fatalf("caller: %v", err)
			}
			defer caller.Close()
			NewService(New()).Attach(ctx, node)
			if err := caller.Connect(ctx, peer.AddrInfo{ID: node.ID(), Addrs: node.Addrs()}); err != nil {
				t.Fatalf("connect: %v", err)
			}

			s, err := caller.NewStream(ctx, node.ID(), Protocol)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer s.Close()
			_ = s.SetDeadline(time.Now().Add(20 * time.Second))
			body, _ := json.Marshal(Request{Op: "register"})
			if err := writeFrame(s, body); err != nil {
				t.Fatalf("write: %v", err)
			}
			// Registering is immediate; by now the node has answered.
			time.Sleep(300 * time.Millisecond)
			if err := s.CloseWrite(); err != nil {
				t.Fatalf("the node answered before the half-close and the request failed: %v", err)
			}
			out, err := readFrame(s)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var resp Response
			if err := json.Unmarshal(out, &resp); err != nil || !resp.OK {
				t.Fatalf("answer %q: %v", out, err)
			}
		})
	}
}
