package app

import (
	"testing"
	"time"
)

// ZW-04. A change made here wins over the stamp held, even when that stamp came
// from a sibling whose clock runs ahead (within readSkew). The sibling's copy,
// offered again, does not turn the setting back.
func TestReceiptsPrefLocalChangeBeatsAFastSibling(t *testing.T) {
	r := newReceiptRig(t)
	ahead := receiptsPref{On: true, At: time.Now().Add(90 * time.Second).UnixMilli()}
	if err := r.s.applyReceiptsPref(ahead, false); err != nil {
		t.Fatal(err)
	}
	if !r.s.ReadReceiptsEnabled() {
		t.Fatal("setup: the sibling's ON was not taken")
	}
	if err := r.s.SetReadReceiptsEnabled(false); err != nil {
		t.Fatal(err)
	}
	cur := r.s.currentReceiptsPref()
	if cur == nil || cur.On || cur.At <= ahead.At {
		t.Fatalf("a later local OFF carries %+v, not a stamp after the sibling's %d", cur, ahead.At)
	}
	_ = r.s.applyReceiptsPref(ahead, false) // the sibling's next hello
	if r.s.ReadReceiptsEnabled() {
		t.Fatal("the sibling's older ON reverted a later local OFF")
	}
}
