package app

import (
	"context"
	"testing"
	"time"
)

// TestPeerDM covers the click-profile-to-DM flow: A and B share a guild; A
// starts a DM with B (pushing a DM invite that B auto-redeems); both then hold
// the 2-person DM and can exchange end-to-end-encrypted messages.
func TestPeerDM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)

	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	ra, rb := &recorder{}, &recorder{}
	a.OnMessage(ra.add)
	b.OnMessage(rb.add)

	// A starts a DM with B (as if clicking B's profile → Message).
	dm, err := a.StartDM(b.Fingerprint())
	if err != nil {
		t.Fatalf("StartDM: %v", err)
	}
	if dm.Kind != "dm" {
		t.Fatalf("DM guild kind = %q, want dm", dm.Kind)
	}

	// B auto-redeems the pushed invite and joins the DM group.
	waitUntil(t, 25*time.Second, func() bool {
		for _, gg := range b.Guilds() {
			if gg.Kind == "dm" {
				return true
			}
		}
		return false
	}, "B never received the DM")

	// Both sides converge to a 2-member DM, then exchange messages.
	dmChannel := dm.Channels[0].ID
	waitUntil(t, 25*time.Second, func() bool {
		n, _ := a.MemberCount(dm.ID)
		return n == 2
	}, "DM group did not reach 2 members")

	sendUntilReceived(t, a, dmChannel, "hey euclid, DM test", rb)
	// B replies on its copy of the DM.
	var bDM string
	for _, gg := range b.Guilds() {
		if gg.Kind == "dm" {
			bDM = gg.Channels[0].ID
		}
	}
	sendUntilReceived(t, b, bDM, "got it, works!", ra)

	// Starting a DM with the same person again returns the SAME conversation.
	dm2, err := a.StartDM(b.Fingerprint())
	if err != nil {
		t.Fatalf("StartDM (repeat): %v", err)
	}
	if dm2.ID != dm.ID {
		t.Fatalf("second StartDM made a new DM (%s vs %s)", dm2.ID, dm.ID)
	}

	// A DM to self returns Notes.
	notes, err := a.StartDM(a.Fingerprint())
	if err != nil || notes.Name != notesGuildName {
		t.Fatalf("self StartDM should be Notes: %q %v", notes.Name, err)
	}
}
