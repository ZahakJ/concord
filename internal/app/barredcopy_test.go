package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

// SG-21. A copy that CannotRepairHere issues no governance op of any type: its
// Seq comes from a log that stopped at the backup, and a signed op stays valid
// for good. Before the bar sat in issueGovOp, only ban, kick and readmit asked.
// The same calls go through once the copy is healthy.
func TestBarredCopyIssuesNoGovernanceOp(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, gid, ch := guardedPair(t, ctx, boot)
	pf := p.Fingerprint()

	rid, err := u.UpsertRole(gid, "", "Greeter", "", PermMuteMembers, 1)
	if err != nil {
		t.Fatal(err)
	}
	// A sample of the ops that were open on a barred copy. None of them
	// grants manage-members or names an heir that stays named, either of
	// which would make the copy repairable and end the test's premise.
	ops := []struct {
		name string
		do   func() error
	}{
		{"role create", func() error { _, err := u.UpsertRole(gid, "", "Another", "", 0, 2); return err }},
		{"role assign", func() error { return u.AssignRole(gid, pf, rid, true) }},
		{"role delete", func() error { return u.DeleteRole(gid, rid) }},
		{"mute", func() error { return u.MuteMember(gid, pf, 5, "") }},
		{"unmute", func() error { return u.UnmuteMember(gid, pf) }},
		{"retention", func() error { return u.SetRetention(gid, ch, 0) }},
		{"slow mode", func() error { return u.SetSlowMode(gid, ch, 0) }},
		{"channel create", func() error { _, err := u.CreateChannel(gid, "later", "text", ""); return err }},
		{"channel rename", func() error { return u.RenameChannel(gid, ch, "renamed") }},
		{"guild rename", func() error { return u.RenameGuild(gid, "Renamed guild") }},
		{"unban", func() error { return u.UnbanMember(gid, pf) }},
		{"set heir", func() error { return u.SetHeir(gid, pf) }},
		{"clear heir", func() error { return u.ClearHeir(gid) }},
	}

	latch(t, u, gid)
	seq := u.nextGovSeq(gid)
	for _, op := range ops {
		if err := op.do(); !errors.Is(err, ErrBehindOnlyCommitter) {
			t.Errorf("%s on a barred copy: err=%v, want ErrBehindOnlyCommitter", op.name, err)
		}
	}
	if err := u.TransferOwnership(gid, pf); !errors.Is(err, ErrBehindOnlyCommitter) {
		t.Errorf("transfer_owner on a barred copy: err=%v, want ErrBehindOnlyCommitter", err)
	}
	if got := u.nextGovSeq(gid); got != seq {
		t.Errorf("a barred copy recorded governance ops: next seq %d, was %d", got, seq)
	}
	if n := len(guildChannels(t, u, gid)); n != 1 {
		t.Errorf("a refused channel create left %d channels", n)
	}

	u.clearBehind(gid)
	if u.CannotRepairHere(gid) {
		t.Fatal("setup: the copy still reads as barred after the latch was cleared")
	}
	for _, op := range ops {
		if err := op.do(); err != nil {
			t.Errorf("%s on a healthy copy: %v", op.name, err)
		}
	}
	if got := u.nextGovSeq(gid); got <= seq {
		t.Errorf("a healthy copy recorded no governance op: next seq %d, was %d", got, seq)
	}
	if err := u.TransferOwnership(gid, pf); err != nil {
		t.Errorf("transfer_owner on a healthy copy: %v", err)
	}
	// What the healthy copy signed reaches the member.
	waitUntil(t, 30*time.Second, func() bool { return p.IsGuildOwner(gid, pf) },
		"the healthy copy's transfer never reached P")
}
