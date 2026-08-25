package net

import "testing"

func TestMobilePlatformRecognisesPhones(t *testing.T) {
	for _, goos := range []string{"android", "ios"} {
		if !mobilePlatform(goos) {
			t.Errorf("%s is a phone and was not recognised as one", goos)
		}
	}
	for _, goos := range []string{"linux", "darwin", "windows", "freebsd", "js", ""} {
		if mobilePlatform(goos) {
			t.Errorf("%s was treated as a phone", goos)
		}
	}
}

// TestPhonesDoNotRelayForOtherPeers is the data-plan regression.
//
// serveRelay's only guard used to be DirectlyReachable, which asks whether any
// interface we listen on carries a routable address. Every carrier hands a
// phone a global IPv6 address and listenAddrs binds /ip6/::, so that is TRUE on
// cellular — and peerRelayResources allows 8 concurrent circuits with no byte
// or duration cap, so one guild member behind CGNAT could route an entire
// session through the phone owner's data plan, both directions.
func TestPhonesDoNotRelayForOtherPeers(t *testing.T) {
	if !relayServiceWanted(true, false) {
		t.Error("a directly reachable desktop must still relay for guild members")
	}
	if relayServiceWanted(true, true) {
		t.Error("a phone with a routable address relayed for other peers")
	}
	if relayServiceWanted(false, false) || relayServiceWanted(false, true) {
		t.Error("a node with no routable address advertised itself as a relay")
	}
}
