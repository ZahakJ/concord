package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestDMInviteRejectsForcedGuildJoin covers the auto-accept hardening: a peer
// that pushes an invite for anything other than a genuine 2-person DM with
// itself (here, a full guild it owns) must NOT be able to silently force the
// victim into that group.
func TestDMInviteRejectsForcedGuildJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	attacker := startService(t, ctx)
	victim := startService(t, ctx)

	// A shared guild just to connect the two peers.
	shared, err := attacker.CreateGuild("shared")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := attacker.InviteCode(shared.ID)
	if _, err := victim.JoinViaInvite(code); err != nil {
		t.Fatalf("victim join shared: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, attacker, victim)

	// The attacker owns a SEPARATE full guild and pushes its invite over the
	// DM-invite (auto-accept) channel — the abuse the hardening must stop.
	trap, err := attacker.CreateGuild("trap")
	if err != nil {
		t.Fatalf("CreateGuild trap: %v", err)
	}
	trapCode, _ := attacker.InviteCode(trap.ID)
	pid, ok := attacker.peerForFingerprint(victim.Fingerprint())
	if !ok {
		t.Fatal("attacker cannot reach victim")
	}
	req, _ := json.Marshal(dmInvite{Code: trapCode})
	if _, err := attacker.host.RequestDMInvite(ctx, pid, req); err != nil {
		t.Fatalf("push dm-invite: %v", err)
	}

	// Give the victim time to (attempt to) join and then undo it.
	time.Sleep(3 * time.Second)

	for _, g := range victim.Guilds() {
		if g.ID == trap.ID {
			t.Fatal("victim was force-joined into the attacker's guild via a DM invite")
		}
	}
}

// TestCreateGroupDMRequiresVerified covers the verified-gate: unverified
// contacts are rejected up front, before any group is created or network used.
func TestCreateGroupDMRequiresVerified(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)

	before := len(a.Guilds())
	// Arbitrary, unverified fingerprints are rejected.
	if _, err := a.CreateGroupDM([]string{"fpr-bob-000", "fpr-cara-00"}); err == nil {
		t.Fatal("CreateGroupDM accepted unverified contacts")
	}
	// A single target (even ignoring verification) can't form a group.
	if _, err := a.CreateGroupDM([]string{"fpr-bob-000"}); err == nil {
		t.Fatal("CreateGroupDM accepted a group of one")
	}
	// No stray group was created by the rejected calls.
	if n := len(a.Guilds()); n != before {
		t.Fatalf("rejected CreateGroupDM left %d guilds, want %d", n, before)
	}
}

// TestGroupDMHappyPath covers the full verified-gated flow: A shares a guild
// with B and C, verifies both, then opens a group DM. B and C auto-accept
// (they share a group with the inviter) and all three converge on one 3-member
// DM group they can talk in.
func TestGroupDMHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	c := startService(t, ctx)

	// A shared guild connects the three peers.
	g, err := a.CreateGuild("hub")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B join: %v", err)
	}
	if _, err := c.JoinViaInvite(code); err != nil {
		t.Fatalf("C join: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, a, b, c)

	// A verifies both, then opens the group DM.
	if err := a.VerifyFingerprint(b.Fingerprint()); err != nil {
		t.Fatalf("verify B: %v", err)
	}
	if err := a.VerifyFingerprint(c.Fingerprint()); err != nil {
		t.Fatalf("verify C: %v", err)
	}
	dm, err := a.CreateGroupDM([]string{b.Fingerprint(), c.Fingerprint()})
	if err != nil {
		t.Fatalf("CreateGroupDM: %v", err)
	}
	if dm.Kind != "dm" {
		t.Fatalf("group DM kind = %q, want dm", dm.Kind)
	}

	// All three converge on the same 3-member DM group (B and C accepted, did
	// not leave).
	waitUntil(t, 30*time.Second, func() bool {
		for _, s := range []*Service{a, b, c} {
			n, _ := s.MemberCount(dm.ID)
			if n != 3 {
				return false
			}
		}
		return true
	}, "group DM did not converge to 3 members on all peers")

	// And they can actually talk in it.
	rb, rc := &recorder{}, &recorder{}
	b.OnMessage(rb.add)
	c.OnMessage(rc.add)
	sendUntilReceived(t, a, dm.Channels[0].ID, "welcome to the group", rb, rc)

	// Asking for a group with the same people again reuses the conversation
	// instead of creating a duplicate (order-independent).
	dm2, err := a.CreateGroupDM([]string{c.Fingerprint(), b.Fingerprint()})
	if err != nil {
		t.Fatalf("CreateGroupDM (repeat): %v", err)
	}
	if dm2.ID != dm.ID {
		t.Fatalf("duplicate group DM created (%s vs %s)", dm2.ID, dm.ID)
	}
}

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
