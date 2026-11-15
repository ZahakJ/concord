package app

import (
	"context"
	"testing"
	"time"
)

// ZW-05. While presence is invisible a device sends no delivered ack and no
// read receipt: each says it is online now. They are held, not dropped, and go
// out when the user is visible again.
func TestInvisibleHoldsAcksAndReadReceiptsUntilVisible(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b, spy, dm, aCh, bCh := receiptPair(t, ctx)
	group := rhGroup(a, dm)
	for _, s := range []*Service{a, b} {
		if err := s.SetReadReceiptsEnabled(true); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.SetDisplayName("A"); err != nil {
		t.Fatal(err)
	}
	presence := func(v string) {
		t.Helper()
		p := a.SelfProfile()
		p.Presence = v
		if err := a.SetProfile(p); err != nil {
			t.Fatalf("SetProfile(%q): %v", v, err)
		}
	}

	presence("invisible")
	sent, err := b.SendMessage(bCh, "while invisible", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return hasMessage(a, aCh, "while invisible") },
		"setup: A never received B's message")
	if _, err := a.MarkRead(aCh, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	if n, r := spy.count(group, "delivered"), spy.count(group, "read_receipt"); n != 0 || r != 0 {
		t.Errorf("an invisible device encrypted %d delivered acks and %d read receipts", n, r)
	}
	if v := tickOf(t, b, bCh, sent.ID); v.Delivered || v.Read {
		t.Errorf("B holds receipts from an invisible device: %+v", v)
	}

	presence("online")
	waitUntil(t, 30*time.Second, func() bool {
		v := tickOf(t, b, bCh, sent.ID)
		return v.Delivered && v.Read
	}, "what was held while invisible never reached B once A was visible")
}
