package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// These tests drive each place a guild, channel or category id first arrives
// from another peer, and check that an id of the wrong shape stops there
// while a well-formed one beside it still arrives.

const malformedID = "not-a-minted-id"

// joinedPair: A owns a guild, B has joined it through A's invite.
func joinedPair(t *testing.T, ctx context.Context) (a, b *Service, g domain.Guild) {
	t.Helper()
	a = startService(t, ctx)
	b = startService(t, ctx)
	g, err := a.CreateGuild("shape")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 60*time.Second, 2, a, b)
	return a, b, g
}

// plant puts a channel straight into a service's guild and store, the way a
// peer on an older build could hold one, without any check.
func plant(t *testing.T, s *Service, guildID string, ch domain.Channel) {
	t.Helper()
	s.mu.Lock()
	g := s.guilds[guildID]
	ch.GuildID = guildID
	g.Channels = append(g.Channels, ch)
	s.channelToGuild[ch.ID] = guildID
	gc := g.Clone()
	s.mu.Unlock()
	if err := s.store.SaveGuild(gc); err != nil {
		t.Fatal(err)
	}
}

func holdsChannel(s *Service, guildID, channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return false
	}
	for _, c := range g.Channels {
		if c.ID == channelID {
			return true
		}
	}
	return false
}

func holdsCategory(t *testing.T, s *Service, guildID, catID string) bool {
	t.Helper()
	cats, err := s.Categories(guildID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.ID == catID {
			return true
		}
	}
	return false
}

// Gossip: channel_added and category_added from a member who may add them.
func TestIDShapeAtGuildMeta(t *testing.T) {
	if testing.Short() {
		t.Skip("networked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b, g := joinedPair(t, ctx)

	a.publishMeta(g.GroupID, guildMeta{Type: "channel_added", Channel: domain.Channel{ID: malformedID, GuildID: g.ID, Name: "odd", Type: "text"}})
	a.publishMeta(g.GroupID, guildMeta{Type: "category_added", Category: domain.Category{ID: malformedID, GuildID: g.ID, Name: "Odd"}})
	good, err := a.CreateChannel(g.ID, "fine", "text", "")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := a.CreateCategory(g.ID, "Fine")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return holdsChannel(b, g.ID, good.ID) && holdsCategory(t, b, g.ID, cat.ID)
	}, "B never learned the well-formed channel and category")
	if holdsChannel(b, g.ID, malformedID) {
		t.Error("a channel with a malformed id was adopted from guild meta")
	}
	if holdsCategory(t, b, g.ID, malformedID) {
		t.Error("a category with a malformed id was adopted from guild meta")
	}
}

// History sync: channels and categories in a peer's snapshot.
func TestIDShapeAtSync(t *testing.T) {
	if testing.Short() {
		t.Skip("networked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b, g := joinedPair(t, ctx)

	good := domain.Channel{ID: domain.NewID(), Name: "later", Type: "text"}
	plant(t, a, g.ID, good)
	plant(t, a, g.ID, domain.Channel{ID: malformedID, Name: "odd", Type: "text"})
	goodCat := domain.Category{ID: domain.NewID(), GuildID: g.ID, Name: "Later"}
	if err := a.store.SaveCategory(goodCat); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SaveCategory(domain.Category{ID: malformedID, GuildID: g.ID, Name: "Odd"}); err != nil {
		t.Fatal(err)
	}

	if err := b.syncGuildFromPeer(g.ID, a.host.PeerID()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !holdsChannel(b, g.ID, good.ID) || !holdsCategory(t, b, g.ID, goodCat.ID) {
		t.Fatal("the well-formed channel and category did not arrive by sync")
	}
	if holdsChannel(b, g.ID, malformedID) {
		t.Error("a channel with a malformed id was adopted from sync")
	}
	if holdsCategory(t, b, g.ID, malformedID) {
		t.Error("a category with a malformed id was adopted from sync")
	}
}

// Joining: the guild record the inviter answers with.
func TestIDShapeAtJoinResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("networked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("shape")
	if err != nil {
		t.Fatal(err)
	}
	plant(t, a, g.ID, domain.Channel{ID: malformedID, Name: "odd", Type: "text"})
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := b.JoinViaInvite(code)
	if err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	if !holdsChannel(b, g.ID, g.Channels[0].ID) {
		t.Fatal("the guild's own channel did not arrive with the join")
	}
	for _, c := range joined.Channels {
		if c.ID == malformedID {
			t.Fatal("the joined guild record carries a malformed channel id")
		}
	}
	if holdsChannel(b, g.ID, malformedID) {
		t.Error("a channel with a malformed id was adopted from the join response")
	}
}

// Invites: an unknown guild id of the wrong shape, on each join path.
func TestIDShapeAtInvite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	ic := inviteCode{GuildID: malformedID, GuildName: notesGuildName, OwnerID: s.PeerID()}
	code := encodeInviteCode(ic)

	// The user's own paste: refused before it touches anything, including
	// the record of a guild the user left.
	if err := s.store.MarkGuildLeft(malformedID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinViaInvite(code); err == nil || err.Error() != "app: bad invite code" {
		t.Errorf("JoinViaInvite: got %v, want a refusal", err)
	}
	if !s.store.GuildIsLeft(malformedID) {
		t.Error("JoinViaInvite changed stored state for a malformed guild id")
	}
	// The automatic path (another device's offer, a link hand-over): the
	// code must not even be remembered as a pending offer. (A second id, as
	// the first now carries a leave record, which that path honours first.)
	ic = inviteCode{GuildID: malformedID + "-2", GuildName: notesGuildName, OwnerID: s.PeerID()}
	code = encodeInviteCode(ic)
	s.joinOfferedInvite(code)
	s.joiningMu.Lock()
	offered := s.notesOffer
	s.joiningMu.Unlock()
	if offered != "" {
		t.Error("an offered invite for a malformed guild id was recorded")
	}
	// The join proper, which every path reaches.
	done := make(chan error, 1)
	go func() { _, err := s.redeemInvite(ic, nil); done <- err }()
	select {
	case err := <-done:
		if err == nil || err.Error() != "app: bad invite code" {
			t.Errorf("redeemInvite: got %v, want a refusal", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("redeemInvite went on to contact the inviter for a malformed guild id")
	}
	s.mu.RLock()
	_, held := s.guilds[malformedID]
	_, held2 := s.guilds[ic.GuildID]
	held = held || held2
	s.mu.RUnlock()
	if held {
		t.Error("a malformed guild id reached the guild list")
	}
}
