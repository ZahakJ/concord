package app

import (
	"context"
	"testing"
	"time"
)

// Adding someone to an existing group DM must actually bring them in. It
// returned OK and sent a guild-join prompt instead, so the person added never
// received the group.
func TestAddMemberToGroupDMDeliversTheGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	c := startService(t, ctx)
	d := startService(t, ctx)
	g, err := a.CreateGuild("hub")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := a.InviteCode(g.ID)
	for _, s := range []*Service{b, c, d} {
		if _, err := s.JoinViaInvite(code); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	waitMembers(t, 40*time.Second, 4, a, b, c, d)
	for _, s := range []*Service{b, c, d} {
		if err := a.VerifyFingerprint(s.Fingerprint()); err != nil {
			t.Fatalf("verify: %v", err)
		}
	}
	dm, err := a.CreateGroupDM([]string{b.Fingerprint(), c.Fingerprint()})
	if err != nil {
		t.Fatalf("CreateGroupDM: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := a.MemberCount(dm.ID)
		return n == 3
	}, "group DM never gathered its first three")

	if err := a.AddMember(dm.ID, d.Fingerprint()); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		if n, _ := d.MemberCount(dm.ID); n != 4 {
			return false
		}
		n, _ := a.MemberCount(dm.ID)
		return n == 4
	}, "the added member never received the group DM")
}

// Leaving a group DM told nobody: the others kept the leaver on their list
// and kept encrypting to them. Now the leave is announced and the creator,
// who may commit removals, takes the leaver's leaves out of the group.
func TestLeavingAGroupDMRemovesTheLeaverForEveryone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	c := startService(t, ctx)
	g, err := a.CreateGuild("hub")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := a.InviteCode(g.ID)
	for _, s := range []*Service{b, c} {
		if _, err := s.JoinViaInvite(code); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	waitMembers(t, 30*time.Second, 3, a, b, c)
	for _, s := range []*Service{b, c} {
		if err := a.VerifyFingerprint(s.Fingerprint()); err != nil {
			t.Fatalf("verify: %v", err)
		}
	}
	dm, err := a.CreateGroupDM([]string{b.Fingerprint(), c.Fingerprint()})
	if err != nil {
		t.Fatalf("CreateGroupDM: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		for _, s := range []*Service{a, b, c} {
			if n, _ := s.MemberCount(dm.ID); n != 3 {
				return false
			}
		}
		return true
	}, "group DM never gathered")

	if err := c.LeaveGuild(dm.ID); err != nil {
		t.Fatalf("LeaveGuild: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		na, _ := a.MemberCount(dm.ID)
		nb, _ := b.MemberCount(dm.ID)
		return na == 2 && nb == 2 && !a.guildHasMember(dm.ID, c.Fingerprint())
	}, "the others still count the leaver as a member")
}
