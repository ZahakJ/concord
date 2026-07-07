package main

// version is the release tag stamped into the binary at build time via:
//
//	-ldflags "-X main.version=vX.Y.Z"
//
// (see the Makefile `release` target and the CI native build). Unstamped local
// and dev builds report "dev"; the update check treats that as "never nag".
var version = "dev"
