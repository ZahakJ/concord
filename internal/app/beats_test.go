package app

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/identity"
)

// ZW-14. Close stops the receipt, hide and read-marker beats, and none is
// armed after it: a beat that fired later would run against a closed store and
// engine.
func TestCloseStopsTheBeatTimers(t *testing.T) {
	r := newReceiptRig(t)
	m := r.send(t, r.bob, time.Now())
	r.s.noteDelivered(m)
	_ = r.s.SetReadReceiptsEnabled(true)
	if _, err := r.s.MarkRead(r.ch, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := r.s.HideMessage(r.ch, m.ID); err != nil {
		t.Fatal(err)
	}
	other := r.send(t, r.bob, time.Now())
	armed := func() (n int) {
		for _, b := range []struct {
			mu *sync.Mutex
			t  **time.Timer
		}{{&r.s.receiptMu, &r.s.receiptTimer}, {&r.s.hideMu, &r.s.hideTimer}, {&r.s.readMarkMu, &r.s.readMarkTimer}} {
			b.mu.Lock()
			if *b.t != nil {
				n++
			}
			b.mu.Unlock()
		}
		return n
	}
	if n := armed(); n != 3 {
		t.Fatalf("setup: %d of 3 beats armed", n)
	}
	_ = r.s.Close()
	if n := armed(); n != 0 {
		t.Fatalf("%d beats still armed after Close", n)
	}
	r.s.noteDelivered(other)
	if n := armed(); n != 0 {
		t.Fatal("a beat was armed after Close")
	}
}

// holdingSpy holds the first hide_marker frame's encryption until release is
// closed, and records what it saw when it went on.
type holdingSpy struct {
	mls.Engine
	once     sync.Once
	entered  chan struct{}
	release  chan struct{}
	finished chan struct{}
	closed   *atomic.Bool
	closedAt atomic.Bool // Close had returned when the held encryption went on
	err      error
}

func (sp *holdingSpy) Encrypt(ctx context.Context, gid mls.GroupID, plaintext []byte) ([]byte, error) {
	var m struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(plaintext, &m) == nil && m.Type == "hide_marker" {
		held := false
		sp.once.Do(func() { held = true })
		if held {
			close(sp.entered)
			<-sp.release
			// Give a Close that does not wait the time to finish.
			for end := time.Now().Add(5 * time.Second); time.Now().Before(end) && !sp.closed.Load(); {
				time.Sleep(20 * time.Millisecond)
			}
			sp.closedAt.Store(sp.closed.Load())
			// The service's context is cancelled by then; the engine must
			// still be open.
			ct, err := sp.Engine.Encrypt(context.Background(), gid, plaintext)
			sp.err = err
			close(sp.finished)
			return ct, err
		}
	}
	return sp.Engine.Encrypt(ctx, gid, plaintext)
}

// A flush already running when Close is called finishes before Close returns:
// the engine, host and store it uses are still open while it runs.
func TestCloseWaitsForARunningBeat(t *testing.T) {
	var closed atomic.Bool
	spy := &holdingSpy{entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}), closed: &closed}
	s, err := Start(context.Background(), Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true,
		wrapMLS: func(e mls.Engine) mls.Engine { spy.Engine = e; return spy }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ensureNotesDM(); err != nil {
		t.Fatal(err)
	}
	if err := s.HideMessage("some-channel", "some-message"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-spy.entered:
	case <-time.After(10 * time.Second):
		_ = s.Close()
		t.Fatal("setup: the hide beat never reached the engine")
	}
	done := make(chan struct{})
	go func() {
		close(spy.release) // Close has been called
		_ = s.Close()
		closed.Store(true)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Close did not return")
	}
	select {
	case <-spy.finished:
	case <-time.After(30 * time.Second):
		t.Fatal("the held flush never went on")
	}
	if spy.closedAt.Load() {
		t.Fatal("Close returned while a beat's flush was still running")
	}
	if spy.err != nil {
		t.Fatalf("the running flush found the engine closed: %v", spy.err)
	}
}

// stallingSpy holds the first hide_marker frame's encryption until release is
// closed, then goes on with the context it was given and records the result.
type stallingSpy struct {
	mls.Engine
	once     sync.Once
	entered  chan struct{}
	release  chan struct{}
	finished chan struct{}
	err      error
}

func (sp *stallingSpy) Encrypt(ctx context.Context, gid mls.GroupID, plaintext []byte) ([]byte, error) {
	var m struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(plaintext, &m) == nil && m.Type == "hide_marker" {
		held := false
		sp.once.Do(func() { held = true })
		if held {
			close(sp.entered)
			<-sp.release
			ct, err := sp.Engine.Encrypt(ctx, gid, plaintext)
			sp.err = err
			close(sp.finished)
			return ct, err
		}
	}
	return sp.Engine.Encrypt(ctx, gid, plaintext)
}

// A flush stalled in the engine holds neither the self-erase nor a Close
// called beside it past their bounds, and it sends nothing once it goes on:
// the erase has run by then, and the engine and host are closed.
func TestCloseAndSelfEraseDoNotWaitOnAStalledBeat(t *testing.T) {
	origBeat, origErase := closeBeatWait, closeEraseWait
	closeBeatWait, closeEraseWait = 300*time.Millisecond, 2*time.Second
	defer func() { closeBeatWait, closeEraseWait = origBeat, origErase }()

	spy := &stallingSpy{entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	released := false
	release := func() {
		if !released {
			released = true
			close(spy.release)
		}
	}
	defer release()
	dir := t.TempDir()
	s, err := Start(context.Background(), Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true,
		wrapMLS: func(e mls.Engine) mls.Engine { spy.Engine = e; return spy }})
	if err != nil {
		t.Fatal(err)
	}
	dev := s.id.DevicePublicKey()
	if err := saveDeviceMarker(dir, s.id.IssueDeviceCertFor(dev, "phone", time.Now().Add(-time.Hour).Unix())); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ensureNotesDM(); err != nil {
		t.Fatal(err)
	}
	if err := s.HideMessage("some-channel", "some-message"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-spy.entered:
	case <-time.After(10 * time.Second):
		_ = s.Close()
		t.Fatal("setup: the hide beat never reached the engine")
	}
	s.saveRevocations([]*identity.DeviceRevocation{s.id.RevokeDevice(dev, time.Now().Unix())})
	if ok, _ := s.selfRevoked(); !ok {
		t.Fatal("setup: the stored revocation does not apply")
	}

	// The self-erase and a Close beside it, both while the flush is held.
	start := time.Now()
	erased, closed := make(chan error, 1), make(chan struct{})
	go func() { erased <- s.wipeSelf() }()
	go func() { _ = s.Close(); close(closed) }()
	bound := closeEraseWait + 2*time.Second
	var eraseErr error
	for erased != nil || closed != nil {
		select {
		case eraseErr = <-erased:
			erased = nil
		case <-closed:
			closed = nil
		case <-time.After(bound - time.Since(start)):
			t.Fatalf("after %v with a beat's flush stalled: self-erase returned %v, Close returned %v",
				time.Since(start).Round(time.Millisecond), erased == nil, closed == nil)
		}
	}
	if eraseErr != nil {
		t.Fatalf("self-erase: %v", eraseErr)
	}
	onlyLockLeft(t, dir, "after the self-erase")

	// The abandoned flush goes on now, after the erase: it can send nothing.
	release()
	select {
	case <-spy.finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the held flush never went on")
	}
	if spy.err == nil {
		t.Fatal("a flush abandoned by Close encrypted a frame after the erase")
	}
	if n := len(s.host.Peers()); n != 0 {
		t.Fatalf("the host still has %d peers after Close", n)
	}
	time.Sleep(200 * time.Millisecond)
	onlyLockLeft(t, dir, "after the abandoned flush went on")
}
