package app

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
)

// livegap.go fetches what live delivery dropped, seconds after it dropped it.
//
// A member who is online is meant to hold every message within moments. Under
// a burst they did not: gossipsub hands each topic's messages to a reader that
// decrypts and stores them one at a time, and whatever arrives while that
// reader's buffer is full is dropped, silently. At 25 messages a second an
// online member was missing a quarter of the channel, and nothing noticed
// until the anti-entropy pass a minute or two later; their screen had no hint
// that anything was missing. The buffer is deeper now (net.subBuffer), and a
// drop that still happens is a gap we KNOW about: the guild is caught up from
// the peer that forwarded the dropped message, a couple of seconds later,
// once per burst rather than once per message.

// liveGapDelay is how long a drop waits before its catch-up runs: long enough
// for the rest of a burst to arrive (one catch-up then covers all of it), short
// enough that the gap is never on screen for long.
const liveGapDelay = 2 * time.Second

// noteLiveGap is net.PubSub's overflow callback. It runs on gossipsub's event
// loop, so it only records the gap and arms one timer.
func (s *Service) noteLiveGap(topic string, via peer.ID) {
	s.liveGapMu.Lock()
	defer s.liveGapMu.Unlock()
	if s.liveGaps == nil {
		s.liveGaps = map[string]peer.ID{}
	}
	first := len(s.liveGaps) == 0
	s.liveGaps[topic] = via
	if first {
		time.AfterFunc(liveGapDelay, s.fillLiveGaps)
	}
}

// fillLiveGaps catches up every guild a dropped message belonged to.
func (s *Service) fillLiveGaps() {
	s.liveGapMu.Lock()
	gaps := s.liveGaps
	s.liveGaps = nil
	s.liveGapMu.Unlock()
	if s.ctx.Err() != nil || len(gaps) == 0 {
		return
	}
	// Topic names are hashes; map them back through the guilds we hold.
	type ask struct {
		guildID string
		via     peer.ID
	}
	var asks []ask
	seen := map[string]bool{}
	s.mu.RLock()
	for _, g := range s.guilds {
		for _, c := range g.Channels {
			via, ok := gaps[domain.TopicID(g.GroupID, c.ID)]
			if ok && !seen[g.ID] {
				seen[g.ID] = true
				asks = append(asks, ask{g.ID, via})
			}
		}
	}
	s.mu.RUnlock()
	for _, a := range asks {
		go func(a ask) {
			if s.syncGuildFromPeer(a.guildID, a.via) != nil {
				s.syncGuildFromAnyPeer(a.guildID)
			}
		}(a)
	}
}
