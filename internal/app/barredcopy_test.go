package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
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

// SG-20. A copy that CannotRepairHere sends nothing: it encrypts to the tree it
// was restored to, which can still seat a member removed since. Edits and
// reactions ride the same path and are refused with it. The error is the one
// the frontend reads as the banner's sentence (lib/errors.js).
func TestBarredCopySendsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, gid, ch := guardedPair(t, ctx, boot)

	if _, err := u.SendMessage(ch, "before the latch", "", ""); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return hasMessage(p, ch, "before the latch") },
		"setup: P never received U's message")
	target := firstMessageID(t, u, ch)

	latch(t, u, gid)
	if _, err := u.SendMessage(ch, "from the dead copy", "", ""); !errors.Is(err, ErrBehindOnlyCommitter) {
		t.Errorf("SendMessage on a barred copy: err=%v, want ErrBehindOnlyCommitter", err)
	}
	if err := u.ToggleReaction(ch, target, "👍"); !errors.Is(err, ErrBehindOnlyCommitter) {
		t.Errorf("a reaction from a barred copy: err=%v, want ErrBehindOnlyCommitter", err)
	}
	if err := u.EditMessage(ch, target, "edited on the dead copy", ""); !errors.Is(err, ErrBehindOnlyCommitter) {
		t.Errorf("an edit from a barred copy: err=%v, want ErrBehindOnlyCommitter", err)
	}
	time.Sleep(3 * time.Second)
	for name, s := range map[string]*Service{"U": u, "P": p} {
		if hasMessage(s, ch, "from the dead copy") || hasMessage(s, ch, "edited on the dead copy") {
			t.Errorf("%s holds what the barred copy tried to send", name)
		}
	}

	u.clearBehind(gid)
	if _, err := u.SendMessage(ch, "from the healthy copy", "", ""); err != nil {
		t.Fatalf("SendMessage on a healthy copy: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool { return hasMessage(p, ch, "from the healthy copy") },
		"a healthy copy's message never reached P")
}

func firstMessageID(t *testing.T, s *Service, ch string) string {
	t.Helper()
	msgs, _ := s.Messages(ch, 500)
	for _, m := range msgs {
		if m.Kind == "" {
			return m.ID
		}
	}
	t.Fatal("no message to react to")
	return ""
}

// NR-02. The guild-meta lane carries content too, and none of it leaves a copy
// that CannotRepairHere: a sample of events, nicknames and a forum post. The
// post is refused with the sentence rather than announced without its body.
// The member's own syncs are held off while the copy is barred, so that what
// it sees can only have come over the meta lane.
func TestBarredCopyPublishesNoMeta(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, gid, _ := guardedPair(t, ctx, boot)
	forum, err := u.CreateChannel(gid, "board", "forum", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return allChannels(p, gid) == 2 },
		"setup: the forum never reached P")
	start := time.Now().Add(24 * time.Hour).Unix()

	held := syncAskFunc(func(context.Context, peer.ID, []byte) ([]byte, error) {
		return nil, errors.New("held off by the test")
	})
	p.requestSync.Store(&held)
	latch(t, u, gid)
	if _, err := u.CreateThread(gid, forum.ID, "a post from the dead copy", "body", nil); !errors.Is(err, ErrBehindOnlyCommitter) {
		t.Errorf("CreateThread on a barred copy: err=%v, want ErrBehindOnlyCommitter", err)
	}
	if n := allChannels(u, gid); n != 2 {
		t.Errorf("a refused post left %d channels on the barred copy", n)
	}
	_, _ = u.CreateEvent(gid, "dead-copy event", "", start, start+3600, "", "", "", 0)
	_ = u.SetNickname(gid, "dead-copy nick")
	time.Sleep(4 * time.Second)
	if evs, _ := p.Events(gid); len(evs) != 0 {
		t.Errorf("an event left the barred copy: %v", evs)
	}
	if n := p.NickOf(gid, u.Fingerprint()); n != "" {
		t.Errorf("a nickname left the barred copy: %q", n)
	}
	if n := allChannels(p, gid); n != 2 {
		t.Errorf("a post left the barred copy: P has %d channels", n)
	}

	// The same lanes from a healthy copy reach P.
	u.clearBehind(gid)
	p.requestSync.Store(nil)
	if _, err := u.CreateEvent(gid, "healthy event", "", start, start+3600, "", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if err := u.SetNickname(gid, "healthy nick"); err != nil {
		t.Fatal(err)
	}
	if _, err := u.CreateThread(gid, forum.ID, "a post from the healthy copy", "body", nil); err != nil {
		t.Fatalf("CreateThread on a healthy copy: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		evs, _ := p.Events(gid)
		for _, e := range evs {
			if e.Title == "healthy event" {
				return p.NickOf(gid, u.Fingerprint()) == "healthy nick" && allChannels(p, gid) == 3
			}
		}
		return false
	}, "the healthy copy's event, nickname or post never reached P")
}

// allChannels counts every channel of a guild on s, threads included.
func allChannels(s *Service, gid string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if g, ok := s.guilds[gid]; ok {
		return len(g.Channels)
	}
	return 0
}
