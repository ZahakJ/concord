package net

import "runtime"

// This file is where "what kind of machine is this" gets decided, once, so the
// rest of the package can ask instead of guessing.
//
// Everything else in internal/net was written for a desktop: a machine on mains
// power, on a connection nobody is metering, that can afford to be a service to
// other peers. gomobile builds the same package for a phone, where all three
// assumptions are wrong — the battery is finite, the bytes are billed, and the
// radio's high-power state is the single largest thing an app can leave running.
// A phone that carries other people's circuits and answers other people's
// reachability probes is spending its owner's money on strangers' traffic.

// mobilePlatform reports whether goos is a phone rather than a computer.
//
// A pure function over GOOS rather than a build tag: the answer is needed in
// half a dozen places, a build-tagged file would duplicate every decision that
// depends on it, and a tagged file cannot be tested from a desktop test run at
// all. gomobile compiles this package with GOOS=android or GOOS=ios, which is
// the whole signal.
func mobilePlatform(goos string) bool {
	switch goos {
	case "android", "ios":
		return true
	default:
		return false
	}
}

// onMobile is mobilePlatform's answer for the binary we are running in.
//
// A var rather than a const so a test can force the phone branch and assert
// what the host actually did with it — the alternative is trusting that an
// `if` nobody ever executes on the test machine is written correctly, which is
// exactly how a mobile-only regression survives a green CI run.
var onMobile = mobilePlatform(runtime.GOOS)

// relayServiceWanted reports whether this node should run a circuit-relay v2
// service for guild members.
//
// proven is evidence that the internet can get in — an unsolicited direct
// connection has actually arrived, see inboundProof — and not merely that our
// address parses as routable. It is necessary and, on a phone, not sufficient.
// A phone on Wi-Fi behind a permissive router can be genuinely reachable and is
// still the wrong machine for the job: peerRelayResources allows 32 reservations
// and 8 concurrent circuits with no per-circuit byte or duration limit (nil
// Limit is load-bearing; see the comment there), so one guild member stuck
// behind CGNAT can route an entire evening's session — ingress and egress both —
// through the phone owner's data plan and radio.
//
// The mobile gate sits here and not in the evidence on purpose: what reached the
// phone reached it, and the reachability panel reads that answer directly, so
// "can people reach you" must keep meaning what it says on every platform.
func relayServiceWanted(proven, mobile bool) bool {
	return proven && !mobile
}
