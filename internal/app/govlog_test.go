package app

import (
	"testing"

	"github.com/ZahakJ/concord/internal/identity"
)

// The moderation log's claim is that a member can read what happened and check
// it. That claim only holds if the verdict the screen prints is the verdict the
// replay actually reached — so these tests are about the three things a row can
// say, and about never saying the wrong one.

// A signature that does not check out must be reported as such, and must never
// be reported as having changed anything.
func TestGovLogFlagsAForgedSignature(t *testing.T) {
	owner := mustID(t)
	stranger := mustID(t)
	victim := identity.FingerprintOf(mustID(t).PublicKey())

	good := banOp(owner, 1, "ban", victim)
	// Take a validly-signed op and change what it says, keeping the signature.
	// This is exactly the shape of a doctored op arriving from a sync responder,
	// which "decryption is not authorization" says must fail closed.
	forged := good
	forged.Seq = 2
	forged.Signer = stranger.PublicKey()

	_, verdicts := replayGuildOpsRecording(owner.PublicKey(), []govOp{good, forged}, true)

	if v := verdicts[good.hash()]; !v.Verified || !v.Applied {
		t.Fatalf("the owner's own ban should verify and apply, got %+v", v)
	}
	if v := verdicts[forged.hash()]; v.Verified || v.Applied {
		t.Fatalf("a doctored op must be neither verified nor applied, got %+v", v)
	}
}

// The case the panel exists to be honest about: a perfectly signed op from
// somebody who was not allowed to do it. It verifies, and it changed nothing.
// A log that showed only the signature would print a ban that never happened.
func TestGovLogSeparatesSignedFromPermitted(t *testing.T) {
	owner := mustID(t)
	nobody := mustID(t)
	victimID := mustID(t)
	victim := identity.FingerprintOf(victimID.PublicKey())

	// An ordinary member with no roles signs a ban. The signature is genuine.
	overreach := banOp(nobody, 1, "ban", victim)

	st, verdicts := replayGuildOpsRecording(owner.PublicKey(), []govOp{overreach}, true)

	v := verdicts[overreach.hash()]
	if !v.Verified {
		t.Fatal("the op is genuinely signed; the log must say so")
	}
	if v.Applied {
		t.Fatal("an unauthorized ban must not read as applied")
	}
	if st.Banned[victim] {
		t.Fatal("and it must not actually have banned anyone")
	}
}

// Authority moves during the fold, so the verdict for one op depends on the ops
// before it. A moderator's ban applies while they hold the role and stops
// applying once it is taken away — which is why the verdicts come from a real
// replay rather than a per-op guess.
func TestGovLogVerdictFollowsTheRoleThroughTheLog(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	modFpr := identity.FingerprintOf(mod.PublicKey())
	a := identity.FingerprintOf(mustID(t).PublicKey())
	b := identity.FingerprintOf(mustID(t).PublicKey())

	whileMod := banOp(mod, 3, "ban", a)
	afterMod := banOp(mod, 5, "ban", b)

	ops := []govOp{
		upsertRole(owner, 1, "r_mod", "Mod", PermManageMembers, 10),
		assignRole(owner, 2, modFpr, "r_mod", true),
		whileMod,
		assignRole(owner, 4, modFpr, "r_mod", false), // the role is taken back
		afterMod,
	}

	st, verdicts := replayGuildOpsRecording(owner.PublicKey(), ops, true)

	if v := verdicts[whileMod.hash()]; !v.Verified || !v.Applied {
		t.Fatalf("the ban issued while holding the role should apply, got %+v", v)
	}
	if v := verdicts[afterMod.hash()]; !v.Verified || v.Applied {
		t.Fatalf("the ban issued after losing the role should not apply, got %+v", v)
	}
	if !st.Banned[a] || st.Banned[b] {
		t.Fatalf("state disagrees with the verdicts: a=%v b=%v", st.Banned[a], st.Banned[b])
	}
}

// Recording must not change what the replay decides. If it did, the screen would
// be describing a different fold from the one the app is actually running on.
func TestGovLogRecordingDoesNotChangeTheFold(t *testing.T) {
	owner := mustID(t)
	mod := mustID(t)
	modFpr := identity.FingerprintOf(mod.PublicKey())
	victim := identity.FingerprintOf(mustID(t).PublicKey())

	ops := []govOp{
		upsertRole(owner, 1, "r_mod", "Mod", PermManageMembers|PermMuteMembers, 10),
		assignRole(owner, 2, modFpr, "r_mod", true),
		banOp(mod, 3, "ban", victim),
		banOp(mod, 4, "unban", victim),
		banOp(mod, 5, "mute", victim),
		{Seq: 6, Signer: owner.PublicKey(), Type: "not_a_real_op", Time: 6},
	}
	ops[5].Sig = owner.Sign(ops[5].signingBytes())

	plain := replayGuildOps(owner.PublicKey(), ops)
	recorded, verdicts := replayGuildOpsRecording(owner.PublicKey(), ops, true)

	if plain.Owner() != recorded.Owner() {
		t.Fatal("recording moved the owner")
	}
	if len(plain.Banned) != len(recorded.Banned) || len(plain.Muted) != len(recorded.Muted) {
		t.Fatal("recording changed the folded state")
	}
	if len(verdicts) != len(ops) {
		t.Fatalf("every op should have a verdict, got %d for %d ops", len(verdicts), len(ops))
	}
	// An op type this build does not know is signed, and folds to nothing.
	if v := verdicts[ops[5].hash()]; !v.Verified || v.Applied {
		t.Fatalf("an unknown op type should verify but not apply, got %+v", v)
	}
}
