// Package version holds the release tag stamped into the binary at build time
// via:
//
//	-ldflags "-X github.com/zahak/concord/internal/version.Version=vX.Y.Z"
//
// (see the Makefile `release` target and the CI native build). Unstamped local
// and dev builds report "dev"; the update check treats that as "never nag".
// It lives in its own package so every shell — desktop mains, the gomobile
// core, the rendezvous node — can share one stamp point.
package version

// Version is the release tag, or "dev" when unstamped.
var Version = "dev"
