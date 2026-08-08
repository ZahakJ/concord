package app

import (
	"context"
	"testing"
	"time"

	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
)

// TestRememberPeerWaitsForTheAccountToResolve pins the settle-wait. A friend's
// linked device dials us as its DEVICE key; until we have seen a certificate for
// that key, presence() answers with the device's own fingerprint, which matches
// no member and no contact. rememberPeer used to judge once, at connect time,
// and nothing ever re-ran it — so losing that race (the host starts dialling
// remembered peers before the guilds are tracked, and it is tracking a guild
// that teaches us the mapping) left a friend a stranger for the whole session:
// no contact row, no relay protection, nothing in the peer cache.
func TestRememberPeerWaitsForTheAccountToResolve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startService(t, ctx)

	// A friend's account, and one of its devices dialling us.
	friend, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate friend: %v", err)
	}
	deviceKey, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate device: %v", err)
	}
	devicePub := deviceKey.PublicKey()
	pub, err := p2pcrypto.UnmarshalEd25519PublicKey(devicePub)
	if err != nil {
		t.Fatalf("unmarshal device key: %v", err)
	}
	devicePeer, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatalf("peer id: %v", err)
	}
	// We have verified the friend's ACCOUNT, so once the device resolves to it
	// there is no question this is someone we know.
	if err := svc.VerifyFingerprint(friend.Fingerprint()); err != nil {
		t.Fatalf("VerifyFingerprint: %v", err)
	}

	// The connection callback fires with the unresolved answer.
	unresolved := identity.FingerprintOf(devicePub)
	go svc.rememberPeer(devicePeer, unresolved)

	// …and the certificate arrives a moment later, the way trackGuild →
	// relearnDevices delivers it just after startup.
	time.Sleep(750 * time.Millisecond)
	svc.learnDeviceCert(friend.IssueDeviceCertFor(devicePub, "phone", time.Now().Unix()).Marshal())

	waitUntil(t, 10*time.Second, func() bool {
		contacts, err := svc.Contacts()
		if err != nil {
			return false
		}
		for _, c := range contacts {
			if c.PeerID == devicePeer.String() && c.Fingerprint == friend.Fingerprint() {
				return true
			}
		}
		return false
	}, "a friend's device stayed a stranger because its account resolved a moment too late")
}
