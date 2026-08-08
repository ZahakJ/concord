package app

import (
	"context"
	"testing"
)

// The whole point of the diagnostic is that it reports what the node observed
// rather than what the settings imply, so this pins the fields a wrong guess
// would flip: no rendezvous configured must not read as one that answers, and
// mDNS that never started must not read as working LAN discovery.
func TestReachabilityReportsMeasuredState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	if err := SaveListenPort(dir, 41999); err != nil {
		t.Fatalf("SaveListenPort: %v", err)
	}
	svc, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatalf("Start service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	r := svc.Reachability()
	if r.HasRendezvous || r.RendezvousReached {
		t.Errorf("no rendezvous configured, got HasRendezvous=%v RendezvousReached=%v", r.HasRendezvous, r.RendezvousReached)
	}
	if r.LANDiscovery {
		t.Error("mDNS was disabled, but LANDiscovery is true")
	}
	// The pinned port is reported as asked for even when the bind lost the race;
	// PinnedPortTaken is the field that says which happened.
	if r.PinnedPort != 41999 {
		t.Errorf("PinnedPort = %d, want 41999", r.PinnedPort)
	}
	if r.PublicDHT {
		t.Error("public DHT is opt-in and was never opted into")
	}
	// Reachable is exactly "some public family is present". The values themselves
	// depend on the machine running the test — a CI box behind NAT reports
	// neither, a developer's laptop on an IPv6-enabled ISP reports v6 — so the
	// invariant is what gets pinned, not the answer.
	if r.Reachable != (r.PublicIPv4 || r.PublicIPv6) {
		t.Errorf("Reachable=%v but PublicIPv4=%v PublicIPv6=%v; the verdict and its reason disagree",
			r.Reachable, r.PublicIPv4, r.PublicIPv6)
	}
	// The UI downgrades its claim when only IPv6 is present, so a v6-only node
	// must never be indistinguishable from a v4 one.
	if r.PublicIPv6 && !r.PublicIPv4 {
		t.Log("this host is IPv6-only-public: the panel must say so rather than claim plain reachability")
	}
}
