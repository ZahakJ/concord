package domain

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// A founder-scheme group id is 0x01 || r || SHA-256(tag || 0 || guild || 0 ||
// founder || r)[:16]. Fixed inputs give a fixed id, and the id verifies only
// against the guild id and key it was made from.
func TestFounderGroupIDVector(t *testing.T) {
	guild := "0123456789abcdef0123456789abcdef"
	founder := bytes.Repeat([]byte{0x11}, 32)
	salt := bytes.Repeat([]byte{0x22}, 16)
	got := FounderGroupIDWithSalt(guild, founder, salt)
	want, _ := hex.DecodeString("01222222222222222222222222222222221b87e526200454249c3890fa7b93e9aa")
	if !bytes.Equal(got, want) {
		t.Fatalf("id %x, want %x", got, want)
	}
	if len(got) != FounderGroupIDLen || !IsFounderGroupID(got) {
		t.Fatalf("id %x is not founder-shaped", got)
	}
	if !FounderMatches(got, guild, founder) {
		t.Fatal("the id does not verify against its own inputs")
	}

	other := bytes.Repeat([]byte{0x12}, 32)
	if FounderMatches(got, guild, other) {
		t.Error("another key verifies")
	}
	if FounderMatches(got, "0123456789abcdef0123456789abcdee", founder) {
		t.Error("another guild id verifies")
	}
	if FounderMatches(got, guild, nil) {
		t.Error("no key verifies")
	}
	v2 := append([]byte{0x02}, got[1:]...)
	if IsFounderGroupID(v2) || FounderMatches(v2, guild, founder) {
		t.Error("an unknown version byte is taken")
	}
	flipped := append([]byte(nil), got...)
	flipped[5] ^= 1 // a salt byte
	if FounderMatches(flipped, guild, founder) {
		t.Error("a changed salt verifies")
	}
	if FounderMatches(got[:32], guild, founder) || FounderMatches(append(got, 0), guild, founder) {
		t.Error("a shortened or lengthened id verifies")
	}
	if FounderGroupIDWithSalt(guild, founder, salt[:15]) != nil {
		t.Error("a short salt was accepted")
	}
}

// A 16-byte id from before the scheme is never founder-shaped and never
// verifies, whatever its bytes.
func TestLegacyGroupIDNamesNobody(t *testing.T) {
	founder := bytes.Repeat([]byte{0x11}, 32)
	for _, gid := range [][]byte{
		bytes.Repeat([]byte{0x01}, LegacyGroupIDLen),
		make([]byte, LegacyGroupIDLen),
	} {
		if IsFounderGroupID(gid) || FounderMatches(gid, "g", founder) {
			t.Errorf("legacy id %x treated as founder-scheme", gid)
		}
	}
}

// Fresh ids for the same guild and founder differ (the salt), and each
// verifies.
func TestNewFounderGroupIDIsSalted(t *testing.T) {
	founder := bytes.Repeat([]byte{0x11}, 32)
	a, err := NewFounderGroupID("g", founder)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewFounderGroupID("g", founder)
	if bytes.Equal(a, b) {
		t.Fatal("two ids for the same guild and founder are equal")
	}
	if !FounderMatches(a, "g", founder) || !FounderMatches(b, "g", founder) {
		t.Fatal("a fresh id does not verify")
	}
	g := NewGuildWithID("g", "name", a, founder)
	if g.ID != "g" || g.Channels[0].GuildID != "g" {
		t.Fatalf("NewGuildWithID: id %q, channel guild %q", g.ID, g.Channels[0].GuildID)
	}
	m := NewMeetingGuildWithID("m", "room", a, founder)
	if m.ID != "m" || m.Channels[0].GuildID != "m" || m.Kind != "meeting" {
		t.Fatalf("NewMeetingGuildWithID: %+v", m)
	}
}
