package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestConcurrentChannelRenamesConverge pins two admins renaming the same
// channel at once. The rename frames are applied in arrival order, so each
// peer used to keep whichever reached it last, for good. The log orders both
// renames the same way everywhere, and that order now decides — including
// against a losing frame that arrives after the log has settled.
func TestConcurrentChannelRenamesConverge(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	admin := startService(t, ctx)
	member := startService(t, ctx)
	g, err := owner.CreateGuild("Two Pens")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	for _, s := range []*Service{admin, member} {
		if _, err := s.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitMembers(t, 30*time.Second, 3, owner, admin, member)
	role, _ := owner.UpsertRole(g.ID, "", "Admin", "#e91e63", PermManageChannels, 5)
	_ = owner.AssignRole(g.ID, admin.id.Fingerprint(), role, true)
	waitUntil(t, 20*time.Second, func() bool { return admin.HasPermission(g.ID, PermManageChannels) }, "admin never got Manage channels")
	ch := g.Channels[0].ID

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = owner.RenameChannel(g.ID, ch, "alice-name") }()
	go func() { defer wg.Done(); _ = admin.RenameChannel(g.ID, ch, "bob-name") }()
	wg.Wait()

	nameOn := func(s *Service) string { return s.channelByID(g.ID, ch).Name }
	// Agreement on screen is not enough: the frames travel apart from the log
	// ops, so all three can show one name while a peer's log still lacks a
	// rename, and the other frame, still on its way, then lands on a log that
	// cannot overrule it. Wait until every log holds both renames and every
	// peer shows what its log says.
	renamesLogged := func(s *Service) int {
		s.mu.RLock()
		defer s.mu.RUnlock()
		n := 0
		for _, o := range s.govOps[g.ID] {
			if o.Type == "channel_rename" && o.ChannelID == ch {
				n++
			}
		}
		return n
	}
	waitUntil(t, 30*time.Second, func() bool {
		want := nameOn(owner)
		for _, s := range []*Service{owner, admin, member} {
			if renamesLogged(s) < 2 || nameOn(s) != want || s.loggedChannels(g.ID)[ch].name != want {
				return false
			}
		}
		return want != ""
	}, "the three peers never agreed on the channel's name")
	winner := nameOn(owner)
	loser := "alice-name"
	loserActor := owner.id.Fingerprint()
	if winner == "alice-name" {
		loser, loserActor = "bob-name", admin.id.Fingerprint()
	}
	// The losing frame turning up late (a slow link, a redelivery) changes nothing.
	member.applyChannelRename(g.ID, loserActor, ch, loser)
	if got := nameOn(member); got != winner {
		t.Fatalf("a late losing rename frame set the name to %q, want %q", got, winner)
	}
}
