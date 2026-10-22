package net

import (
	"fmt"
	"strconv"
	"time"

	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
)

// RendezvousRelayResourcesFromEnv is RendezvousRelayResources with optional
// overrides from the environment, for operators who run this node for
// something other than Concord. With none of the variables set it returns
// exactly RendezvousRelayResources — Concord's own deployment is unchanged.
//
//	CONCORD_RELAY_CIRCUIT_BYTES       per-circuit byte cap, each direction
//	CONCORD_RELAY_CIRCUIT_DURATION    per-circuit time cap (Go duration, e.g. 2m)
//	CONCORD_RELAY_MAX_CIRCUITS        concurrent circuits per peer
//	CONCORD_RELAY_MAX_RESERVATIONS    reservations in total
//	CONCORD_RELAY_MAX_RESERVATIONS_PER_PEER
//	CONCORD_RELAY_MAX_RESERVATIONS_PER_IP
//
// Setting either circuit cap brings back the per-circuit Limit, and with it
// the "limited" connections that RendezvousRelayResources explains gossipsub
// refuses to use. That is the point for a node that only relays signaling
// (ferry's rendezvous: a few kilobytes to set up a direct connection, never
// bulk data), and exactly wrong for a node Concord peers depend on to
// publish. When only one cap is set, the other takes the library default.
//
// A value that does not parse, or is not positive, is an error: a node that
// silently fell back to unlimited circuits would be the failure this exists
// to prevent.
func RendezvousRelayResourcesFromEnv(getenv func(string) string) (relayv2.Resources, error) {
	r := RendezvousRelayResources()

	num := func(name string, set func(int)) error {
		v := getenv(name)
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("%s must be a positive whole number, got %q", name, v)
		}
		set(n)
		return nil
	}

	bytes, dur := getenv("CONCORD_RELAY_CIRCUIT_BYTES"), getenv("CONCORD_RELAY_CIRCUIT_DURATION")
	if bytes != "" || dur != "" {
		l := relayv2.DefaultLimit()
		if bytes != "" {
			n, err := strconv.ParseInt(bytes, 10, 64)
			if err != nil || n <= 0 {
				return r, fmt.Errorf("CONCORD_RELAY_CIRCUIT_BYTES must be a positive number of bytes, got %q", bytes)
			}
			l.Data = n
		}
		if dur != "" {
			d, err := time.ParseDuration(dur)
			if err != nil || d <= 0 {
				return r, fmt.Errorf("CONCORD_RELAY_CIRCUIT_DURATION must be a positive duration such as 2m, got %q", dur)
			}
			l.Duration = d
		}
		r.Limit = l
	}

	for _, f := range []struct {
		name string
		set  func(int)
	}{
		{"CONCORD_RELAY_MAX_CIRCUITS", func(n int) { r.MaxCircuits = n }},
		{"CONCORD_RELAY_MAX_RESERVATIONS", func(n int) { r.MaxReservations = n }},
		{"CONCORD_RELAY_MAX_RESERVATIONS_PER_PEER", func(n int) { r.MaxReservationsPerPeer = n }},
		{"CONCORD_RELAY_MAX_RESERVATIONS_PER_IP", func(n int) { r.MaxReservationsPerIP = n }},
	} {
		if err := num(f.name, f.set); err != nil {
			return r, err
		}
	}
	return r, nil
}
