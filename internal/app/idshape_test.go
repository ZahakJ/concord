package app

import (
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

func TestEntityIDShape(t *testing.T) {
	for i := 0; i < 50; i++ {
		if id := domain.NewID(); !validEntityID(id) {
			t.Fatalf("a minted id is refused: %q", id)
		}
	}
	for _, id := range []string{"", "constructor", "__proto__", "c1", "g1",
		"0123456789abcdef0123456789abcdeF", "0123456789abcdef0123456789abcde", "0123456789abcdef0123456789abcdef0", "0123456789abcdef0123456789abcdeg"} {
		if validEntityID(id) {
			t.Errorf("validEntityID(%q) = true", id)
		}
	}
}

func TestChannelRecordsFromPeersAreShapeChecked(t *testing.T) {
	good, cat, fid := domain.NewID(), domain.NewID(), domain.NewID()
	ch := domain.Channel{ID: good, Category: "constructor", Links: []string{"toString", fid}}
	if !sanitizeChannelRefs(&ch) {
		t.Fatal("a well-formed channel was refused")
	}
	if ch.Category != "" || len(ch.Links) != 1 || ch.Links[0] != fid {
		t.Fatalf("malformed refs kept: %+v", ch)
	}
	ok := domain.Channel{ID: good, Category: cat, Parent: fid, Type: "thread"}
	if !sanitizeChannelRefs(&ok) || ok.Category != cat || ok.Parent != fid {
		t.Fatalf("well-formed refs changed: %+v", ok)
	}
	for _, bad := range []domain.Channel{{ID: "constructor"}, {ID: "__proto__"}, {ID: good, Parent: "constructor", Type: "thread"}} {
		b := bad
		if sanitizeChannelRefs(&b) {
			t.Errorf("a malformed channel record was admitted: %+v", bad)
		}
	}
	g := domain.Guild{ID: domain.NewID(), Channels: []domain.Channel{{ID: good}, {ID: "constructor"}}}
	sanitizeGuildRecord(&g)
	if len(g.Channels) != 1 || g.Channels[0].ID != good {
		t.Fatalf("guild record channels: %+v", g.Channels)
	}
}

func TestInviteGuildIDShape(t *testing.T) {
	s := &Service{guilds: map[string]*domain.Guild{"legacy-guild": {ID: "legacy-guild"}}}
	if !s.admitsInviteGuild(domain.NewID()) {
		t.Error("a well-formed guild id was refused")
	}
	if s.admitsInviteGuild("constructor") {
		t.Error("a malformed unknown guild id was admitted")
	}
	// A guild already here is never made unreachable by its id.
	if !s.admitsInviteGuild("legacy-guild") {
		t.Error("a guild already on this device was refused")
	}
	// And the code itself still round-trips a raw (non-hex) id, so an older
	// peer's code for such a guild still decodes for whoever holds it.
	ic, err := decodeInviteCode(encodeInviteCode(inviteCode{GuildID: "legacy-guild", OwnerID: "12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN"}))
	if err != nil || ic.GuildID != "legacy-guild" {
		t.Fatalf("raw guild id round trip: %v %+v", err, ic)
	}
}
