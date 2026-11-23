package app

import (
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// A device of ours that is not a guild's committer offers that guild to its
// siblings with a code addressed at whoever is (ownerInviteCode). When it
// knows no address of the committer's it could hand on, the guild is left out
// of the offer. The offer used to go out once per connection between the two
// devices, so a guild left out of it stayed missing on the other device until
// one of them reconnected: a DM started with an account on two devices seated
// one of them and never the other. An offer with something left out is
// therefore made again: on a short backoff, and whenever a committer of a
// guild that was left out connects to us, since that is when an address of
// theirs usually appears. Both are bounded.
//
// Only our own devices are ever offered anything here, and the frame is the
// hello every connection already exchanges, so nothing new reaches anyone.

// offerGapBackoff is when an incomplete offer is made again, counted from the
// offer before.
var offerGapBackoff = []time.Duration{5 * time.Second, 15 * time.Second, 45 * time.Second}

// offerGapMaxTries bounds the offers made again to one device on one
// connection, timed and on a committer connecting together. A complete offer
// resets it.
const offerGapMaxTries = 8

// withholdOwnerCodes makes ownerInviteCode find no address, as a device that
// knows none would, so a test can leave a guild out of an offer. Nothing in
// the product ever sets it.
var withholdOwnerCodes atomic.Bool

// offerGap is the state for one device of ours whose last offer left
// something out.
type offerGap struct {
	owners map[string]bool // committers (account fingerprints) of what was left out
	tries  int
	timer  *time.Timer
}

// noteOfferGaps records what the offer just composed for our device p left
// out, and schedules the next offer while something is.
func (s *Service) noteOfferGaps(p peer.ID, missing []string) {
	owners := map[string]bool{}
	for _, gid := range missing {
		if f := s.effectiveOwner(gid); f != "" {
			owners[f] = true
		}
	}
	s.offerGapMu.Lock()
	defer s.offerGapMu.Unlock()
	gap := s.offerGaps[p]
	if len(missing) == 0 {
		if gap != nil {
			if gap.timer != nil {
				gap.timer.Stop()
			}
			delete(s.offerGaps, p)
		}
		return
	}
	if gap == nil {
		if s.offerGaps == nil {
			s.offerGaps = map[peer.ID]*offerGap{}
		}
		gap = &offerGap{}
		s.offerGaps[p] = gap
	}
	gap.owners = owners
	if gap.timer != nil || gap.tries >= len(offerGapBackoff) || gap.tries >= offerGapMaxTries {
		return
	}
	wait := offerGapBackoff[gap.tries]
	gap.tries++
	// beat: not after Close has begun, and Close waits for one running.
	gap.timer = time.AfterFunc(wait, s.beat(func() {
		s.offerGapMu.Lock()
		if g := s.offerGaps[p]; g == gap {
			g.timer = nil
		}
		s.offerGapMu.Unlock()
		s.reoffer(p)
	}))
}

// reofferOnCommitterConnect offers again to every device of ours whose last
// offer left out a guild that the newly connected peer's account commits in.
//
// It runs on connect and again from peerResolved, and one connection counts
// once: a peer that already resolves to its account at connect (an original
// device, or one whose certificate we hold) never reaches peerResolved, which
// runs only when a hello changes what the peer resolves to; one that does
// not resolves to its own device key at connect, which commits in nothing.
func (s *Service) reofferOnCommitterConnect(q peer.ID) {
	f := s.presence(q).Fingerprint
	if f == "" || f == s.id.Fingerprint() {
		return
	}
	var due []peer.ID
	s.offerGapMu.Lock()
	for p, gap := range s.offerGaps {
		if gap.owners[f] && gap.tries < offerGapMaxTries {
			gap.tries++
			due = append(due, p)
		}
	}
	s.offerGapMu.Unlock()
	for _, p := range due {
		go s.reoffer(p)
	}
}

// reoffer greets our device p again, which composes and sends a fresh offer.
func (s *Service) reoffer(p peer.ID) {
	if s.ctx.Err() != nil {
		return // closing: the store and host are going away under us
	}
	if !s.host.HasConnection(p) || !s.ownDevice(p) {
		s.forgetOfferGap(p)
		return
	}
	s.greeted.release(p)
	s.greet(p)
}

// stopOfferGaps stops every pending re-offer, for Close.
func (s *Service) stopOfferGaps() {
	s.offerGapMu.Lock()
	for p, gap := range s.offerGaps {
		if gap.timer != nil {
			gap.timer.Stop()
		}
		delete(s.offerGaps, p)
	}
	s.offerGapMu.Unlock()
}

// forgetOfferGap drops the state for p; its next connection starts afresh.
func (s *Service) forgetOfferGap(p peer.ID) {
	s.offerGapMu.Lock()
	if gap := s.offerGaps[p]; gap != nil {
		if gap.timer != nil {
			gap.timer.Stop()
		}
		delete(s.offerGaps, p)
	}
	s.offerGapMu.Unlock()
}
