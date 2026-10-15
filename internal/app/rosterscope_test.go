package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// pretendMembers makes the roster cache answer that fprs are a guild's
// members, for tests that need a membership the MLS group would take a second
// peer to produce. Stamped in the future so the cache's TTL never expires it;
// a commit or welcome for the guild drops it like any other entry.
func pretendMembers(s *Service, guildID string, fprs []string) {
	c := &memberSet{fprs: map[string]bool{}, at: time.Now().Add(time.Hour)}
	for _, f := range fprs {
		if !c.fprs[f] {
			c.fprs[f] = true
			c.list = append(c.list, f)
		}
	}
	s.memberSetMu.Lock()
	s.memberSets[guildID] = c
	s.memberSetMu.Unlock()
}

// A guild's sync and its invite handshake carry the profiles of that guild's
// members and nobody else's. Before, both carried every profile this peer had
// learned anywhere: a member of one guild could read, off an ordinary
// catch-up, the name and mailbox key of someone you only ever talk to in
// another guild or a private DM — and the requester's inventory of digests
// handed the same list of fingerprints to whoever answered.
func TestGuildSyncCarriesOnlyThatGuildsProfiles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	if err := s.SetDisplayName("Self"); err != nil {
		t.Fatal(err)
	}
	ga, err := s.CreateGuild("A")
	if err != nil {
		t.Fatal(err)
	}
	gb, err := s.CreateGuild("B")
	if err != nil {
		t.Fatal(err)
	}
	inA := strings.Repeat("a", 64)
	onlyB := strings.Repeat("b", 64)
	s.mu.Lock()
	s.profiles[inA] = Profile{Name: "Ada", MailboxPub: []byte("mbx-ada")}
	s.profiles[onlyB] = Profile{Name: "Bilal", MailboxPub: []byte("mbx-bilal")}
	s.mu.Unlock()
	self := s.Fingerprint()
	pretendMembers(s, ga.ID, []string{self, inA})
	pretendMembers(s, gb.ID, []string{self, onlyB})

	now := time.Now().Unix()
	src := s.syncSourceFor(ga.ID, ga, now)
	src.reqFpr = inA
	payload, _ := buildSyncPayload(src, nil)
	if _, leaked := payload.Profiles[onlyB]; leaked {
		t.Fatal("guild A's sync payload carries the profile of someone known only through guild B")
	}
	if payload.Profiles[self].Name != "Self" {
		t.Fatalf("guild A's sync lost our own profile: %+v", payload.Profiles[self])
	}
	srcSelf := s.syncSourceFor(ga.ID, ga, now)
	if _, ok := srcSelf.profiles[inA]; !ok {
		t.Fatal("guild A's sync no longer carries its own member's profile")
	}
	if d := s.syncDigestFor(ga.ID, ga, now); d.Profiles[onlyB] != "" {
		t.Fatal("guild A's sync inventory names someone known only through guild B")
	}
	if _, leaked := s.joinRoster(ga.ID)[onlyB]; leaked {
		t.Fatal("guild A's invite response carries the profile of someone known only through guild B")
	}

	// And the profile still travels where it belongs.
	if _, ok := s.syncSourceFor(gb.ID, gb, now).profiles[onlyB]; !ok {
		t.Fatal("guild B's sync stopped carrying its own member's profile")
	}
	if _, ok := s.joinRoster(gb.ID)[onlyB]; !ok {
		t.Fatal("guild B's invite response stopped carrying its own member's profile")
	}
}
