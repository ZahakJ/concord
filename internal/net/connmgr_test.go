package net

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
)

func TestMobileConnLimitsAreSmallerThanDesktop(t *testing.T) {
	phone, desk := peerConnLimits(true), peerConnLimits(false)
	if phone.high >= desk.high || phone.low >= desk.low {
		t.Fatalf("phone marks %+v are not below desktop's %+v", phone, desk)
	}
	if phone.low >= phone.high {
		t.Fatalf("low water mark must sit below high: %+v", phone)
	}
	// Gossipsub wants D=6 mesh peers per topic and Concord opens one per
	// channel, all drawn from the same guild-member pool. A phone in several
	// guilds must not be squeezed under that before a single stranger has been
	// trimmed.
	if phone.high < 32 {
		t.Fatalf("high water %d leaves no room for a multi-guild gossipsub mesh", phone.high)
	}
	if desk.low != 160 || desk.high != 192 {
		t.Fatalf("desktop marks changed from the stated policy: %+v", desk)
	}
	if phone.grace <= 0 {
		t.Fatal("a zero grace period trims a guild member before the hello that protects them")
	}
}

// TestLoadBearingPeersAreExemptFromTrimming walks the set a low water mark must
// never be allowed to shed. Each one costs something specific if it goes: the
// rendezvous carries the mailbox (offline delivery), a linked device is the
// other half of the user's own account, and a guild member is the mesh.
func TestLoadBearingPeersAreExemptFromTrimming(t *testing.T) {
	rendezvous := peer.ID("rendezvous-node")
	device := peer.ID("my-other-phone")
	member := peer.ID("guild-member")

	id, _ := identity.Generate()
	h, err := New(context.Background(), Config{
		Identity:       id,
		BootstrapPeers: []peer.AddrInfo{{ID: rendezvous}},
	})
	if err != nil {
		t.Fatalf("New host: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	cm := h.Libp2p().ConnManager()

	h.ProtectDevice(device)
	h.Protect(member)

	for _, p := range []peer.ID{rendezvous, device, member} {
		if !cm.IsProtected(p, "") {
			t.Errorf("%s is trimmable", p)
		}
	}

	// Only guild membership may open a circuit on us. Keeping a rendezvous or
	// our own device connected must not quietly hand it the relay ACL, which is
	// what reusing relayTag for everything would have done.
	if cm.IsProtected(rendezvous, relayTag) || cm.IsProtected(device, relayTag) {
		t.Error("a trim exemption granted relay rights it should not have")
	}
	if !cm.IsProtected(member, relayTag) {
		t.Error("a guild member lost their relay reservation rights")
	}

	// A rendezvous the user replaces stops being exempt; the new one becomes so.
	replacement := peer.ID("new-rendezvous")
	h.SetBootstrapPeers([]peer.AddrInfo{{ID: replacement}})
	if cm.IsProtected(rendezvous, "") {
		t.Error("a replaced rendezvous kept its exemption for the life of the process")
	}
	if !cm.IsProtected(replacement, "") {
		t.Error("the newly configured rendezvous is trimmable")
	}

	h.UnprotectDevice(device)
	if cm.IsProtected(device, "") {
		t.Error("an unlinked device kept its exemption")
	}
}

// TestConnManagerIsExplicit guards against the option quietly disappearing: a
// host with no ConnectionManager option gets go-libp2p's NullConnMgr only in
// tests, but in production it gets the 160/192 default, which is how this went
// unnoticed on phones for so long.
func TestConnManagerIsExplicit(t *testing.T) {
	id, _ := identity.Generate()
	h := newTestHost(t, id)
	cm := h.Libp2p().ConnManager()
	p := peer.ID("somebody")
	cm.Protect(p, "probe")
	if !cm.IsProtected(p, "probe") {
		t.Fatal("the host has no real connection manager — protection is a no-op")
	}
}
