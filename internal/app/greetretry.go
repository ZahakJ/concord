package app

import (
	"errors"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// A hello to a device of ours is how that device learns of a guild this one
// just created or joined (regreetOwnDevices), and the hello can fail: the
// connection resets under it, the other side is too busy to answer within
// helloTimeout, or the peer is between two connections. greet used to release
// its claim on a failure and leave the retry to the next connection, and a
// connection that stayed up meant the guild, setting or typing choice it
// carried stayed missing on the other device. A failed hello to a device of
// ours is therefore made again on a short backoff while the device stays
// connected. Bounded per run of failures; a hello that gets through, a
// disconnect, or Close ends it.
//
// Only our own devices are retried: a stranger's hello carries nothing that a
// later connection cannot, and retrying every peer the host dials would turn a
// dead relay candidate into steady traffic.

// greetRetryBackoff is when a failed hello to a device of ours is made again,
// counted from the failure before. Its length bounds the retries in one run.
var greetRetryBackoff = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}

var errHelloDropped = errors.New("hello dropped (test)")

// greetRetry is the state for one device of ours whose last hello failed.
type greetRetry struct {
	tries int
	timer *time.Timer
}

// noteGreetResult records how a hello to p went: success ends a run of
// retries, a failure schedules the next one while the backoff lasts.
func (s *Service) noteGreetResult(p peer.ID, err error) {
	if err == nil {
		s.forgetGreetRetry(p)
		return
	}
	if s.ctx.Err() != nil || !s.ownDevice(p) {
		return
	}
	s.greetRetryMu.Lock()
	defer s.greetRetryMu.Unlock()
	r := s.greetRetries[p]
	if r == nil {
		if s.greetRetries == nil {
			s.greetRetries = map[peer.ID]*greetRetry{}
		}
		r = &greetRetry{}
		s.greetRetries[p] = r
	}
	if r.timer != nil || r.tries >= len(greetRetryBackoff) {
		return
	}
	wait := greetRetryBackoff[r.tries]
	r.tries++
	r.timer = time.AfterFunc(wait, func() {
		s.greetRetryMu.Lock()
		if s.greetRetries[p] == r {
			r.timer = nil
		}
		s.greetRetryMu.Unlock()
		s.regreet(p)
	})
}

// regreet makes a failed hello to our device p again, if p is still a
// connected device of ours.
func (s *Service) regreet(p peer.ID) {
	if s.ctx.Err() != nil {
		return
	}
	if !s.host.HasConnection(p) || !s.ownDevice(p) {
		s.forgetGreetRetry(p)
		return
	}
	s.greeted.release(p)
	s.greet(p)
}

// forgetGreetRetry drops the retry state for p.
func (s *Service) forgetGreetRetry(p peer.ID) {
	s.greetRetryMu.Lock()
	if r := s.greetRetries[p]; r != nil {
		if r.timer != nil {
			r.timer.Stop()
		}
		delete(s.greetRetries, p)
	}
	s.greetRetryMu.Unlock()
}

// stopGreetRetries stops every pending retry, for Close.
func (s *Service) stopGreetRetries() {
	s.greetRetryMu.Lock()
	for p, r := range s.greetRetries {
		if r.timer != nil {
			r.timer.Stop()
		}
		delete(s.greetRetries, p)
	}
	s.greetRetryMu.Unlock()
}
