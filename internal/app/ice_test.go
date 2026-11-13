package app

import (
	"encoding/json"
	"testing"
)

// TestDefaultIceNamesNoServer: this layer reports what the rendezvous offers
// and nothing else. A public STUN server is added, or not, by the frontend,
// where the user's "No third-party call servers" switch lives; a server named
// here would get past that switch. The frontend reads iceServers as a list, so
// it must stay one in JSON.
func TestDefaultIceNamesNoServer(t *testing.T) {
	if n := len(defaultIce.IceServers); n != 0 {
		t.Fatalf("the fallback ICE configuration names %d server(s): %+v", n, defaultIce.IceServers)
	}
	if defaultIce.RelayAvailable {
		t.Fatal("the fallback claims a relay it does not have")
	}
	b, err := json.Marshal(defaultIce)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"iceServers":[],"relayAvailable":false}` {
		t.Fatalf("fallback marshals as %s", b)
	}
}
