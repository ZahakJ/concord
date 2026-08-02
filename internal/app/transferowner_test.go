package app

import (
	"context"
	"testing"
	"time"

	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
)

// Ownership transfer, pure-replay side (govstate.go). These tests inject ops
// straight into replayGuildOps, which deliberately bypasses the ingest-time
// membership gate — replay's own contract is what's under test here: only the
// THEN-current owner's signature moves the crown, in canonical order, and
// every later authority rule re-anchors at the new owner.

func transferOp(id *identity.Identity, seq uint64, target string) govOp {
	o := govOp{Seq: seq, Signer: id.PublicKey(), Type: "transfer_owner", Target: target, Time: int64(seq)}
	o.Sig = id.Sign(o.signingBytes())
	return o
}

func TestTransferOwnerMovesEffectiveOwner(t *testing.T) {
	founder := mustID(t)
	heir := mustID(t)
	bystander := mustID(t)
	founderFpr := identity.FingerprintOf(founder.PublicKey())
	heirFpr := identity.FingerprintOf(heir.PublicKey())
	bystanderFpr := identity.FingerprintOf(bystander.PublicKey())

	st := replayGuildOps(founder.PublicKey(), []govOp{
		transferOp(founder, 1, heirFpr),
	})
	if st.Owner() != heirFpr {
		t.Fatalf("owner = %q, want the heir %q", st.Owner(), heirFpr)
	}
	if !st.Can(st.Owner(), heirFpr, permAll) {
		t.Fatal("the new owner must implicitly hold every permission")
	}
	if st.Can(st.Owner(), founderFpr, PermManageMembers) {
		t.Fatal("the dethroned founder must be an ordinary member with no implicit authority")
	}

	// The crown's immunities move with it: the new owner bans the founder
	// (possible now), and nobody can ban the new owner.
	st = replayGuildOps(founder.PublicKey(), []govOp{
		transferOp(founder, 1, heirFpr),
		banOp(heir, 2, "ban", founderFpr),
		banOp(founder, 3, "ban", bystanderFpr), // ex-owner has no ban authority left
	})
	if !st.Banned[founderFpr] {
		t.Fatal("the ex-owner must be bannable by the new owner")
	}
	if st.Banned[bystanderFpr] {
		t.Fatal("the ex-owner must not retain ban authority after the transfer")
	}
}

func TestTransferOwnerByNonOwnerIgnored(t *testing.T) {
	founder := mustID(t)
	mallory := mustID(t)
	founderFpr := identity.FingerprintOf(founder.PublicKey())
	malloryFpr := identity.FingerprintOf(mallory.PublicKey())

	// A hand-crafted grab: mallory signs herself the crown; and a forgery:
	// an op NAMING the founder as signer but signed by mallory's key.
	forged := govOp{Seq: 2, Signer: founder.PublicKey(), Type: "transfer_owner", Target: malloryFpr, Time: 2}
	forged.Sig = mallory.Sign(forged.signingBytes())

	st := replayGuildOps(founder.PublicKey(), []govOp{
		transferOp(mallory, 1, malloryFpr),
		forged,
	})
	if st.Owner() != founderFpr {
		t.Fatalf("owner = %q, want the founder %q — a non-owner transfer must be dead on replay", st.Owner(), founderFpr)
	}
}

func TestTransferOwnerChainConvergesOrderIndependent(t *testing.T) {
	a, b, c, d := mustID(t), mustID(t), mustID(t), mustID(t)
	bFpr := identity.FingerprintOf(b.PublicKey())
	cFpr := identity.FingerprintOf(c.PublicKey())
	dFpr := identity.FingerprintOf(d.PublicKey())

	ab := transferOp(a, 1, bFpr) // A → B
	bc := transferOp(b, 2, cFpr) // then B → C
	// A stale grab by the dethroned founder AFTER handing over — worthless.
	ad := transferOp(a, 3, dFpr)

	forward := replayGuildOps(a.PublicKey(), []govOp{ab, bc, ad})
	shuffled := replayGuildOps(a.PublicKey(), []govOp{ad, bc, ab})
	if forward.Owner() != cFpr || shuffled.Owner() != cFpr {
		t.Fatalf("chain must converge on C regardless of arrival order: forward=%q shuffled=%q want=%q",
			forward.Owner(), shuffled.Owner(), cFpr)
	}
	_ = dFpr
}

func TestTransferOwnerSelfAndBannedTargetIgnored(t *testing.T) {
	founder := mustID(t)
	outcast := mustID(t)
	founderFpr := identity.FingerprintOf(founder.PublicKey())
	outcastFpr := identity.FingerprintOf(outcast.PublicKey())

	st := replayGuildOps(founder.PublicKey(), []govOp{
		transferOp(founder, 1, founderFpr), // self-transfer: a no-op, not a change
		banOp(founder, 2, "ban", outcastFpr),
		transferOp(founder, 3, outcastFpr), // a banned fingerprint can't take the crown
	})
	if st.Owner() != founderFpr {
		t.Fatalf("owner = %q, want %q — self/banned transfers must not move ownership", st.Owner(), founderFpr)
	}
}

// TestEffectiveOwnerDrivesAuthorizedCommitter pins the enforcement point: after
// a transfer, honest peers apply the NEW owner's membership commits and refuse
// the founder's. Built on a bare Service (maps only) because authorizedCommitter
// is a pure read of guilds+govState.
func TestEffectiveOwnerDrivesAuthorizedCommitter(t *testing.T) {
	founder := mustID(t)
	heir := mustID(t)
	heirFpr := identity.FingerprintOf(heir.PublicKey())

	s := &Service{
		guilds:   map[string]*domain.Guild{"g1": {ID: "g1", OwnerID: founder.PublicKey()}},
		govOps:   map[string][]govOp{},
		govState: map[string]GuildState{},
	}
	s.rebuildGovStateLocked("g1")

	if !s.authorizedCommitter("g1", founder.PublicKey()) {
		t.Fatal("before any transfer the founder must be the authorized committer")
	}
	if s.authorizedCommitter("g1", heir.PublicKey()) {
		t.Fatal("a plain member must not be an authorized committer")
	}

	s.govOps["g1"] = []govOp{transferOp(founder, 1, heirFpr)}
	s.rebuildGovStateLocked("g1")

	if !s.authorizedCommitter("g1", heir.PublicKey()) {
		t.Fatal("after the transfer the new owner's commits must be accepted")
	}
	if s.authorizedCommitter("g1", founder.PublicKey()) {
		t.Fatal("after the transfer the founder's commits must be refused")
	}
	if !s.IsGuildOwner("g1", heirFpr) || s.IsGuildOwner("g1", identity.FingerprintOf(founder.PublicKey())) {
		t.Fatal("IsGuildOwner must follow the effective owner")
	}
}

// TestTransferOwnershipLive is the end-to-end handover: three real peers, A
// founds, transfers to B, and every trust decision follows — B's kick is
// applied by all, A's crafted commit is refused, the MLS epoch never moves
// (the transfer is ONE signed gov op, not a membership change), and messages
// keep flowing across the handover.
func TestTransferOwnershipLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx) // founder
	b := startService(t, ctx) // heir
	c := startService(t, ctx) // bystander (later kicked by B)

	ra, rb, rc := &recorder{}, &recorder{}, &recorder{}
	a.OnMessage(ra.add)
	b.OnMessage(rb.add)
	c.OnMessage(rc.add)

	g, err := a.CreateGuild("handover")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B join: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)
	if _, err := c.JoinViaInvite(code); err != nil {
		t.Fatalf("C join: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, a, b, c)

	groupID := a.Guilds()[0].GroupID
	epochBefore, err := a.mls.Epoch(a.ctx, groupID)
	if err != nil {
		t.Fatalf("epoch before: %v", err)
	}

	// Issue-side gates: a non-owner cannot transfer, and the owner cannot
	// transfer to a stranger.
	if err := b.TransferOwnership(g.ID, c.Fingerprint()); err == nil {
		t.Fatal("a non-owner's transfer must be refused at issue")
	}
	if err := a.TransferOwnership(g.ID, identity.FingerprintOf(mustID(t).PublicKey())); err == nil {
		t.Fatal("a transfer to a non-member must be refused at issue")
	}

	// The handover.
	if err := a.TransferOwnership(g.ID, b.Fingerprint()); err != nil {
		t.Fatalf("TransferOwnership: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		for _, s := range []*Service{a, b, c} {
			if !s.IsGuildOwner(g.ID, b.Fingerprint()) || s.IsGuildOwner(g.ID, a.Fingerprint()) {
				return false
			}
		}
		return true
	}, "all three peers must converge on B as the effective owner")

	// The transfer must not have touched the MLS group: same epoch, no re-key.
	epochAfter, err := a.mls.Epoch(a.ctx, groupID)
	if err != nil {
		t.Fatalf("epoch after: %v", err)
	}
	if epochAfter != epochBefore {
		t.Fatalf("epoch moved %d → %d across the transfer — it must not commit to MLS", epochBefore, epochAfter)
	}

	// A hand-crafted transfer op gossiped to an honest peer is inert on replay:
	// C signs itself the crown and A ingests it (C IS a member, so the ingest
	// membership gate passes — only the replay owner-chain check stops it).
	grab := govOp{Seq: 99, Signer: c.PublicKey(), Type: "transfer_owner", Target: c.Fingerprint(), Time: time.Now().UnixNano()}
	grab.Sig = c.id.Sign(grab.signingBytes())
	a.ingestGovOp(g.ID, grab)
	if !a.IsGuildOwner(g.ID, b.Fingerprint()) {
		t.Fatal("a non-owner's hand-crafted transfer op must be ignored on replay")
	}

	// Comms are unbroken across the handover, in both directions.
	sendUntilReceived(t, a, channel, "still-here-from-A", rb, rc)
	sendUntilReceived(t, b, channel, "crown-fits-from-B", ra, rc)

	// The dethroned founder can no longer kick — refused at issue.
	if err := a.RemoveMember(g.ID, c.PublicKey()); err == nil {
		t.Fatal("the ex-owner's kick must be refused at issue")
	}

	// The new owner CAN kick, and the ex-owner applies B's commit (the gate
	// recognizes B's authority everywhere, not just on B's own screen).
	if err := b.RemoveMember(g.ID, c.PublicKey()); err != nil {
		t.Fatalf("new owner's RemoveMember: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		na, _ := a.MemberCount(g.ID)
		nb, _ := b.MemberCount(g.ID)
		return na == 2 && nb == 2
	}, "A and B must converge to 2 members after the new owner's kick")

	// And the guild still talks.
	sendUntilReceived(t, b, channel, "after-the-kick", ra)

	// Last (it forks the crafter's own ratchet, so nothing else can follow):
	// the ex-owner CRAFTS a membership commit removing B and publishes it —
	// honest B must refuse to apply it, exactly like the pre-transfer
	// unauthorized-committer test but with the roles reversed.
	rogue, err := a.mls.Remove(a.ctx, groupID, b.PublicKey())
	if err == nil {
		_ = a.ps.Publish(a.ctx, domain.ControlTopicID(groupID), rogue)
		time.Sleep(3 * time.Second)
		if n, _ := b.MemberCount(g.ID); n != 2 {
			t.Fatalf("B applied the ex-owner's rogue commit (members=%d, want 2)", n)
		}
		if !b.IsGuildOwner(g.ID, b.Fingerprint()) {
			t.Fatal("B must still be the owner after the ex-owner's rogue commit")
		}
	}
}
