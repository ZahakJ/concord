package app

import (
	"crypto/rand"
	"encoding/hex"

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

// notePeerSession records the Session a hello from p carried, and when it
// differs from the one before, releases what this service holds for p's old
// run. Only for devices of our own account: the claims are also what keeps a
// stranger from making us compose a full answer on every stream it opens, and
// a stranger could name a new session each time.
func (s *Service) notePeerSession(p peer.ID, session string) {
	if session == "" || !s.ownDevice(p) {
		return
	}
	s.peerSessionsMu.Lock()
	prev := s.peerSessions[p]
	if s.peerSessions == nil {
		s.peerSessions = map[peer.ID]string{}
	}
	s.peerSessions[p] = session
	s.peerSessionsMu.Unlock()
	if prev == "" || prev == session {
		return
	}
	s.resetPeerSession(p)
}

// resetPeerSession is what a disconnect would have released for p.
func (s *Service) resetPeerSession(p peer.ID) {
	s.greeted.release(p)
	s.answered.release(p)
	s.solicited.release(p)
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
