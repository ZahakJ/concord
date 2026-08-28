package app

import (
	"context"
	"testing"
	"time"
)

// The unresolvable banner. A guild whose only other members have been removed
// has nobody who could ever bring the missing updates, and the owner of one
// watched "Waiting for someone with the missing updates to come online" for
// forty-five minutes. The predicate underneath the fix has to be exact: it reads
// the ROSTER, because members who are merely offline can still turn up, and a
// banner that gives up on them would be the opposite error.
func TestStrandingResolvesOnlyWhenNobodyIsLeft(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	member := startService(t, ctx)

	g, err := owner.CreateGuild("Riverside Makers")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, owner, member)

	if owner.aloneInGuild(g.ID) {
		t.Fatal("a guild with two members reported itself empty — the banner would give up on a member who is merely away")
	}

	if err := owner.KickMember(g.ID, member.id.Fingerprint()); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool { return owner.aloneInGuild(g.ID) },
		"the roster still lists a removed member")

	// Now strand it the way the critic's session did — unreadable traffic and
	// nobody who could bridge it — and confirm the pass ends rather than parking.
	owner.setOutOfSync(g.ID, true)
	owner.recoverOutOfSync(g.ID)
	if owner.OutOfSync(g.ID) {
		t.Fatal("a guild with nobody left in it is still waiting for someone to come online")
	}
}
