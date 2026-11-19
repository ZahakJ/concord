//go:build !wails && !linux

package main

import "net"

// Elsewhere the launch socket relies on its directory and mode alone (no
// SO_PEERCRED; see main_web_peercred_linux.go).
func launchPeerListener(ln net.Listener, _ int) net.Listener { return ln }
