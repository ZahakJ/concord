package bridge

import (
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

// blockList is a stand-in for the service's block list. visibleViews only ever
// asks it one question, so the test does not need a running node to pin the
// behaviour that matters.
type blockList map[string]bool

func (b blockList) SenderBlocked(sender []byte) bool  { return b[string(sender)] }
func (b blockList) IsBlocked(fingerprint string) bool { return b[fingerprint] }

// The point of the filter: a blocked member's messages leave the store
// untouched and simply stop being converted into something the UI can draw.
// Everyone else in the channel is unaffected, and the surrounding conversation
// keeps its order — a hidden message must not reshuffle the ones around it.
func TestBlockedSenderIsHiddenNotDropped(t *testing.T) {
	stored := []domain.Message{
		{ID: "1", Sender: []byte("alice"), Content: "morning"},
		{ID: "2", Sender: []byte("mallory"), Content: "abuse"},
		{ID: "3", Sender: []byte("bob"), Content: "morning back"},
		{ID: "4", Sender: []byte("mallory"), Content: "more abuse"},
		{ID: "5", Sender: []byte("alice"), Content: "anyway"},
	}

	blocked := blockList{"mallory": true}
	got := visibleViews(blocked, stored)

	want := []string{"1", "3", "5"}
	if len(got) != len(want) {
		t.Fatalf("view has %d messages, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("view[%d] = %q, want %q — hiding a message reordered the feed", i, got[i].ID, id)
		}
	}

	// The store was handed in by value-slice and must come back whole: hiding
	// is a decision about this render, not an edit to the record. If this ever
	// fails, sync convergence is the next thing to break.
	if len(stored) != 5 {
		t.Fatalf("filtering mutated the caller's messages: %d left, want 5", len(stored))
	}

	// Unblocking is just the same call with an empty list — nothing has to be
	// re-fetched from a peer, because nothing was ever thrown away.
	restored := visibleViews(blockList{}, stored)
	if len(restored) != 5 {
		t.Fatalf("after unblocking the view has %d messages, want all 5 back", len(restored))
	}
	if restored[1].Content != "abuse" || restored[3].Content != "more abuse" {
		t.Fatal("unblocking restored the rows but not their content")
	}
}

// Hiding a blocked account's messages while still drawing their reactions left
// them a guaranteed way to reach the one row you are certain to read: your own.
// The tally counts them and the hover card names them, so the emoji has to go
// with the messages.
func TestBlockedReactionsAreStrippedFromEveryoneElsesMessages(t *testing.T) {
	stored := []domain.Message{{
		ID: "1", Sender: []byte("alice"), Content: "my message",
		Reactions: map[string][]string{
			"👀": {"mallory"},                    // only reactor is blocked → emoji goes
			"🔥": {"bob", "mallory"},             // tally must drop to one
			"✅": {"bob"},                        // untouched
			"💀": {"mallory", "bob", "mallory2"}, // two blocked, one kept
		},
	}}
	blocked := blockList{"mallory": true, "mallory2": true}

	got := visibleViews(blocked, stored)
	if len(got) != 1 {
		t.Fatalf("view has %d messages, want 1", len(got))
	}
	rx := got[0].Reactions
	if _, still := rx["👀"]; still {
		t.Fatal("REGRESSION: an emoji whose only reactor is blocked is still on the message")
	}
	if len(rx["🔥"]) != 1 || rx["🔥"][0] != "bob" {
		t.Fatalf("REGRESSION: blocked reactor still counted: 🔥 = %v, want [bob]", rx["🔥"])
	}
	if len(rx["✅"]) != 1 || rx["✅"][0] != "bob" {
		t.Fatalf("an unblocked reaction was disturbed: ✅ = %v", rx["✅"])
	}
	if len(rx["💀"]) != 1 || rx["💀"][0] != "bob" {
		t.Fatalf("REGRESSION: 💀 = %v, want just [bob]", rx["💀"])
	}

	// Same rule as the message filter: nothing is edited, so unblocking has the
	// whole tally back with no round trip.
	src := stored[0].Reactions
	if len(src["🔥"]) != 2 || len(src["💀"]) != 3 || len(src) != 4 {
		t.Fatal("filtering edited the stored reactions — the local block list must never reach the shared record")
	}
	back := visibleViews(blockList{}, stored)[0].Reactions
	if len(back) != 4 || len(back["💀"]) != 3 {
		t.Fatalf("unblocking did not restore the tally: %v", back)
	}
}

// An empty block list must not cost anything or change anything — this is the
// path every message on every device takes.
func TestNoBlocksMeansEveryMessageRenders(t *testing.T) {
	stored := []domain.Message{
		{ID: "1", Sender: []byte("alice")},
		{ID: "2", Sender: []byte("bob")},
	}
	if got := visibleViews(blockList{}, stored); len(got) != 2 {
		t.Fatalf("unfiltered view has %d messages, want 2", len(got))
	}
	if got := visibleViews(blockList{}, nil); len(got) != 0 {
		t.Fatalf("an empty channel produced %d messages", len(got))
	}
}
