package app

import "testing"

// TestValidArtURL pins the album-art allowlist: known music CDNs pass, and —
// the actual attack — an arbitrary host a peer controls is rejected, so art
// can render by default without leaking viewers' IPs.
func TestValidArtURL(t *testing.T) {
	ok := []string{
		"https://i.scdn.co/image/ab67616d0000b273",
		"https://i.ytimg.com/vi/xyz/hqdefault.jpg",
		"https://lh3.googleusercontent.com/abc",
		"https://is1-ssl.mzstatic.com/image/thumb/Music.jpg",
		"https://f4.bcbits.com/img/a1.jpg",
		"https://coverartarchive.org/release/x/front.jpg",
	}
	for _, u := range ok {
		if !validArtURL(u) {
			t.Errorf("validArtURL(%q) = false, want true", u)
		}
	}
	bad := []string{
		"",
		"http://i.scdn.co/insecure.jpg",        // https only
		"https://evil.example.com/track.jpg",   // arbitrary host = IP harvester
		"https://iscdn.co/spoof.jpg",           // suffix must match on a dot boundary
		"https://scdn.co.evil.net/x.jpg",       // suffix spoof
		"https://i.scdn.co.attacker.io/x.jpg",  // suffix spoof
		"file:///home/user/art.png",            // no local paths
		"javascript:alert(1)",                  // no script URIs
		"https://" + string(make([]byte, 600)), // over maxArtURLBytes
	}
	for _, u := range bad {
		if validArtURL(u) {
			t.Errorf("validArtURL(%q) = true, want false", u)
		}
	}
}
