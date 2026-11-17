package net

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/ZahakJ/concord/internal/identity"
)

// pairOn starts two hosts listening on addr and connects a to b.
func pairOn(t *testing.T, addr string) (a, b *Host) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	a, err := New(ctx, Config{Identity: idA, ListenAddrs: []string{addr}})
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	b, err = New(ctx, Config{Identity: idB, ListenAddrs: []string{addr}})
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	if err := a.Connect(ctx, b.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return a, b
}

var transports = []struct{ name, addr string }{
	{"quic", "/ip4/127.0.0.1/udp/0/quic-v1"},
	{"tcp", "/ip4/127.0.0.1/tcp/0"},
}

// TestRequesterBefore061GetsEveryAnswer plays the requester that 0.60.x
// ships: it writes its request, half-closes, and gives up if the half-close
// fails. It is held between the two until the responder has answered, which
// is the order load produces. Every request/response responder must still be
// reading when the half-close arrives, so the half-close succeeds and the
// answer is read.
func TestRequesterBefore061GetsEveryAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	for _, tc := range transports {
		t.Run(tc.name, func(t *testing.T) {
			a, b := pairOn(t, tc.addr)
			answer := bytes.Repeat([]byte("answer "), 300)
			answered := make(chan struct{}, 1)
			respond := func(context.Context, peer.ID, []byte) ([]byte, error) {
				answered <- struct{}{}
				return answer, nil
			}
			b.HandleAttachments(respond)
			b.HandleChronicle(respond)
			b.HandleDMInvites(respond)
			ServeGifSearch(b.ctx, b.h, respond)
			b.HandleHello(respond)
			b.HandleInvites(respond)
			b.HandleLink(respond)
			b.HandleProfile(respond)
			b.HandleRelease(respond)
			b.HandleSync(respond)

			for _, proto := range []protocol.ID{
				attachProtocol, chronicleProtocol, dmInviteProtocol, gifSearchProtocol,
				helloProtocol, inviteProtocol, linkProtocol, profileProtocol,
				releaseProtocol, syncProtocol,
			} {
				t.Run(string(proto), func(t *testing.T) {
					got, err := requestAsBefore061(t, a, b.PeerID(), proto, answered)
					if err != nil {
						t.Fatalf("the responder answered before the half-close and the request failed: %v", err)
					}
					if !bytes.Equal(got, answer) {
						t.Fatalf("got %d bytes back, want the %d-byte answer", len(got), len(answer))
					}
				})
			}
		})
	}
}

// requestAsBefore061 is sendRequest and readFrame as 0.60.x had them: a
// failed half-close fails the request.
func requestAsBefore061(t *testing.T, a *Host, to peer.ID, proto protocol.ID, answered <-chan struct{}) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := a.newStream(ctx, to, proto)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(20 * time.Second))
	if err := writeFrame(s, []byte(`{"q":"x"}`), 1<<10); err != nil {
		return nil, err
	}
	select {
	case <-answered:
	case <-time.After(10 * time.Second):
		return nil, errors.New("the responder was never asked")
	}
	// The responder writes and closes as soon as its function returns; give
	// that a moment to reach us.
	time.Sleep(300 * time.Millisecond)
	if err := s.CloseWrite(); err != nil {
		return nil, err
	}
	return readFrame(s, 1<<20)
}

// TestEndAnswerDoesNotHoldTheAnswer measures what the wait costs. A current
// requester half-closes at once, so the responder's slot is free within a
// moment of the answer and the round trip is unchanged. A requester that
// never half-closes still gets its answer at once, and the responder lets
// go of the stream after answerDrain.
func TestEndAnswerDoesNotHoldTheAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	for _, tc := range transports {
		t.Run(tc.name, func(t *testing.T) {
			a, b := pairOn(t, tc.addr)
			page := bytes.Repeat([]byte("p"), 8<<10)
			b.HandleChronicle(func(context.Context, peer.ID, []byte) ([]byte, error) {
				return page, nil
			})
			ctx := context.Background()

			const rounds = 40
			var took []time.Duration
			for range rounds {
				start := time.Now()
				got, err := a.RequestChronicleChunk(ctx, b.PeerID(), []byte(`{"chunk":"x"}`))
				took = append(took, time.Since(start))
				if err != nil || !bytes.Equal(got, page) {
					t.Fatalf("request: %d bytes, %v", len(got), err)
				}
			}
			sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
			t.Logf("current requester, %d rounds: median %v, max %v", rounds, took[rounds/2], took[rounds-1])
			waitDrained(t, time.Second, "after current requesters")

			// A requester that never half-closes.
			s, err := a.newStream(ctx, b.PeerID(), chronicleProtocol)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer s.Close()
			_ = s.SetDeadline(time.Now().Add(20 * time.Second))
			start := time.Now()
			if err := writeFrame(s, []byte(`{"chunk":"x"}`), 1<<10); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := readFrame(s, 1<<20)
			if err != nil || !bytes.Equal(got, page) {
				t.Fatalf("read: %d bytes, %v", len(got), err)
			}
			answerIn := time.Since(start)
			if answerIn > time.Second {
				t.Fatalf("the answer took %v to a requester that has not half-closed", answerIn)
			}
			// The responder ended its side once it had answered.
			if _, err := s.Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("after the answer: %v, want EOF", err)
			}
			if len(draining) != 1 {
				t.Fatalf("%d responders waiting, want 1", len(draining))
			}
			waitDrained(t, answerDrain+2*time.Second, "after answerDrain")
			t.Logf("requester that never half-closes: answer in %v, responder let go after %v", answerIn, time.Since(start))
		})
	}
}

func waitDrained(t *testing.T, within time.Duration, when string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for len(draining) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d responders still waiting after %v", when, len(draining), within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// fakeStream records what EndAnswer does to a stream.
type fakeStream struct {
	network.Stream
	closedWrite  bool
	readDeadline time.Time
	read         int
	endless      bool
}

func (f *fakeStream) CloseWrite() error { f.closedWrite = true; return nil }
func (f *fakeStream) SetReadDeadline(d time.Time) error {
	f.readDeadline = d
	return nil
}
func (f *fakeStream) Read(p []byte) (int, error) {
	if !f.endless {
		return 0, io.EOF
	}
	f.read += len(p)
	return len(p), nil
}

func TestEndAnswerIsBounded(t *testing.T) {
	t.Run("waits at most answerDrain", func(t *testing.T) {
		f := &fakeStream{}
		before := time.Now()
		EndAnswer(f)
		if !f.closedWrite {
			t.Fatal("did not half-close")
		}
		if f.readDeadline.IsZero() || f.readDeadline.After(before.Add(answerDrain+time.Second)) {
			t.Fatalf("read deadline %v, want within %v", f.readDeadline.Sub(before), answerDrain)
		}
	})
	t.Run("stops reading a requester that keeps sending", func(t *testing.T) {
		f := &fakeStream{endless: true}
		EndAnswer(f)
		if f.read > answerDrainBytes+64<<10 {
			t.Fatalf("read %d bytes, want about %d", f.read, answerDrainBytes)
		}
	})
	t.Run("closes at once past maxDraining", func(t *testing.T) {
		for range maxDraining {
			draining <- struct{}{}
		}
		defer func() {
			for range maxDraining {
				<-draining
			}
		}()
		f := &fakeStream{}
		EndAnswer(f)
		if f.closedWrite || !f.readDeadline.IsZero() {
			t.Fatal("waited with maxDraining streams already waiting")
		}
	})
	if len(draining) != 0 {
		t.Fatalf("%d slots left taken", len(draining))
	}
}
