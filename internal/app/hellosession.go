package app

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// A device of ours that stops and starts again under the same peer ID can
// redial us before we notice its old connection die. The host then never
// reports a disconnect or a fresh connect (the peer was connected throughout,
// as far as it can tell), so nothing released the per-connection claims: our
// answer to the restarted device's hello was the short one, without the
// guilds it is missing, and we never greeted it again. A DM formed while it
// was away then never reached it (TestDMReachesASecondDeviceThatComesOnlineLater,
// intermittently). Each run of the service names itself in its hellos, and a
// new name from an own device is treated as the reconnect it is.

// newHelloSession mints this run's hello Session.
func newHelloSession() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// peerSessionResetEvery bounds how often a change of Session releases what we
// hold for one device: a device of ours that names a new Session in every
// hello must not make us compose a full answer to each of them.
const peerSessionResetEvery = 30 * time.Second

// peerSession is what the hellos of one device of ours have told us: the
// Session they last carried (empty for a build that sends none) and when a
// change of it last released anything.
type peerSession struct {
	session string
	resetAt time.Time
}

// notePeerSession records the Session a hello from p carried, and when it
// differs from the one before, releases what this service holds for p's old
// run. Only for devices of our own account: the claims are also what keeps a
// stranger from making us compose a full answer on every stream it opens, and
// a stranger could name a new session each time.
//
// A hello with no Session is recorded too, as seen: it is an older build, and
// its first hello after an upgrade names a Session for the first time, which
// is the restart that needs the full answer. A device we had no entry for is
// on a connection that started afresh, so its first Session changes nothing.
//
// asked is a hello p sent unasked (handleHello). An answer to ours (asked
// false) releases only the claims on what we answer and solicit: our hello
// just reached p's new run, so greeting it again would only repeat itself.
func (s *Service) notePeerSession(p peer.ID, session string, asked bool) {
	if !s.ownDevice(p) {
		return
	}
	now := time.Now()
	s.peerSessionsMu.Lock()
	prev, seen := s.peerSessions[p]
	if s.peerSessions == nil {
		s.peerSessions = map[peer.ID]peerSession{}
	}
	changed := seen && prev.session != session
	reset := changed && now.Sub(prev.resetAt) >= peerSessionResetEvery
	next := peerSession{session: session, resetAt: prev.resetAt}
	if reset {
		next.resetAt = now
	}
	s.peerSessions[p] = next
	s.peerSessionsMu.Unlock()
	if reset {
		s.resetPeerSession(p, asked)
	}
}

// resetPeerSession is what a disconnect would have released for p; with full
// false, only the claims on answering and soliciting it.
func (s *Service) resetPeerSession(p peer.ID, full bool) {
	s.answered.release(p)
	s.solicited.release(p)
	if !full {
		return
	}
	s.greeted.release(p)
	s.forgetOfferGap(p)
	s.ownProfile.forget(p)
}

// forgetPeerSession drops p's recorded Session when it disconnects; its next
// connection starts afresh anyway.
func (s *Service) forgetPeerSession(p peer.ID) {
	s.peerSessionsMu.Lock()
	delete(s.peerSessions, p)
	s.peerSessionsMu.Unlock()
}
