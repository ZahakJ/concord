//go:build !linux

package app

// nowPlaying is a no-op on platforms without an MPRIS media-session bridge yet
// (macOS MediaRemote / Windows SMTC are future work). Rich presence simply
// surfaces nothing there.
func nowPlaying() string { return "" }
