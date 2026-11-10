package app

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

// A member whose connection predates their membership never had an arrival
// announced; their departure must still reach the interface, or they stay
// green until something unrelated refreshes it.
func TestPeerDownReachesUIForAMemberWhoseArrivalWasSilent(t *testing.T) {
	s, _ := inboxService(t)
	friend := mustID(t)
	if err := s.store.RecordContact("p1", friend.Fingerprint()); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetVerifiedByFingerprint(friend.Fingerprint()); err != nil {
		t.Fatal(err)
	}
	downs := 0
	s.OnPeerDisconnected(func(PeerPresence) { downs++ })

	s.emitPeerDown(peer.ID("p1"), PeerPresence{Fingerprint: friend.Fingerprint()})
	if downs != 1 {
		t.Fatalf("a known member's departure was not announced (%d events)", downs)
	}
	s.emitPeerDown(peer.ID("p2"), PeerPresence{Fingerprint: mustID(t).Fingerprint()})
	if downs != 1 {
		t.Fatal("a stranger's departure must stay silent")
	}
}
