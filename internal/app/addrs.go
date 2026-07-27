package app

import (
	"strings"

	"github.com/multiformats/go-multiaddr"
)

// codeAddrs assembles the addresses an invite or link code should carry: our
// own dialable addresses, plus one relay path per relay we currently hold a
// reservation with.
//
// Circuits used to be string-built from the configured rendezvous list, one
// per entry, unconditionally — so a rendezvous that had been dead for months
// still shipped a confident "reach me here" in every code, and the joiner
// spent its dial budget on it. libp2p only puts a /p2p-circuit address in
// h.Addrs() once the reservation is actually held, so that list is the
// truthful answer to "which relays will carry traffic for me right now".
//
// It is not quite the answer we want to advertise, though: libp2p renders a
// reservation as "<the relay's IP addr>/p2p/<relay>/p2p-circuit", a snapshot of
// where that relay answered this session, whereas the same relay is in our
// config in /dns form — which survives the host moving (a fly.io deploy re-IPs
// it) and which the encoder elides for free, since the code already carries it
// as a bootstrap entry. So for a relay we were configured with we advertise the
// configured form; for one we weren't (a friend relaying for us) libp2p's
// rendering is all there is.
func codeAddrs(addrs []multiaddr.Multiaddr, bootstrap []string) []string {
	// Relay peer ID -> the configured entry naming it.
	configured := map[string]string{}
	for _, b := range bootstrap {
		if b = strings.TrimSpace(b); b == "" {
			continue
		}
		if id := relayID(b); id != "" {
			configured[id] = b
		}
	}

	out := make([]string, 0, len(addrs)+len(bootstrap))
	for _, a := range addrs {
		s := a.String()
		if _, err := a.ValueForProtocol(multiaddr.P_CIRCUIT); err != nil {
			out = append(out, s)
			continue
		}
		if id := relayID(s); configured[id] != "" {
			// Drop libp2p's session-snapshot form; the configured /dns entry for
			// this same relay is appended below and outlives a redeploy.
			continue
		}
		out = append(out, s)
	}
	// In configured order, so the code's circuits line up with the rendezvous
	// list the joiner adopts alongside them.
	//
	// A configured rendezvous goes in whether or not we hold a reservation yet.
	// Emitting only confirmed ones reads as the honest thing to do and is worse:
	// codes are minted by hand, often seconds after launch and before AutoRelay
	// has finished, and a code is a permanent artifact — pasted into a chat,
	// screenshotted, redeemed next week. One with no circuit at all is dead
	// forever for anyone off the LAN, while a circuit that is merely not ready
	// yet costs a joiner one failed dial and works on the retry. The joiner
	// adopts these same entries as bootstrap, so this asks nothing of them that
	// the code was not already telling them to trust.
	for _, b := range bootstrap {
		if id := relayID(strings.TrimSpace(b)); id != "" && configured[id] != "" {
			out = append(out, configured[id]+"/p2p-circuit")
		}
	}
	return out
}

// relayID is the peer ID a multiaddr names — for a circuit address that is the
// relay's, since its own /p2p component comes before /p2p-circuit.
func relayID(addr string) string {
	ma, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return ""
	}
	id, err := ma.ValueForProtocol(multiaddr.P_P2P)
	if err != nil {
		return ""
	}
	return id
}
