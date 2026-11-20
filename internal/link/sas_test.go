package link

import (
	"bytes"
	"regexp"
	"testing"
)

// The short authentication string is six digits in two groups, the same for
// the same inputs on both devices, and different when any input differs.
func TestSAS(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, SecretSize)
	dev := bytes.Repeat([]byte{9}, 32)
	got := SAS(secret, testPeerID, dev)
	if !regexp.MustCompile(`^\d{3} \d{3}$`).MatchString(got) {
		t.Fatalf("SAS = %q; want \"ddd ddd\"", got)
	}
	if again := SAS(secret, testPeerID, dev); again != got {
		t.Fatalf("SAS not deterministic: %q then %q", got, again)
	}
	otherDev := append([]byte(nil), dev...)
	otherDev[31] ^= 1
	otherSecret := append([]byte(nil), secret...)
	otherSecret[0] ^= 1
	for name, s := range map[string]string{
		"device key": SAS(secret, testPeerID, otherDev),
		"secret":     SAS(otherSecret, testPeerID, dev),
		"issuer":     SAS(secret, testPeerID+"x", dev),
	} {
		if s == got {
			t.Errorf("changing the %s left the SAS at %q", name, s)
		}
	}
	// Leading zeros are kept: the string is always seven characters.
	seen := map[int]bool{}
	for i := 0; i < 2000; i++ {
		dev[0], dev[1] = byte(i), byte(i>>8)
		seen[len(SAS(secret, testPeerID, dev))] = true
	}
	if len(seen) != 1 || !seen[7] {
		t.Fatalf("SAS lengths seen: %v", seen)
	}
}
