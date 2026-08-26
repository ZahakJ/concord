package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// testAvatar is a data URI of about size bytes — small enough to be legal, big
// enough that shipping one per member is the thing that made a join expensive.
func testAvatar(size int) string {
	const px = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg"
	var b strings.Builder
	b.WriteString("data:image/png;base64,")
	for b.Len() < size {
		b.WriteString(px)
	}
	return b.String()
}

// TestJoinRosterCarriesNamesAndConverges is the contract behind the join-time
// payload diet: a new member must be able to READ the room the moment they
// arrive — every name, straight away — and the pictures must arrive on their
// own shortly after, without anybody asking for them.
//
// The first half is what the diet costs (one member's avatar is deliberately
// not in the response) and the second half is why that is safe: the member who
// owns the avatar re-announces its profile to the mesh as soon as it meets the
// newcomer, so the gap closes in seconds and closes from the AUTHOR, not from a
// relayed copy.
func TestJoinRosterCarriesNamesAndConverges(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	early := startServiceOn(t, ctx, t.TempDir(), boot)
	late := startServiceOn(t, ctx, t.TempDir(), boot)

	if err := owner.SetProfile(Profile{Name: "Owner", Avatar: testAvatar(4096)}); err != nil {
		t.Fatalf("owner SetProfile: %v", err)
	}
	if err := early.SetProfile(Profile{Name: "Early", Avatar: testAvatar(4096)}); err != nil {
		t.Fatalf("early SetProfile: %v", err)
	}
	if err := late.SetDisplayName("Late"); err != nil {
		t.Fatalf("late SetDisplayName: %v", err)
	}

	g, err := owner.CreateGuild("portraits")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := early.JoinViaInvite(code); err != nil {
		t.Fatalf("early JoinViaInvite: %v", err)
	}
	// The owner must actually hold Early's picture before we can prove it chose
	// not to forward it.
	waitUntil(t, 60*time.Second, func() bool {
		return owner.ProfileOf(early.Fingerprint()).Avatar != ""
	}, "the owner never learned the early member's avatar")

	// What the owner will actually put on the wire. Asserted here rather than
	// on the joiner's side because the early member's own announce reaches the
	// newcomer within milliseconds — which is the point, and which would make
	// "the joiner has no avatar yet" a race rather than a proof.
	sent := owner.joinRoster(g.ID)
	if p, ok := sent[early.Fingerprint()]; !ok || p.Name != "Early" {
		t.Fatalf("the early member is missing from the roster we serve: %+v", p)
	} else if p.Avatar != "" {
		t.Fatalf("the roster still carries a %d-byte avatar for an ordinary member", len(p.Avatar))
	}
	if p := sent[owner.Fingerprint()]; p.Avatar == "" {
		t.Fatal("the admitting member's own picture must survive the diet")
	}

	if _, err := late.JoinViaInvite(code); err != nil {
		t.Fatalf("late JoinViaInvite: %v", err)
	}

	// Immediately on arrival: every name is there.
	if got := late.ProfileOf(early.Fingerprint()).Name; got != "Early" {
		t.Fatalf("a new member sees %q where the early member's name should be — "+
			"the roster must carry names, whatever else it drops", got)
	}
	if got := late.ProfileOf(owner.Fingerprint()).Name; got != "Owner" {
		t.Fatalf("a new member sees %q where the owner's name should be", got)
	}
	// The two faces a new member sees without going looking came whole.
	if late.ProfileOf(owner.Fingerprint()).Avatar == "" {
		t.Fatal("the admitting member's own avatar must ride the invite response")
	}

	// And the rest fills in by itself.
	waitUntil(t, 90*time.Second, func() bool {
		return late.ProfileOf(early.Fingerprint()).Avatar != ""
	}, "the early member's avatar never reached the new member")
}
