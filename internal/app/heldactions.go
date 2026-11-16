package app

import (
	"sync"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// A live message action (delete, pin, reaction, edit) can arrive before what
// it depends on, and a live frame is delivered once:
//
//   - its target row, when the two travel separately: a mailbox drain across
//     several nodes, or a drain racing a catch-up;
//   - its sender's readmit, when a removed member was let back in and the MLS
//     commit that re-added them reached this device before the governance op
//     that lifts the removal. The two travel on different lanes.
//
// Such an action is kept here for a short while and delivered again, through
// the whole of deliverMessage, when the row arrives live or the readmit is
// applied. Nothing is granted by being held: every check runs again on the
// second delivery. An action that is still waiting when it expires, or that is
// pushed out by newer ones, is dropped, as it was before.
//
// A target that arrives by history sync releases nothing: the served row
// carries its reactions, pin and deletion as the source holds them, and
// replaying a toggle on top of that could undo it. The held actions for that
// row are dropped instead.
const (
	heldActionTTL = 2 * time.Minute
	heldActionCap = 256
)

type heldAction struct {
	carrier  string // guild whose group carried the frame
	m        domain.Message
	senderID []byte
	// What releases it: the target row, or the readmit of fpr in guild.
	target string
	guild  string
	fpr    string
	at     time.Time
}

type heldActions struct {
	mu    sync.Mutex
	queue []heldAction // oldest first
}

func (h *heldActions) add(a heldAction) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked(a.at)
	if len(h.queue) >= heldActionCap {
		h.queue = h.queue[1:]
	}
	h.queue = append(h.queue, a)
}

func (h *heldActions) pruneLocked(now time.Time) {
	keep := h.queue[:0]
	for _, a := range h.queue {
		if now.Sub(a.at) < heldActionTTL {
			keep = append(keep, a)
		}
	}
	h.queue = keep
}

// take removes and returns, in arrival order, the held actions match selects.
func (h *heldActions) take(match func(heldAction) bool) []heldAction {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked(time.Now())
	var out []heldAction
	keep := h.queue[:0]
	for _, a := range h.queue {
		if match(a) {
			out = append(out, a)
		} else {
			keep = append(keep, a)
		}
	}
	h.queue = keep
	return out
}

// holdForTarget keeps an action whose target row is not held yet.
func (s *Service) holdForTarget(carrier string, m domain.Message, senderID []byte) {
	s.held.add(heldAction{carrier: carrier, m: m, senderID: senderID, target: m.ReplyTo, at: time.Now()})
}

// holdForReadmit keeps an action from a member this device holds as removed
// while the MLS group already holds them again.
func (s *Service) holdForReadmit(carrier, guildID, fpr string, m domain.Message, senderID []byte) {
	s.held.add(heldAction{carrier: carrier, m: m, senderID: senderID, guild: guildID, fpr: fpr, at: time.Now()})
}

// releaseForTarget delivers again the actions waiting for a row that arrived live.
func (s *Service) releaseForTarget(targetID string) {
	for _, a := range s.held.take(func(a heldAction) bool { return a.target == targetID }) {
		s.deliverMessage(a.carrier, a.m, a.senderID)
	}
}

// dropForTarget forgets the actions waiting for a row that arrived by sync.
func (s *Service) dropForTarget(targetID string) {
	s.held.take(func(a heldAction) bool { return a.target == targetID })
}

// releaseForReadmit delivers again the actions waiting for fpr's readmit.
func (s *Service) releaseForReadmit(guildID, fpr string) {
	for _, a := range s.held.take(func(a heldAction) bool { return a.guild == guildID && a.fpr == fpr }) {
		s.deliverMessage(a.carrier, a.m, a.senderID)
	}
}
