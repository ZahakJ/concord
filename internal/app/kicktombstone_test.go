package app

import (
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
)

// The kick tombstone, at the level that decides it: the replay.
//
// The bug these pin was not in the replay — it was that there was nothing in the
// replay to consult. A kick was an MLS commit and nothing else, so the only
// question the admission gate could ask about a returning member was "are they
// banned", and a kick does not write a banlist. These fix the shape of the
// answer: a removal is a recorded decision, only somebody who could have made it
// can make it, and exactly two things lift it.

func channelOp(idKey *identity.Identity, seq uint64, typ, channelID, name string) govOp {
	o := govOp{
		Seq: seq, Signer: idKey.PublicKey(), Type: typ,
		ChannelID: channelID, Name: name, Time: int64(seq),
	}
	o.Sig = idKey.Sign(o.signingBytes())
	return o
}

func namedOp(idKey *identity.Identity, seq uint64, typ, name string) govOp {
	o := govOp{Seq: seq, Signer: idKey.PublicKey(), Type: typ, Name: name, Time: int64(seq)}
	o.Sig = idKey.Sign(o.signingBytes())
	return o
}

func TestKickIsRecordedAndOnlyLiftedDeliberately(t *testing.T) {
	owner := mustID(t)
	member := mustID(t)
	mFpr := member.Fingerprint()

	st := replayGuildOps(owner.PublicKey(), []govOp{
		banOp(owner, 1, "remove_member", mFpr),
	})
	if !st.Removed[mFpr] {
		t.Fatal("a kick left no record: the admission gate has nothing to consult")
	}
	if st.Banned[mFpr] {
		t.Fatal("a kick is not a ban")
	}

	// A readmit lifts it, and nothing else in an ordinary log does.
	st = replayGuildOps(owner.PublicKey(), []govOp{
		banOp(owner, 1, "remove_member", mFpr),
		banOp(owner, 2, "readmit", mFpr),
	})
	if st.Removed[mFpr] {
		t.Fatal("readmit did not lift the removal")
	}

	// Order is canonical, not arrival: a readmit that reaches us first must not
	// be folded after the kick it predates.
	st = replayGuildOps(owner.PublicKey(), []govOp{
		banOp(owner, 2, "readmit", mFpr),
		banOp(owner, 1, "remove_member", mFpr),
	})
	if st.Removed[mFpr] {
		t.Fatal("replay is order-dependent: the log folded out of canonical order")
	}
}

func TestBanImpliesRemovalAndUnbanLiftsBoth(t *testing.T) {
	owner := mustID(t)
	member := mustID(t)
	mFpr := member.Fingerprint()

	st := replayGuildOps(owner.PublicKey(), []govOp{banOp(owner, 1, "ban", mFpr)})
	if !st.Removed[mFpr] || !st.Banned[mFpr] {
		t.Fatalf("ban did not record a removal: removed=%v banned=%v", st.Removed[mFpr], st.Banned[mFpr])
	}

	// Kick, then ban, then unban. The trap this closes: a tombstone left standing
	// after an unban locks someone out with no lever the UI offers.
	st = replayGuildOps(owner.PublicKey(), []govOp{
		banOp(owner, 1, "remove_member", mFpr),
		banOp(owner, 2, "ban", mFpr),
		banOp(owner, 3, "unban", mFpr),
	})
	if st.Banned[mFpr] || st.Removed[mFpr] {
		t.Fatalf("unban left them locked out: banned=%v removed=%v", st.Banned[mFpr], st.Removed[mFpr])
	}
}

func TestReadmitCannotSmugglePastABan(t *testing.T) {
	owner := mustID(t)
	member := mustID(t)
	mFpr := member.Fingerprint()

	st := replayGuildOps(owner.PublicKey(), []govOp{
		banOp(owner, 1, "ban", mFpr),
		banOp(owner, 2, "readmit", mFpr),
	})
	if !st.Removed[mFpr] || !st.Banned[mFpr] {
		t.Fatal("readmit reopened the door on a banned fingerprint")
	}
}

func TestOnlyManageMembersCanKick(t *testing.T) {
	owner := mustID(t)
	nobody := mustID(t)
	victim := mustID(t)
	vFpr := victim.Fingerprint()

	// An ordinary member's perfectly-signed kick folds to nothing.
	st := replayGuildOps(owner.PublicKey(), []govOp{banOp(nobody, 1, "remove_member", vFpr)})
	if st.Removed[vFpr] {
		t.Fatal("a member with no permission removed someone")
	}

	// The owner cannot be removed, by anyone, ever.
	mod := mustID(t)
	st = replayGuildOps(owner.PublicKey(), []govOp{
		upsertRole(owner, 1, "r", "Mod", PermManageMembers, 1),
		assignRole(owner, 2, mod.Fingerprint(), "r", true),
		banOp(mod, 3, "remove_member", owner.Fingerprint()),
	})
	if st.Removed[owner.Fingerprint()] {
		t.Fatal("a moderator removed the owner")
	}
	// ...but that same moderator can remove an ordinary member.
	st = replayGuildOps(owner.PublicKey(), []govOp{
		upsertRole(owner, 1, "r", "Mod", PermManageMembers, 1),
		assignRole(owner, 2, mod.Fingerprint(), "r", true),
		banOp(mod, 3, "remove_member", vFpr),
	})
	if !st.Removed[vFpr] {
		t.Fatal("a moderator holding manage-members could not remove a member")
	}
}

func TestNamingSomeoneOwnerReadmitsThem(t *testing.T) {
	owner := mustID(t)
	heir := mustID(t)
	hFpr := heir.Fingerprint()

	st := replayGuildOps(owner.PublicKey(), []govOp{
		banOp(owner, 1, "remove_member", hFpr),
		banOp(owner, 2, "transfer_owner", hFpr),
	})
	if st.Removed[hFpr] {
		t.Fatal("the new owner is still marked removed — they would be refused at their own gate")
	}
	if st.Owner() != hFpr {
		t.Fatalf("ownership did not move: %q", st.Owner())
	}
}

// An op type this build has never heard of must still be relayed, still be
// verifiable, and still change nothing — the property that makes adding the
// types above safe for peers that predate them. Asserted here as well as in
// govlog_test.go because it is the compat story for this whole batch.
func TestUnknownGovOpTypeIsInertNotFatal(t *testing.T) {
	owner := mustID(t)
	victim := mustID(t)
	unknown := namedOp(owner, 2, "confiscate_everything", "x")

	st, verdicts := replayGuildOpsRecording(owner.PublicKey(), []govOp{
		banOp(owner, 1, "remove_member", victim.Fingerprint()),
		unknown,
		banOp(owner, 3, "ban", victim.Fingerprint()),
	}, true)
	if v := verdicts[unknown.hash()]; !v.Verified || v.Applied {
		t.Fatalf("unknown op: want verified-but-not-applied, got %+v", v)
	}
	// And it did not stop the fold: the ops on either side of it both landed.
	if !st.Removed[victim.Fingerprint()] || !st.Banned[victim.Fingerprint()] {
		t.Fatal("an unrecognised op broke the replay of the ops around it")
	}
}
