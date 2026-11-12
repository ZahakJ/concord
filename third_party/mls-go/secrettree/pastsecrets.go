package secrettree

import (
	"crypto/sha256"
	"sort"
)

// Concord addition (see THIRD-PARTY-NOTICES.md).
//
// PastSecrets is the out-of-order window of one epoch's secret tree: the
// ratchet secrets of generations a sender's ratchet has already moved past
// but whose messages have NOT arrived yet.
//
// It exists for a Client that reloads group state from storage on every
// operation (CacheNone, which Concord uses). The stored state leaves past
// generations out, so every reload started with an empty window, and a
// message that arrived after a later one from the same sender could never be
// decrypted ("generation N already advanced past"). Gossip reorders, and a
// sender with several sends in flight publishes out of order. The window is
// handed from one load to the next in memory only, never written to disk.
//
// What is kept, and for how long: at most PastWindow generations per sender
// leaf, the most recent ones, only those not yet received (a generation's
// secret is zeroed when the load that decrypted its message ends), only
// until the epoch changes.
type PastSecrets struct {
	epochKey [32]byte
	leaves   map[uint32]map[uint32]*cachedGenSecret
	// used is, per leaf, the recent generations already decrypted. Carried so
	// a generation consumed in one load is not cached as "missing" by the
	// next one when its ratchet moves past it, and so a replay of it fails.
	used map[uint32]map[uint32]struct{}
}

// PastWindow bounds the carried window per sender leaf. It is the library's
// own cap on past generations (maxCachedGenerations), and it has to be about
// that deep: a receiver working through a backlog of a sender's live messages
// can meanwhile decrypt a history-sync response from the same sender, which
// that sender encrypted later, and the backlog behind it is the window needed.
// In a 40-message-a-second burst that backlog was close to 900 generations.
const PastWindow = 2048

func (t *Tree) epochKey() [32]byte {
	return sha256.Sum256(t.encryptionSecret.AsSlice())
}

// TakePastSecrets moves the tree's out-of-order window out of it, keeping only
// the PastWindow most recent generations per leaf. With dropConsumed, the
// generations whose application message this tree decrypted are dropped (and
// zeroed) too; a caller whose operation failed part-way passes false, so a
// generation is not burnt by a message that was then rejected. Everything not
// carried is zeroed. The caller is expected to discard the tree.
func (t *Tree) TakePastSecrets(dropConsumed bool) *PastSecrets {
	if t == nil || t.encryptionSecret == nil {
		return nil
	}
	p := &PastSecrets{epochKey: t.epochKey(), leaves: map[uint32]map[uint32]*cachedGenSecret{},
		used: map[uint32]map[uint32]struct{}{}}
	for idx, ls := range t.leafCache {
		if ls == nil {
			continue
		}
		var floor uint64
		if ls.generation > PastWindow {
			floor = ls.generation - PastWindow
		}
		if dropConsumed && len(ls.usedApplicationGenerations) > 0 {
			used := map[uint32]struct{}{}
			for g := range ls.usedApplicationGenerations {
				if uint64(g) >= floor {
					used[g] = struct{}{}
				}
			}
			p.used[idx] = used
		}
		var gens []uint32
		for g, c := range ls.secretCache {
			if _, used := ls.usedApplicationGenerations[g]; dropConsumed && used {
				c.zero()
				continue
			}
			gens = append(gens, g)
		}
		sort.Slice(gens, func(i, j int) bool { return gens[i] > gens[j] })
		keep := map[uint32]*cachedGenSecret{}
		for i, g := range gens {
			if i < PastWindow {
				keep[g] = ls.secretCache[g]
			} else {
				ls.secretCache[g].zero()
			}
		}
		ls.secretCache = nil
		if len(keep) > 0 {
			p.leaves[idx] = keep
		}
	}
	return p
}

// KeepReplaysOf sets p's record of consumed generations to q's, for a window
// taken after an operation that failed part-way: what it marked consumed
// must not stay marked.
func (p *PastSecrets) KeepReplaysOf(q *PastSecrets) *PastSecrets {
	if p == nil {
		return nil
	}
	p.used = nil
	if q != nil && q.epochKey == p.epochKey {
		p.used = q.used
	}
	return p
}

// AdoptPastSecrets moves a window taken from an earlier load of the SAME epoch
// into the tree. A window from another epoch cannot open anything this tree is
// asked about; it is zeroed and dropped.
func (t *Tree) AdoptPastSecrets(p *PastSecrets) {
	if t == nil || p == nil {
		return
	}
	if t.encryptionSecret == nil || p.epochKey != t.epochKey() {
		p.zero()
		return
	}
	for idx, gens := range p.leaves {
		ls, err := t.LeafForIndex(idx)
		if err != nil {
			continue
		}
		for g, c := range gens {
			if uint64(g) >= ls.generation {
				c.zero() // the reloaded ratchet derives these itself
				continue
			}
			if ls.secretCache == nil {
				ls.secretCache = make(map[uint32]*cachedGenSecret)
			}
			if _, ok := ls.secretCache[g]; ok {
				c.zero()
				continue
			}
			ls.secretCache[g] = c
		}
	}
	// The consumed generations are copied, not moved: a failed operation
	// hands the same record back (KeepReplaysOf).
	for idx, gens := range p.used {
		ls, err := t.LeafForIndex(idx)
		if err != nil {
			continue
		}
		if ls.usedApplicationGenerations == nil {
			ls.usedApplicationGenerations = make(map[uint32]struct{}, len(gens))
		}
		for g := range gens {
			ls.usedApplicationGenerations[g] = struct{}{}
		}
	}
	p.leaves = nil
}

func (p *PastSecrets) zero() {
	for _, gens := range p.leaves {
		for _, c := range gens {
			c.zero()
		}
	}
	p.leaves = nil
}

func (c *cachedGenSecret) zero() {
	if c.application != nil {
		c.application.SecureZero()
	}
	if c.handshake != nil {
		c.handshake.SecureZero()
	}
}
