package app

import (
	"context"
	"testing"
	"time"
)

// TestLeaveGuildSticksAgainstAutoReadd is the regression for "I deleted some
// guilds but they came back": leaving wrote no intent anywhere, so any
// surviving copy of an invite — a linked device's hello offer, a re-link
// handover — silently re-added the guild. Leaving must (1) tombstone the
// guild, (2) veto every automatic adoption path, and (3) still yield to an
// EXPLICIT user re-join, which clears the tombstone.
func TestLeaveGuildSticksAgainstAutoReadd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	member := startService(t, ctx)

	g, err := owner.CreateGuild("Sticky Leave")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, owner, member)

	if err := member.LeaveGuild(g.ID); err != nil {
		t.Fatalf("LeaveGuild: %v", err)
	}
	if hasGuild(member, g.ID) {
		t.Fatal("guild still tracked right after LeaveGuild")
	}
	if !member.store.GuildIsLeft(g.ID) {
		t.Fatal("LeaveGuild wrote no tombstone — every auto re-add vector is open")
	}

	// The two automatic adoption paths a surviving invite rides in on: the
	// hello offer from another of our devices, and the device-link handover.
	// Both must refuse a tombstoned guild.
	member.joinOfferedInvite(code)
	if hasGuild(member, g.ID) {
		t.Fatal("hello-offer path re-added a guild the user left")
	}
	member.JoinLinkedInvite(code)
	if hasGuild(member, g.ID) {
		t.Fatal("link-handover path re-added a guild the user left")
	}

	// The human's own paste is the one voice allowed to override the leave.
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("explicit re-join after leave: %v", err)
	}
	if !hasGuild(member, g.ID) {
		t.Fatal("explicit re-join did not restore the guild")
	}
	if member.store.GuildIsLeft(g.ID) {
		t.Fatal("explicit re-join left the tombstone in place — the next restart would delete the guild again")
	}
}

// TestLeaveDMHidesWithoutTombstone: a 1:1 DM (Notes included) is special-cased
// to a non-destructive hide, and that path must NOT tombstone — the
// conversation is designed to resurface when either side messages again.
func TestLeaveDMHidesWithoutTombstone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startService(t, ctx)
	notes, err := svc.NotesDM()
	if err != nil {
		t.Fatalf("NotesDM: %v", err)
	}
	if err := svc.LeaveGuild(notes.ID); err != nil {
		t.Fatalf("LeaveGuild on a 1:1 DM: %v", err)
	}
	if !hasGuild(svc, notes.ID) {
		t.Fatal("closing a 1:1 DM destroyed it — the hide path must keep it tracked")
	}
	if svc.store.GuildIsLeft(notes.ID) {
		t.Fatal("hide path wrote a tombstone — a re-opened DM would be vetoed")
	}
}

// hasGuild reports whether a service currently tracks guildID (hidden DMs
// included — this asks about tracking, not visibility).
func hasGuild(s *Service, guildID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.guilds[guildID]
	return ok
}
