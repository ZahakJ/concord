package app

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// A realistic invite: hex guild ID (MLS group ID), valid owner peer ID, LAN +
// QUIC addrs plus a relay circuit derived from the one rendezvous node.
func realisticInvite() inviteCode {
	const pid = "12D3KooWCzonwxmETSLwgDY9JAe9WczUHssnYTrSpCqkp6UXZg1q"
	boot := "/dns/concord-rdv.example.dev/tcp/4001/p2p/" + pid
	return inviteCode{
		GuildID:   "a539ad2bf25ecce33cbe1c767b3825c7",
		GuildName: "Toluene",
		OwnerID:   pid,
		OwnerAddr: []string{
			"/ip4/192.168.1.23/tcp/4001",
			"/ip4/192.168.1.23/udp/4001/quic-v1",
			boot + "/p2p-circuit",
		},
		Bootstrap: []string{boot},
	}
}

// TestInviteCodeCompactFormat pins the compact invite format: round-trip
// (circuit addr re-derived), a fraction of the legacy size, legacy decodes.
func TestInviteCodeCompactFormat(t *testing.T) {
	ic := realisticInvite()

	code := encodeInviteCode(ic)
	if !strings.HasPrefix(code, invitePrefix) {
		t.Fatalf("expected %s prefix, got %q", invitePrefix, code[:8])
	}
	if len(code) >= 350 {
		t.Fatalf("compact invite code should be <350 chars, got %d", len(code))
	}

	got, err := decodeInviteCode(code)
	if err != nil {
		t.Fatalf("decodeInviteCode: %v", err)
	}
	if got.GuildID != ic.GuildID || got.GuildName != ic.GuildName || got.OwnerID != ic.OwnerID {
		t.Fatalf("invite did not round-trip: %+v", got)
	}
	for _, want := range ic.OwnerAddr { // incl. the elided-then-restored circuit
		found := false
		for _, a := range got.OwnerAddr {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("addr %q lost in round-trip (got %v)", want, got.OwnerAddr)
		}
	}
	if len(got.Bootstrap) != 1 || got.Bootstrap[0] != ic.Bootstrap[0] {
		t.Fatalf("bootstrap did not round-trip: %v", got.Bootstrap)
	}

	// Legacy JSON+base64url invites must still decode.
	legacyJSON, _ := json.Marshal(ic)
	legacy := base64.RawURLEncoding.EncodeToString(legacyJSON)
	lic, err := decodeInviteCode(legacy)
	if err != nil {
		t.Fatalf("legacy invite must decode: %v", err)
	}
	if lic.GuildID != ic.GuildID || lic.OwnerID != ic.OwnerID {
		t.Fatalf("legacy invite mangled: %+v", lic)
	}
	if len(code) >= len(legacy)/2 {
		t.Fatalf("compact invite (%d chars) should be under half the legacy size (%d chars)",
			len(code), len(legacy))
	}
	t.Logf("invite code: legacy %d chars → compact %d chars", len(legacy), len(code))
}

// A non-hex guild ID (or uppercase hex, which wouldn't survive re-encoding)
// must fall back to the raw-string representation losslessly.
func TestInviteCodeNonHexGuildID(t *testing.T) {
	ic := realisticInvite()
	ic.GuildID = "NOT-hex-ID"
	got, err := decodeInviteCode(encodeInviteCode(ic))
	if err != nil {
		t.Fatal(err)
	}
	if got.GuildID != ic.GuildID {
		t.Fatalf("raw guild id mangled: %q", got.GuildID)
	}
}
