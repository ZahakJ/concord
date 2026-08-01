//go:build !linux || android

package app

// nowPlaying is a no-op on platforms without an MPRIS media-session bridge yet
// (macOS MediaRemote / Windows SMTC are future work). Rich presence simply
// surfaces nothing there.
func nowPlaying() (string, *Activity) { return "", nil }

// richPresenceSupported gates the 8s poll loop: where nowPlaying is a stub
// there is nothing to poll, and on a phone a timer that fires seven times a
// minute forever to learn nothing is pure battery. The setting can stay on;
// the loop just never starts here.
const richPresenceSupported = false
