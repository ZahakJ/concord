package app

// Client side of IP-private calls (see cmd/rendezvous/turn.go for the relay).
//
// A member's app fetches ICE configuration — a STUN server, plus a TURN relay
// with fresh short-lived credentials — from the rendezvous it already bootstraps
// through. The frontend adds these to its RTCPeerConnection and, when the user
// wants IP privacy (or when a browser guest is in the call), forces
// `iceTransportPolicy: "relay"` so its real address never leaves the machine.
//
// Fetching over HTTP leaks nothing new: the app is already connected to this
// same rendezvous over libp2p, so it already knows our IP. The win is that our
// CALL PEERS stop learning it.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// IceServer is the RTCIceServer shape the browser expects.
type IceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// IceConfig is what the frontend needs to open an (optionally relayed) call.
type IceConfig struct {
	IceServers []IceServer `json:"iceServers"`
	// RelayAvailable is true when a TURN relay is configured, i.e. when hiding
	// your IP is actually possible. The UI greys out "hide my IP" without it.
	RelayAvailable bool `json:"relayAvailable"`
}

// defaultIce is the fallback when no relay is configured: a public STUN server,
// exactly the behaviour before TURN existed. Calls still work; they just can't
// hide IPs.
var defaultIce = IceConfig{
	IceServers:     []IceServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	RelayAvailable: false,
}

// CallIceServers returns ICE configuration for a call, fetching fresh TURN
// credentials from the rendezvous gateway when one is deployed. On any failure
// it degrades to plain STUN — a call is never blocked by the relay being
// absent or unreachable.
func (s *Service) CallIceServers() IceConfig {
	base := s.guestGatewayBase()
	if base == "" {
		return defaultIce
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/turn", nil)
	if err != nil {
		return defaultIce
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return defaultIce // no relay on this deployment, or it's down
	}
	defer resp.Body.Close()
	var out struct {
		IceServers []IceServer `json:"iceServers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.IceServers) == 0 {
		return defaultIce
	}
	// A relay is present only if at least one server is a turn: URL.
	relay := false
	for _, srv := range out.IceServers {
		for _, u := range srv.URLs {
			if len(u) >= 5 && u[:5] == "turn:" {
				relay = true
			}
		}
	}
	return IceConfig{IceServers: out.IceServers, RelayAvailable: relay}
}
