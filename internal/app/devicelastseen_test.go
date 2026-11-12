package app

import (
	"encoding/hex"
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
)

// A device that drops off is stamped as seen at that moment, not left showing
// the time of its last hello (which could be its link time, minutes ago).
func TestStampLastSeenOnDisconnect(t *testing.T) {
	a, _ := identity.Generate()
	b, _ := identity.Generate()
	recs := []ownDevice{
		{Key: hex.EncodeToString(a.DevicePublicKey()), LastSeen: 100},
		{Key: hex.EncodeToString(b.DevicePublicKey()), LastSeen: 100},
	}
	pid, ok := devicePeerID(b.DevicePublicKey())
	if !ok {
		t.Fatal("no peer id")
	}
	if !stampLastSeen(recs, pid, 999) {
		t.Fatal("own device not matched")
	}
	if recs[1].LastSeen != 999 || recs[0].LastSeen != 100 {
		t.Fatalf("stamped wrong record: %+v", recs)
	}
	other, _ := identity.Generate()
	opid, _ := devicePeerID(other.DevicePublicKey())
	if stampLastSeen(recs, opid, 5) {
		t.Fatal("a stranger matched an own device")
	}
}
