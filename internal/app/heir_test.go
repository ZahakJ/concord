package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// Named-heir succession, pure-replay side (govstate.go). Like
// transferowner_test.go these inject ops straight into replayGuildOps,
// bypassing the ingest-time membership gate on purpose: replay's own contract
// is under test — only the then-current owner's signature records an heir,
// only the named heir's signature converts the designation into ownership,
// and any ownership change voids the designation.

func setHeirOp(id *identity.Identity, seq uint64, target string) govOp {
	o := govOp{Seq: seq, Signer: id.PublicKey(), Type: "set_heir", Target: target, Time: int64(seq)}
	o.Sig = id.Sign(o.signingBytes())
	return o
}

func claimHeirOp(id *identity.Identity, seq uint64) govOp {
	o := govOp{Seq: seq, Signer: id.PublicKey(), Type: "claim_heir", Time: int64(seq)}
	o.Sig = id.Sign(o.signingBytes())
	return o
}

func TestSetHeirRecordsChangesAndClears(t *testing.T) {
	founder := mustID(t)
	basil := mustID(t)
	clover := mustID(t)
	basilFpr := identity.FingerprintOf(basil.PublicKey())
	cloverFpr := identity.FingerprintOf(clover.PublicKey())

	st := replayGuildOps(founder.PublicKey(), []govOp{setHeirOp(founder, 1, basilFpr)})
	if st.Heir() != basilFpr {
		t.Fatalf("heir = %q, want %q", st.Heir(), basilFpr)
	}

	// Re-issuing replaces the designation; an empty target revokes it.
	st = replayGuildOps(founder.PublicKey(), []govOp{
		setHeirOp(founder, 1, basilFpr),
		setHeirOp(founder, 2, cloverFpr),
	})
	if st.Heir() != cloverFpr {
		t.Fatalf("heir = %q, want the re-issued %q", st.Heir(), cloverFpr)
	}
	st = replayGuildOps(founder.PublicKey(), []govOp{
		setHeirOp(founder, 1, basilFpr),
		setHeirOp(founder, 2, ""),
	})
	if st.Heir() != "" {
		t.Fatalf("heir = %q, want cleared", st.Heir())
	}
}

func TestSetHeirByNonOwnerOrForgedIgnored(t *testing.T) {
	founder := mustID(t)
	mallory := mustID(t)
	malloryFpr := identity.FingerprintOf(mallory.PublicKey())

	// Mallory names herself heir; and a forgery NAMING the founder as signer
	// but actually signed with mallory's key.
	forged := govOp{Seq: 2, Signer: founder.PublicKey(), Type: "set_heir", Target: malloryFpr, Time: 2}
	forged.Sig = mallory.Sign(forged.signingBytes())

	st := replayGuildOps(founder.PublicKey(), []govOp{
		setHeirOp(mallory, 1, malloryFpr),
		forged,
	})
	if st.Heir() != "" {
		t.Fatalf("heir = %q, want none — a non-owner's or forged set_heir must be inert", st.Heir())
	}
}

func TestClaimHeirMovesOwnershipAndConsumesDesignation(t *testing.T) {
	founder := mustID(t)
	heir := mustID(t)
	founderFpr := identity.FingerprintOf(founder.PublicKey())
	heirFpr := identity.FingerprintOf(heir.PublicKey())

	st := replayGuildOps(founder.PublicKey(), []govOp{
		setHeirOp(founder, 1, heirFpr),
		claimHeirOp(heir, 2),
	})
	if st.Owner() != heirFpr {
		t.Fatalf("owner = %q, want the claiming heir %q", st.Owner(), heirFpr)
	}
	if st.Heir() != "" {
		t.Fatalf("heir = %q, want consumed after the claim", st.Heir())
	}
	// The crown fully moved: the new owner holds everything, the founder nothing.
	if !st.Can(st.Owner(), heirFpr, permAll) {
		t.Fatal("the claiming heir must implicitly hold every permission")
	}
	if st.Can(st.Owner(), founderFpr, PermManageMembers) {
		t.Fatal("the vanished founder must retain no implicit authority")
	}
}

func TestClaimHeirByNonHeirIgnored(t *testing.T) {
	founder := mustID(t)
	heir := mustID(t)
	mallory := mustID(t)
	founderFpr := identity.FingerprintOf(founder.PublicKey())
	heirFpr := identity.FingerprintOf(heir.PublicKey())

	// A claim with no designation at all, and a claim by someone who isn't the
	// named heir — both inert.
	st := replayGuildOps(founder.PublicKey(), []govOp{claimHeirOp(mallory, 1)})
	if st.Owner() != founderFpr {
		t.Fatalf("owner = %q, want %q — a claim with no designation must be inert", st.Owner(), founderFpr)
	}
	st = replayGuildOps(founder.PublicKey(), []govOp{
		setHeirOp(founder, 1, heirFpr),
		claimHeirOp(mallory, 2),
	})
	if st.Owner() != founderFpr || st.Heir() != heirFpr {
		t.Fatalf("owner=%q heir=%q — a non-heir's claim must change nothing", st.Owner(), st.Heir())
	}
}

func TestRevokedHeirCannotClaim(t *testing.T) {
	founder := mustID(t)
	heir := mustID(t)
	founderFpr := identity.FingerprintOf(founder.PublicKey())
	heirFpr := identity.FingerprintOf(heir.PublicKey())

	set := setHeirOp(founder, 1, heirFpr)
	revoke := setHeirOp(founder, 2, "")
	claim := claimHeirOp(heir, 3)

	// Canonical order (Seq) decides, not arrival order: however the three ops
	// reach a peer, the revoke lands before the claim and the claim is dead.
	forward := replayGuildOps(founder.PublicKey(), []govOp{set, revoke, claim})
	shuffled := replayGuildOps(founder.PublicKey(), []govOp{claim, set, revoke})
	if forward.Owner() != founderFpr || shuffled.Owner() != founderFpr {
		t.Fatalf("owner forward=%q shuffled=%q, want %q — a revoked designation must not be claimable",
			forward.Owner(), shuffled.Owner(), founderFpr)
	}
}

func TestOwnershipChangeVoidsHeirDesignation(t *testing.T) {
	founder := mustID(t)
	heir := mustID(t)
	buyer := mustID(t)
	heirFpr := identity.FingerprintOf(heir.PublicKey())
	buyerFpr := identity.FingerprintOf(buyer.PublicKey())

	// A voluntary transfer voids the old owner's designation: the heir was the
	// OLD owner's trust decision, and the new owner names their own.
	st := replayGuildOps(founder.PublicKey(), []govOp{
		setHeirOp(founder, 1, heirFpr),
		transferOp(founder, 2, buyerFpr),
		claimHeirOp(heir, 3), // stale claim against the voided designation
	})
	if st.Owner() != buyerFpr {
		t.Fatalf("owner = %q, want %q — a stale heir claim must not override a transfer", st.Owner(), buyerFpr)
	}
	if st.Heir() != "" {
		t.Fatalf("heir = %q, want voided by the transfer", st.Heir())
	}

	// A claim voids it the same way: the crown moved, the designation is spent.
	st = replayGuildOps(founder.PublicKey(), []govOp{
		setHeirOp(founder, 1, heirFpr),
		claimHeirOp(heir, 2),
		claimHeirOp(heir, 3), // double-claim: second is inert (heir cleared)
	})
	if st.Owner() != heirFpr || st.Heir() != "" {
		t.Fatalf("owner=%q heir=%q — a claim must consume the designation", st.Owner(), st.Heir())
	}
}

func TestBannedHeirCannotClaimOrBeNamed(t *testing.T) {
	founder := mustID(t)
	outcast := mustID(t)
	founderFpr := identity.FingerprintOf(founder.PublicKey())
	outcastFpr := identity.FingerprintOf(outcast.PublicKey())

	// Naming a banned fingerprint is inert; and an heir banned AFTER being
	// named cannot cash the designation while the ban stands.
	st := replayGuildOps(founder.PublicKey(), []govOp{
		banOp(founder, 1, "ban", outcastFpr),
		setHeirOp(founder, 2, outcastFpr),
	})
	if st.Heir() != "" {
		t.Fatalf("heir = %q, want none — a banned fingerprint can't be named heir", st.Heir())
	}
	st = replayGuildOps(founder.PublicKey(), []govOp{
		setHeirOp(founder, 1, outcastFpr),
		banOp(founder, 2, "ban", outcastFpr),
		claimHeirOp(outcast, 3),
	})
	if st.Owner() != founderFpr {
		t.Fatalf("owner = %q, want %q — a banned heir must not be able to claim", st.Owner(), founderFpr)
	}
}

// TestClaimHeirDrivesAuthorizedCommitter pins the enforcement point: after a
// claim, honest peers apply the NEW owner's membership commits and refuse the
// vanished founder's — same gate as a voluntary transfer, because the claim
// moves ownership through the same replayed chain.
func TestClaimHeirDrivesAuthorizedCommitter(t *testing.T) {
	founder := mustID(t)
	heir := mustID(t)
	heirFpr := identity.FingerprintOf(heir.PublicKey())

	s := &Service{
		guilds:   map[string]*domain.Guild{"g1": {ID: "g1", OwnerID: founder.PublicKey()}},
		govOps:   map[string][]govOp{},
		govState: map[string]GuildState{},
	}
	s.govOps["g1"] = []govOp{
		setHeirOp(founder, 1, heirFpr),
		claimHeirOp(heir, 2),
	}
	s.rebuildGovStateLocked("g1")

	if !s.authorizedCommitter("g1", heir.PublicKey()) {
		t.Fatal("after the claim the heir's commits must be accepted")
	}
	if s.authorizedCommitter("g1", founder.PublicKey()) {
		t.Fatal("after the claim the founder's commits must be refused")
	}
	if !s.IsGuildOwner("g1", heirFpr) || s.IsGuildOwner("g1", identity.FingerprintOf(founder.PublicKey())) {
		t.Fatal("IsGuildOwner must follow the claimed crown")
	}
	if s.GuildHeir("g1") != "" {
		t.Fatal("the designation must be consumed by the claim")
	}
}

// TestHeirClaimLive is the end-to-end succession: A founds and names B heir,
// every peer converges on the designation, revocation kills a claim, a
// re-issued designation lets B take the crown with the MLS epoch untouched
// and comms flowing, and a restarted peer replays to the same owner.
func TestHeirClaimLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx) // founder, later vanishes (narratively)
	b := startService(t, ctx) // the heir
	// C gets a FIXED dir so it can be restarted to prove replay determinism.
	cDir := t.TempDir()
	c, err := Start(ctx, Config{DataDir: cDir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatalf("Start C: %v", err)
	}
	defer func() { _ = c.Close() }()

	ra, rb, rc := &recorder{}, &recorder{}, &recorder{}
	a.OnMessage(ra.add)
	b.OnMessage(rb.add)
	c.OnMessage(rc.add)

	g, err := a.CreateGuild("succession")
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

	groupID := theGuild(t, a).GroupID
	epochBefore, err := a.mls.Epoch(a.ctx, groupID)
	if err != nil {
		t.Fatalf("epoch before: %v", err)
	}

	// Issue-side gates: only the owner names an heir, only a member can be
	// one, and only the named heir may claim.
	if err := b.SetHeir(g.ID, c.Fingerprint()); err == nil {
		t.Fatal("a non-owner's set-heir must be refused at issue")
	}
	if err := a.SetHeir(g.ID, identity.FingerprintOf(mustID(t).PublicKey())); err == nil {
		t.Fatal("naming a non-member heir must be refused at issue")
	}
	if err := c.ClaimOwnership(g.ID); err == nil {
		t.Fatal("a claim by someone who was never named must be refused at issue")
	}

	// A names B — all three peers converge on the same designation.
	if err := a.SetHeir(g.ID, b.Fingerprint()); err != nil {
		t.Fatalf("SetHeir: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		for _, s := range []*Service{a, b, c} {
			if s.GuildHeir(g.ID) != b.Fingerprint() {
				return false
			}
		}
		return true
	}, "all three peers must converge on B as the heir")

	// Hand-crafted grabs by C, ingested at an honest peer, are inert on
	// replay: a claim not signed by the named heir, and a set_heir not signed
	// by the owner. (C IS a member, so the ingest membership gates pass —
	// the replay rules are what stop these.)
	grab := govOp{Seq: 99, Signer: c.PublicKey(), Type: "claim_heir", Time: time.Now().UnixNano()}
	grab.Sig = c.id.Sign(grab.signingBytes())
	a.ingestGovOp(g.ID, grab, true)
	usurp := govOp{Seq: 100, Signer: c.PublicKey(), Type: "set_heir", Target: c.Fingerprint(), Time: time.Now().UnixNano()}
	usurp.Sig = c.id.Sign(usurp.signingBytes())
	a.ingestGovOp(g.ID, usurp, true)
	if !a.IsGuildOwner(g.ID, a.Fingerprint()) || a.GuildHeir(g.ID) != b.Fingerprint() {
		t.Fatal("C's crafted claim/set_heir must be inert on replay at an honest peer")
	}

	// Revocation beats a later claim: A clears the designation, and B's claim
	// is refused at issue AND a hand-crafted one is inert on replay.
	if err := a.ClearHeir(g.ID); err != nil {
		t.Fatalf("ClearHeir: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return a.GuildHeir(g.ID) == "" && b.GuildHeir(g.ID) == "" && c.GuildHeir(g.ID) == ""
	}, "all three peers must converge on the revoked designation")
	if err := b.ClaimOwnership(g.ID); err == nil {
		t.Fatal("a claim after revocation must be refused at issue")
	}
	stale := govOp{Seq: 101, Signer: b.PublicKey(), Type: "claim_heir", Time: time.Now().UnixNano()}
	stale.Sig = b.id.Sign(stale.signingBytes())
	a.ingestGovOp(g.ID, stale, true)
	if !a.IsGuildOwner(g.ID, a.Fingerprint()) {
		t.Fatal("a crafted claim against a revoked designation must be inert on replay")
	}

	// A names B again, then "vanishes". B cashes the designation.
	if err := a.SetHeir(g.ID, b.Fingerprint()); err != nil {
		t.Fatalf("SetHeir (again): %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return b.GuildHeir(g.ID) == b.Fingerprint()
	}, "B must see the re-issued designation")
	if err := b.ClaimOwnership(g.ID); err != nil {
		t.Fatalf("ClaimOwnership: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		for _, s := range []*Service{a, b, c} {
			if !s.IsGuildOwner(g.ID, b.Fingerprint()) || s.IsGuildOwner(g.ID, a.Fingerprint()) || s.GuildHeir(g.ID) != "" {
				return false
			}
		}
		return true
	}, "all three peers must converge on B as owner with the designation consumed")

	// The claim is governance-lane only: same MLS epoch, no re-key, and the
	// guild still talks in both directions across the succession.
	epochAfter, err := a.mls.Epoch(a.ctx, groupID)
	if err != nil {
		t.Fatalf("epoch after: %v", err)
	}
	if epochAfter != epochBefore {
		t.Fatalf("epoch moved %d → %d across the claim — it must not commit to MLS", epochBefore, epochAfter)
	}
	sendUntilReceived(t, a, channel, "ex-owner-still-talks", rb, rc)
	sendUntilReceived(t, b, channel, "new-owner-talks", ra, rc)

	// Replay-from-scratch determinism: restart C and let it refold the
	// persisted log — it must land on the same owner without any network help.
	if err := c.Close(); err != nil {
		t.Fatalf("close C: %v", err)
	}
	c2, err := Start(ctx, Config{DataDir: cDir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatalf("restart C: %v", err)
	}
	defer func() { _ = c2.Close() }()
	if !c2.IsGuildOwner(g.ID, b.Fingerprint()) || c2.IsGuildOwner(g.ID, a.Fingerprint()) {
		t.Fatal("a restarted peer must replay to the same owner")
	}

	// The dethroned founder can no longer kick; the new owner can, and the
	// ex-owner APPLIES the new owner's commit.
	if err := a.RemoveMember(g.ID, c.PublicKey()); err == nil {
		t.Fatal("the ex-owner's kick must be refused at issue")
	}
	if err := b.RemoveMember(g.ID, c.PublicKey()); err != nil {
		t.Fatalf("new owner's RemoveMember: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		na, _ := a.MemberCount(g.ID)
		nb, _ := b.MemberCount(g.ID)
		return na == 2 && nb == 2
	}, "A and B must converge to 2 members after the new owner's kick")
	sendUntilReceived(t, b, channel, "after-the-kick", ra)
}
