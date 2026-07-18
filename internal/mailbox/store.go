// Package mailbox is Concord's optional store-and-forward layer, run on the
// untrusted rendezvous node. When a member is offline, senders deposit an
// end-to-end-encrypted envelope addressed to a 16-byte mailbox tag; the
// recipient drains it on reconnect. The node never sees plaintext, group
// membership, or who is talking to whom — envelopes are sealed to the
// recipient's mailbox key and addressed by an opaque tag it cannot reverse.
//
// The store is bounded and in-memory (no disk, no cost to grow): short TTLs
// plus hard per-mailbox and global caps keep it tiny. It is a safety net that
// complements peer history sync, not a message database — a dropped envelope
// (node restart, eviction) still reaches the recipient from any peer that
// holds the message.
package mailbox

import (
	"sync"
	"time"
)

// Limits (deliberately small — this is a friend-group safety net).
const (
	MaxEnvelope   = 64 << 10  // 64 KiB per envelope (wake tokens / small msgs)
	MaxPerMailbox = 200       // envelopes held per recipient
	MaxTotalBytes = 64 << 20  // 64 MiB across all mailboxes, oldest-evicted
	DefaultTTL    = 14 * 24 * time.Hour
	registerTTL   = 30 * 24 * time.Hour // forget mailboxes not seen in a month
)

// Envelope is one sealed message waiting for a recipient.
type Envelope struct {
	ID      string
	Data    []byte
	depositor string // opaque id of who deposited it (for fair eviction)
	stored  time.Time
	expires time.Time
}

// Store holds pending envelopes per mailbox tag. Safe for concurrent use.
type Store struct {
	mu         sync.Mutex
	boxes      map[string][]Envelope // mailboxID (hex) -> envelopes
	registered map[string]time.Time  // mailboxID (hex) -> last seen
	total      int                   // total bytes across all boxes
	seq        uint64
}

// New returns an empty mailbox store.
func New() *Store {
	return &Store{
		boxes:      map[string][]Envelope{},
		registered: map[string]time.Time{},
	}
}

// Register marks a mailbox as belonging to a connected, real user. The node
// only accepts deposits to registered mailboxes, which stops spammers filling
// storage with envelopes for random tags.
func (s *Store) Register(mailboxID string) {
	s.mu.Lock()
	s.registered[mailboxID] = time.Now()
	s.mu.Unlock()
}

// IsRegistered reports whether a mailbox has been registered.
func (s *Store) IsRegistered(mailboxID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.registered[mailboxID]
	return ok
}

// Deposit stores a sealed envelope for a mailbox. Rejects oversized envelopes
// and deposits to unregistered mailboxes. depositor is an opaque id for whoever
// deposited (their PeerID) — used only to keep one flooder from evicting another
// sender's genuine mail. Returns the assigned envelope ID.
func (s *Store) Deposit(mailboxID, depositor string, data []byte, ttl time.Duration) (string, bool) {
	if len(data) == 0 || len(data) > MaxEnvelope {
		return "", false
	}
	if ttl <= 0 || ttl > DefaultTTL {
		ttl = DefaultTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.registered[mailboxID]; !ok {
		return "", false
	}
	s.gcLocked()

	s.seq++
	id := formatID(s.seq)
	now := time.Now()
	env := Envelope{ID: id, Data: append([]byte(nil), data...), depositor: depositor, stored: now, expires: now.Add(ttl)}

	box := s.boxes[mailboxID]
	// Cap per mailbox. A single mailbox tag is derivable by any co-member, so
	// strict oldest-first eviction lets one attacker flush a victim's genuine
	// pending mail simply by depositing MaxPerMailbox junk. Instead evict from the
	// HEAVIEST depositor in the box, so a flooder only ever displaces its own
	// envelopes — an honest sender's one or two messages are never the victim.
	for len(box) >= MaxPerMailbox {
		box = s.dropHeaviestDepositorLocked(box)
	}
	box = append(box, env)
	s.boxes[mailboxID] = box
	s.total += len(data)

	// Global cap: evict oldest across all mailboxes.
	s.evictLocked()
	return id, true
}

// dropHeaviestDepositorLocked removes the oldest envelope belonging to whichever
// depositor holds the most envelopes in the box (ties broken by oldest overall).
// Caller holds s.mu.
func (s *Store) dropHeaviestDepositorLocked(box []Envelope) []Envelope {
	if len(box) == 0 {
		return box
	}
	counts := make(map[string]int, len(box))
	for _, e := range box {
		counts[e.depositor]++
	}
	worst, worstN := "", -1
	for dep, n := range counts {
		if n > worstN {
			worst, worstN = dep, n
		}
	}
	// Drop the oldest envelope from the heaviest depositor (box is append-ordered,
	// so the first match is the oldest).
	for i, e := range box {
		if e.depositor == worst {
			s.total -= len(e.Data)
			return append(box[:i:i], box[i+1:]...)
		}
	}
	// Fallback (shouldn't happen): drop oldest.
	s.total -= len(box[0].Data)
	return box[1:]
}

// Drain returns (without deleting) the non-expired envelopes for a mailbox.
// Deletion happens on Ack, so a recipient that crashes mid-processing gets
// them again next time.
func (s *Store) Drain(mailboxID string) []Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered[mailboxID] = time.Now()
	s.gcLocked()
	box := s.boxes[mailboxID]
	out := make([]Envelope, 0, len(box))
	now := time.Now()
	for _, e := range box {
		if e.expires.After(now) {
			out = append(out, e)
		}
	}
	return out
}

// Ack deletes the given envelopes from a mailbox once the recipient has
// processed them.
func (s *Store) Ack(mailboxID string, ids []string) {
	if len(ids) == 0 {
		return
	}
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	box := s.boxes[mailboxID]
	kept := box[:0]
	for _, e := range box {
		if drop[e.ID] {
			s.total -= len(e.Data)
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(s.boxes, mailboxID)
	} else {
		s.boxes[mailboxID] = kept
	}
}

// GC removes expired envelopes and stale registrations. Call periodically.
func (s *Store) GC() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
}

// Stats reports current occupancy (for logging).
func (s *Store) Stats() (mailboxes, envelopes, bytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.boxes {
		envelopes += len(b)
	}
	return len(s.boxes), envelopes, s.total
}

func (s *Store) gcLocked() {
	now := time.Now()
	for mid, box := range s.boxes {
		kept := box[:0]
		for _, e := range box {
			if e.expires.After(now) {
				kept = append(kept, e)
			} else {
				s.total -= len(e.Data)
			}
		}
		if len(kept) == 0 {
			delete(s.boxes, mid)
		} else {
			s.boxes[mid] = kept
		}
	}
	for mid, seen := range s.registered {
		if now.Sub(seen) > registerTTL {
			delete(s.registered, mid)
		}
	}
}

// evictLocked drops the globally-oldest envelopes until under the byte cap.
func (s *Store) evictLocked() {
	for s.total > MaxTotalBytes {
		var oldestMID string
		var oldest time.Time
		found := false
		for mid, box := range s.boxes {
			if len(box) == 0 {
				continue
			}
			if !found || box[0].stored.Before(oldest) {
				oldest = box[0].stored
				oldestMID = mid
				found = true
			}
		}
		if !found {
			return
		}
		box := s.boxes[oldestMID]
		s.total -= len(box[0].Data)
		if len(box) == 1 {
			delete(s.boxes, oldestMID)
		} else {
			s.boxes[oldestMID] = box[1:]
		}
	}
}

func formatID(n uint64) string {
	const hexdigits = "0123456789abcdef"
	var b [16]byte
	for i := 15; i >= 0; i-- {
		b[i] = hexdigits[n&0xf]
		n >>= 4
	}
	return string(b[:])
}
