package store

import "testing"

// TestLeftGuildTombstone covers the leave-tombstone lifecycle that makes
// "leave sticks" possible: mark → visible, clear → gone, and both idempotent.
// The tombstone is what adoption paths consult before re-adding a guild, so a
// false negative here IS the resurrection bug.
func TestLeftGuildTombstone(t *testing.T) {
	s, _ := openTestStore(t)

	if s.GuildIsLeft("g1") {
		t.Fatal("fresh store claims g1 was left")
	}
	if err := s.MarkGuildLeft("g1"); err != nil {
		t.Fatalf("MarkGuildLeft: %v", err)
	}
	if !s.GuildIsLeft("g1") {
		t.Fatal("tombstone not visible after MarkGuildLeft")
	}
	// Marking twice must not error (leave → re-join-fail → leave again).
	if err := s.MarkGuildLeft("g1"); err != nil {
		t.Fatalf("MarkGuildLeft twice: %v", err)
	}
	// Unrelated guilds are unaffected.
	if s.GuildIsLeft("g2") {
		t.Fatal("tombstone for g1 leaked onto g2")
	}

	_ = s.MarkGuildLeft("g2")
	left, err := s.LeftGuilds()
	if err != nil {
		t.Fatalf("LeftGuilds: %v", err)
	}
	if len(left) != 2 {
		t.Fatalf("LeftGuilds = %v, want two entries", left)
	}

	if err := s.ClearGuildLeft("g1"); err != nil {
		t.Fatalf("ClearGuildLeft: %v", err)
	}
	if s.GuildIsLeft("g1") {
		t.Fatal("tombstone survives ClearGuildLeft — an explicit re-join could never stick")
	}
	if !s.GuildIsLeft("g2") {
		t.Fatal("clearing g1 also cleared g2")
	}
	// Clearing a guild that was never left is a no-op, not an error (every
	// user-initiated join clears unconditionally).
	if err := s.ClearGuildLeft("never-left"); err != nil {
		t.Fatalf("ClearGuildLeft on unknown id: %v", err)
	}
}

// TestLeftGuildSurvivesReopen: the tombstone is intent that must outlive the
// process — a restart was one of the reported resurrection vectors.
func TestLeftGuildSurvivesReopen(t *testing.T) {
	s, path := openTestStore(t)
	if err := s.MarkGuildLeft("g1"); err != nil {
		t.Fatalf("MarkGuildLeft: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = 0x42
	}
	s2, err := Open(path, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if !s2.GuildIsLeft("g1") {
		t.Fatal("tombstone lost across reopen")
	}
}
