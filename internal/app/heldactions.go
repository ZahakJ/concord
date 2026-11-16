package app

import (
	"sync"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// A live delete, pin or reaction can arrive before what it depends on, and a
// live frame is delivered once:
//
//   - its target row, when the two travel separately: a mailbox drain across
//     several nodes, or a drain racing a catch-up;
//   - its sender's readmit, when a removed member was let back in and the MLS
//     commit that re-added them reached this device before the governance op
//     that lifts the removal. The two travel on different lanes.
//
// Such an action is kept here for a short while and delivered again, through
// the whole of deliverMessage, when the row arrives or the readmit is applied.
// Nothing is granted by being held: every check runs again on the second
// delivery. An action that is still waiting when it expires, or that is pushed
// out by newer ones from the same sender, is dropped, as it was before.
//
// Only a small fixed record is held. Edits are not: an edit carries a whole
// message body, which has no small bound. An edit that outruns its target is
// dropped.
//
// A target that arrives by history sync releases its held deletes; a delete
// is idempotent and its checks run again. Its held reactions and pins are
// dropped instead: the served row carries them as the source holds them, and
// replaying a toggle on top of that could undo it.
const (
	heldActionTTL = 2 * time.Minute
	// heldActionCap bounds the whole queue, heldPerSender what one sender may
	// hold in one guild. A sender at its cap loses its own oldest entry; a full
	// queue loses the oldest entry of the sender holding the most.
	heldActionCap = 256
	heldPerSender = 16
	// heldActionMaxBytes bounds the variable fields of a held action: its id,
	// channel, target, sender name, content, direction and signature. An
	// honest one needs at most about 270 bytes: three 32-byte ids, a name of
	// at most maxNameBytes, a 64-byte signature, and a body that is "deleted",
	// "pin" or one emoji (a :custom: name is at most 34 bytes, the longest
	// standard emoji sequence about 40). Anything larger is not held.
	heldActionMaxBytes = 512
)

type heldReason uint8

const (
	heldForTarget heldReason = iota + 1
	heldForReadmit
)

type heldAction struct {
	reason   heldReason
	carrier  string // guild whose group carried the frame
	guild    string // guild of the channel it names
	fpr      string // its sender's account fingerprint
	target   string // the row it names
	m        domain.Message
	senderID []byte
	at       time.Time
}

type heldActions struct {
	mu    sync.Mutex
	queue []heldAction // oldest first
	timer *time.Timer  // the next expiry, while anything is held
}

// holdable reports whether m is a kind that is held and small enough to hold.
func holdable(m domain.Message) bool {
	switch m.Kind {
	case "delete", "pin", "reaction":
	default:
		return false
	}
	if m.ID == "" || m.ReplyTo == "" {
		return false
	}
	n := len(m.ID) + len(m.ChannelID) + len(m.ReplyTo) + len(m.Name) + len(m.Content) + len(m.Dir) + len(m.Sig)
	return n <= heldActionMaxBytes
}

// heldCopy keeps only what deliverMessage reads from a held action. The sender
// is set again from the MLS credential on delivery.
func heldCopy(m domain.Message) domain.Message {
	return domain.Message{
		ID: m.ID, ChannelID: m.ChannelID, Name: m.Name, Kind: m.Kind,
		ReplyTo: m.ReplyTo, Content: m.Content, Dir: m.Dir, Sent: m.Sent, Sig: m.Sig,
	}
}

// add holds a, and reports whether it did.
func (h *heldActions) add(a heldAction) bool {
	if !holdable(a.m) || a.guild == "" || a.fpr == "" {
		return false
	}
	a.m = heldCopy(a.m)
	a.target = a.m.ReplyTo
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked(a.at)
	n, oldest := 0, -1
	for i, b := range h.queue {
		if b.guild == a.guild && b.fpr == a.fpr {
			if oldest < 0 {
				oldest = i
			}
			n++
		}
	}
	if n >= heldPerSender {
		h.removeLocked(oldest)
	} else if len(h.queue) >= heldActionCap {
		h.removeLocked(h.busiestLocked())
	}
	h.queue = append(h.queue, a)
	h.scheduleLocked()
	return true
}

// busiestLocked is the index of the oldest entry of the sender holding the
// most entries across every guild.
func (h *heldActions) busiestLocked() int {
	count := map[string]int{}
	for _, b := range h.queue {
		count[b.fpr]++
	}
	best := 0
	for i, b := range h.queue {
		if count[b.fpr] > count[h.queue[best].fpr] {
			best = i
		}
	}
	return best
}

// removeLocked drops entry i and clears the slot it frees.
func (h *heldActions) removeLocked(i int) {
	last := len(h.queue) - 1
	copy(h.queue[i:], h.queue[i+1:])
	h.queue[last] = heldAction{}
	h.queue = h.queue[:last]
}

// filterLocked keeps the entries keep accepts, in order, and clears the slots
// it frees so nothing removed stays reachable.
func (h *heldActions) filterLocked(keep func(heldAction) bool) {
	kept := h.queue[:0]
	for _, a := range h.queue {
		if keep(a) {
			kept = append(kept, a)
		}
	}
	clear(h.queue[len(kept):])
	h.queue = kept
}

func (h *heldActions) pruneLocked(now time.Time) {
	h.filterLocked(func(a heldAction) bool { return now.Sub(a.at) < heldActionTTL })
}

// scheduleLocked arms a timer for the oldest entry's expiry, so what expires
// is freed without waiting for the next arrival.
func (h *heldActions) scheduleLocked() {
	if h.timer != nil || len(h.queue) == 0 {
		return
	}
	h.timer = time.AfterFunc(time.Until(h.queue[0].at.Add(heldActionTTL)), h.expire)
}

func (h *heldActions) expire() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timer = nil
	h.pruneLocked(time.Now())
	h.scheduleLocked()
}

// take removes and returns, in arrival order, the held actions match selects.
func (h *heldActions) take(match func(heldAction) bool) []heldAction {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked(time.Now())
	var out []heldAction
	h.filterLocked(func(a heldAction) bool {
		if match(a) {
			out = append(out, a)
			return false
		}
		return true
	})
	return out
}

// holdForTarget keeps an action whose target row is not held yet. The row may
// have been stored between the caller's lookup and the add, by a lane whose
// release then found nothing, so it is looked up again after.
func (s *Service) holdForTarget(carrier, guildID, fpr string, m domain.Message, senderID []byte) {
	if !s.held.add(heldAction{reason: heldForTarget, carrier: carrier, guild: guildID, fpr: fpr, m: m, senderID: senderID, at: time.Now()}) {
		return
	}
	if _, ok, err := s.store.MessageByID(m.ReplyTo); err == nil && ok {
		s.releaseForTarget(m.ReplyTo)
	}
}

// holdForReadmit keeps an action from a member this device holds as removed
// while the MLS group already holds them again.
func (s *Service) holdForReadmit(carrier, guildID, fpr string, m domain.Message, senderID []byte) {
	s.held.add(heldAction{reason: heldForReadmit, carrier: carrier, guild: guildID, fpr: fpr, m: m, senderID: senderID, at: time.Now()})
}

// releaseForTarget delivers again the actions waiting for a row that arrived.
// deliverMessage refuses an unsigned frame from a flagged leaf before it holds
// anything, and a seat flag bears only on unsigned frames, so held actions are
// delivered again as unflagged.
func (s *Service) releaseForTarget(targetID string) {
	if targetID == "" {
		return
	}
	for _, a := range s.held.take(func(a heldAction) bool { return a.reason == heldForTarget && a.target == targetID }) {
		s.deliverMessage(a.carrier, a.m, a.senderID, false)
	}
}

// releaseSyncedTarget settles the actions waiting for a row that arrived by
// history sync: deletes are delivered again, reactions and pins dropped.
func (s *Service) releaseSyncedTarget(targetID string) {
	if targetID == "" {
		return
	}
	for _, a := range s.held.take(func(a heldAction) bool { return a.reason == heldForTarget && a.target == targetID }) {
		if a.m.Kind == "delete" {
			s.deliverMessage(a.carrier, a.m, a.senderID, false)
		}
	}
}

// releaseForReadmit delivers again the actions waiting for fpr's readmit.
func (s *Service) releaseForReadmit(guildID, fpr string) {
	if guildID == "" || fpr == "" {
		return
	}
	for _, a := range s.held.take(func(a heldAction) bool {
		return a.reason == heldForReadmit && a.guild == guildID && a.fpr == fpr
	}) {
		s.deliverMessage(a.carrier, a.m, a.senderID, false)
	}
}
