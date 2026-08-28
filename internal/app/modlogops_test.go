package app

import "testing"

// The audit-only ops record who did something and change nothing. What the
// replay contributes is the VERDICT, and that is the whole reason they are
// signed ops rather than a local note: a reader has to be able to see that the
// person who deleted the channel was allowed to.
func TestAuditOpsCheckAuthorityAndChangeNoState(t *testing.T) {
	owner := mustID(t)
	nobody := mustID(t)

	before := replayGuildOps(owner.PublicKey(), nil)
	after := replayGuildOps(owner.PublicKey(), []govOp{
		channelOp(owner, 1, "channel_create", "c1", "general"),
		channelOp(owner, 2, "channel_rename", "c1", "chat"),
		channelOp(owner, 3, "channel_delete", "c1", "chat"),
		namedOp(owner, 4, "guild_rename", "Riverside"),
		namedOp(owner, 5, "emoji_add", "parrot"),
	})
	if len(after.Roles) != len(before.Roles) || len(after.Banned) != len(before.Banned) ||
		len(after.Removed) != len(before.Removed) || len(after.SlowMode) != len(before.SlowMode) ||
		after.Owner() != before.Owner() {
		t.Fatal("an audit-only op changed governance state")
	}

	_, verdicts := replayGuildOpsRecording(owner.PublicKey(), []govOp{
		channelOp(owner, 1, "channel_delete", "c1", "general"),
		channelOp(nobody, 2, "channel_delete", "c2", "secrets"),
		namedOp(nobody, 3, "guild_rename", "Mine Now"),
	}, true)
	ok := channelOp(owner, 1, "channel_delete", "c1", "general")
	bad := channelOp(nobody, 2, "channel_delete", "c2", "secrets")
	rename := namedOp(nobody, 3, "guild_rename", "Mine Now")
	if v := verdicts[ok.hash()]; !v.Verified || !v.Applied {
		t.Fatalf("the owner's own channel delete read as refused: %+v", v)
	}
	if v := verdicts[bad.hash()]; !v.Verified || v.Applied {
		t.Fatalf("a channel delete from someone without manage-channels was not marked refused: %+v", v)
	}
	if v := verdicts[rename.hash()]; !v.Verified || v.Applied {
		t.Fatalf("a guild rename from someone without manage-guild was not marked refused: %+v", v)
	}
}
