package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Receipts and typing are frames to the conversation like any message, and a
// copy that CannotRepairHere sends none of them (mintBarred): its tree can
// still seat a member removed since. Messages still arrive at it; it only
// says nothing back. The same lanes from a healthy copy work, which is what
// makes the silence the guard's doing.
//
// The own-device lanes over Notes (read marks, hides, chat and receipt and
// typing prefs) ask mintBarred too, but no test can show it: a Notes group
// with another device of the account in it is never barred
// (onlyPossibleCommitter), and one without is alone (aloneInGuild).
func TestBarredCopySendsNoReceiptsOrTyping(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, _, _ := guardedPair(t, ctx, boot)
	dm, uCh, pCh := dmChannelFor(t, u, p)
	_ = u.SetTypingEnabled(true)
	_ = p.SetTypingEnabled(true)
	var typed atomic.Int32
	p.OnTyping(func(_, ch string, stop bool) {
		if ch == pCh && !stop {
			typed.Add(1)
		}
	})
	ru := &recorder{}
	u.OnMessage(ru.add)

	latch(t, u, dm)
	silent := sendOnce(t, p, pCh, ru, "to the barred copy")
	if err := u.sendTypingSignal(uCh, typingStart); !errors.Is(err, ErrBehindOnlyCommitter) {
		t.Errorf("typing from a barred copy: err=%v, want ErrBehindOnlyCommitter", err)
	}
	time.Sleep(3 * time.Second)
	if v, _ := p.Receipts(pCh); v[silent.ID].Delivered {
		t.Error("a delivered receipt left the barred copy")
	}
	if n := typed.Load(); n != 0 {
		t.Errorf("%d typing frames left the barred copy", n)
	}

	u.clearBehind(dm)
	heard := sendOnce(t, p, pCh, ru, "to the healthy copy")
	waitUntil(t, 20*time.Second, func() bool {
		v, _ := p.Receipts(pCh)
		return v[heard.ID].Delivered
	}, "a healthy copy's delivered receipt never arrived")
	waitUntil(t, 20*time.Second, func() bool {
		_ = u.sendTypingSignal(uCh, typingStart)
		return typed.Load() > 0
	}, "a healthy copy's typing never arrived")
}
