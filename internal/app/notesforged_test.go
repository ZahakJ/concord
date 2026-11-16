package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// shapeAsNotesOf rewrites s's record of a guild, in memory and in the store,
// into the shape of whose's Notes: a DM owned by whose, named Notes, created
// before anything else. That is the record an inviter hands a joiner verbatim.
func shapeAsNotesOf(t *testing.T, s *Service, guildID string, whose *Service) {
	t.Helper()
	s.mu.Lock()
	g := s.guilds[guildID]
	g.Kind, g.Name, g.OwnerID = "dm", notesGuildName, whose.PublicKey()
	g.Created = time.Unix(1, 0)
	rec := g.Clone()
	s.mu.Unlock()
	if err := s.store.SaveGuild(rec); err != nil {
		t.Fatal(err)
	}
}

// ZW-01. A group counts as this account's Notes only if every leaf of it is a
// device of this account. A record shaped as our Notes on a group that seats
// another account is not our Notes, however early its Created: the private
// own-device lanes are never encrypted to it.
func TestNotesShapedGroupWithAnotherAccountIsNotNotes(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, gid, _ := guardedPair(t, ctx, boot)
	canon, ok := u.findNotesDM()
	if !ok {
		t.Fatal("setup: no Notes")
	}
	shapeAsNotesOf(t, u, gid, u)
	u.mu.RLock()
	shaped := isNotesGuild(*u.guilds[gid], u.PublicKey())
	u.mu.RUnlock()
	if !shaped {
		t.Fatal("setup: the shared group's record does not have the Notes shape")
	}
	got, ok := u.findNotesDM()
	if !ok || got.ID != canon.ID {
		t.Fatalf("findNotesDM = %s (%v), want the real Notes %s and never the shared group %s", got.ID, ok, canon.ID, gid)
	}
	// Every other reader of "a Notes group" applies the same test.
	if u.IsNotesGroup(gid) || u.NotesCopy(gid) {
		t.Errorf("the shared group reads as a Notes group (IsNotesGroup %v, NotesCopy %v)", u.IsNotesGroup(gid), u.NotesCopy(gid))
	}
	if held, notes := u.holdsNotes(gid); !held || notes {
		t.Errorf("holdsNotes(shared group) = %v, %v; want held and not Notes", held, notes)
	}
	ids := u.notesGuildIDs()
	if !slices.Contains(ids, canon.ID) || slices.Contains(ids, gid) {
		t.Errorf("notesGuildIDs = %v, want the real Notes %s and never the shared group %s", ids, canon.ID, gid)
	}
	if !u.IsNotesGroup(canon.ID) || u.NotesCopy(canon.ID) {
		t.Error("the real Notes does not read as the Notes")
	}
	if !p.guildHasMember(gid, u.Fingerprint()) {
		t.Fatal("setup: the shared group lost U")
	}
}

// ZW-01. A joiner refuses a record shaped as its own Notes from an inviter
// that is not one of its own devices.
func TestJoinRefusesOurNotesFromAnotherAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u := startServiceOn(t, ctx, t.TempDir(), boot)
	p := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := p.CreateGuild("offered")
	if err != nil {
		t.Fatal(err)
	}
	code, err := p.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	shapeAsNotesOf(t, p, g.ID, u)
	_, err = u.JoinViaInvite(code)
	if err == nil || !strings.Contains(err.Error(), "Notes") {
		t.Fatalf("JoinViaInvite of our Notes offered by another account: err=%v, want a refusal", err)
	}
	u.mu.RLock()
	_, held := u.guilds[g.ID]
	u.mu.RUnlock()
	if held {
		t.Fatal("the joiner holds the record it refused")
	}
}
