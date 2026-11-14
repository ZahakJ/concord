// Package version holds the release tag stamped into the binary at build time
// via:
//
//	-ldflags "-X github.com/ZahakJ/concord/internal/version.Version=vX.Y.Z"
//
// (see the Makefile `release` target and the CI native build). Unstamped local
// and dev builds report "dev"; the update check treats that as "never nag".
// It lives in its own package so every shell — desktop mains, the gomobile
// core, the rendezvous node — can share one stamp point.
package version

import "strings"

// Version is the release tag, or "dev" when unstamped.
var Version = "dev"

// Rendezvous is the rendezvous list built in to this copy of Concord, comma
// separated, stamped the same way as Version:
//
//	-ldflags "-X github.com/ZahakJ/concord/internal/version.Rendezvous=/dns4/rdv.example.com/tcp/4001/p2p/12D3KooW…"
//
// (`make … RENDEZVOUS=…` does it for every target). Empty in the source, so a
// plain clone builds a Concord that names no node; whoever builds and hands out
// a copy decides which node it starts from. It lives here rather than in the
// app so that --version and --help can say it without starting anything.
var Rendezvous = ""

// RendezvousList is Rendezvous as a list of addresses.
func RendezvousList() []string {
	var out []string
	for _, a := range strings.Split(Rendezvous, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// RendezvousHosts names the built-in nodes for people: the host part of each
// address (/dns4/HOST/… or /ip4/HOST/…), for --version, --help and About.
func RendezvousHosts() []string {
	var out []string
	for _, a := range RendezvousList() {
		parts := strings.Split(strings.TrimPrefix(a, "/"), "/")
		if len(parts) >= 2 {
			out = append(out, parts[1])
		} else {
			out = append(out, a)
		}
	}
	return out
}
