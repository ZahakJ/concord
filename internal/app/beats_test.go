package app

import (
	"sync"
	"testing"
	"time"
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
