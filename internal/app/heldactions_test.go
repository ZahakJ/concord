package app

import (
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func heldFrom(guild, fpr, kind, content string, at time.Time) heldAction {
	return heldAction{
		reason: heldForTarget, carrier: guild, guild: guild, fpr: fpr, at: at,
		m: domain.Message{ID: domain.NewID(), ChannelID: domain.NewID(), Kind: kind, ReplyTo: domain.NewID(), Content: content, Sig: make([]byte, 64)},
	}
}

func (h *heldActions) count(fpr string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, a := range h.queue {
		if a.fpr == fpr {
			n++
		}
	}
	return n
}

// One sender who keeps naming rows that never arrive fills only their own
// share of the queue, in one guild or in many, and another sender's held
// action stays.
func TestOneSenderCannotPushOutAnotherSendersHeldAction(t *testing.T) {
	var h heldActions
	now := time.Now()
	kept := heldFrom("g1", "honest", "reaction", "👍", now)
	if !h.add(kept) {
		t.Fatal("an honest reaction was not held")
	}
	for i := 0; i < 2*heldActionCap; i++ {
		h.add(heldFrom("g1", "noisy", "delete", "deleted", now))
	}
	if n := h.count("noisy"); n != heldPerSender {
		t.Errorf("one sender holds %d actions in one guild, want %d", n, heldPerSender)
	}
	for g := 0; g < 2*heldActionCap/heldPerSender; g++ {
		for i := 0; i < heldPerSender; i++ {
			h.add(heldFrom(domain.NewID(), "noisy", "pin", "pin", now))
		}
	}
	if len(h.queue) > heldActionCap {
		t.Errorf("the queue holds %d actions, cap %d", len(h.queue), heldActionCap)
	}
	got := h.take(func(a heldAction) bool { return a.target == kept.m.ReplyTo })
	if len(got) != 1 || got[0].fpr != "honest" {
		t.Errorf("another sender's actions pushed out a held action: %d left", len(got))
	}
}

// What is held is a small record: an action larger than any honest delete,
// pin or reaction is not held, and neither is an edit, whose body has no
// small bound.
func TestAnActionTooLargeToHoldIsNotHeld(t *testing.T) {
	var h heldActions
	now := time.Now()
	honest := heldFrom("g", "a", "reaction", "👨🏻‍👩🏼‍👧🏽‍👦🏾", now)
	honest.m.Name = strings.Repeat("n", maxNameBytes)
	if !h.add(honest) {
		t.Error("the largest honest reaction was not held")
	}
	if h.add(heldFrom("g", "a", "reaction", strings.Repeat("x", 600<<10), now)) {
		t.Error("a 600 KiB reaction was held")
	}
	big := heldFrom("g", "a", "delete", "deleted", now)
	big.m.Name = strings.Repeat("n", heldActionMaxBytes)
	if h.add(big) {
		t.Error("a delete with an oversized name was held")
	}
	if h.add(heldFrom("g", "a", "edit", "new text", now)) {
		t.Error("an edit was held")
	}
	withMap := heldFrom("g", "a", "reaction", "👍", now)
	withMap.m.Reactions = map[string][]string{"x": {strings.Repeat("y", 1<<20)}}
	if !h.add(withMap) {
		t.Fatal("a reaction was not held")
	}
	if got := h.take(func(a heldAction) bool { return a.target == withMap.m.ReplyTo }); len(got) != 1 || got[0].m.Reactions != nil {
		t.Error("a held action kept fields that are not part of it")
	}
}

// Removing an action clears its slot, and an expired action is freed without
// waiting for another to arrive.
func TestHeldActionsFreeWhatTheyDrop(t *testing.T) {
	var h heldActions
	now := time.Now()
	var targets []string
	for i := 0; i < 8; i++ {
		a := heldFrom("g", "a", "reaction", "👍", now)
		targets = append(targets, a.m.ReplyTo)
		h.add(a)
	}
	h.take(func(a heldAction) bool { return a.target != targets[0] })
	h.mu.Lock()
	tail := h.queue[len(h.queue):cap(h.queue)]
	for i, a := range tail {
		if a.m.ID != "" || a.senderID != nil || a.fpr != "" {
			t.Errorf("slot %d past the end still holds a removed action", i)
		}
	}
	h.mu.Unlock()

	var e heldActions
	e.add(heldFrom("g", "a", "reaction", "👍", now.Add(-heldActionTTL+50*time.Millisecond)))
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.mu.Lock()
		n := len(e.queue)
		e.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("an expired action is still held with nothing else arriving")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
