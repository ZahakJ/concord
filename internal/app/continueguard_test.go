package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A guild owner whose device is restored from a backup taken before it last
// admitted someone wakes at a dead epoch while its members stand ahead. When it
// is the guild's only possible committer, nobody can re-admit it, and the
// banner's "Continue from here" used to drop the fork evidence and carry on: the
// device then admitted and kicked people on a private copy that reached the
// same epoch numbers and never flagged itself again.
func TestOnlyCommitterBehindCannotContinue(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	t.Run("sole committer is refused", func(t *testing.T) { runContinueGuard(t, guardSole) })
	t.Run("another committer exists", func(t *testing.T) { runContinueGuard(t, guardOtherCommitter) })
	t.Run("a committer who left does not count", func(t *testing.T) { runContinueGuard(t, guardCommitterLeft) })
	t.Run("the owner's other device can repair", func(t *testing.T) { runContinueGuard(t, guardLinkedDevice) })
}

type guardCase int

const (
	guardSole guardCase = iota
	guardOtherCommitter
	guardCommitterLeft
	guardLinkedDevice
)

func runContinueGuard(t *testing.T, c guardCase) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	start := func(dir string) *Service {
		t.Helper()
		svc, err := Start(ctx, Config{
			DataDir: dir, Passphrase: "test-pass", DisableMDNS: true,
			BootstrapPeers: []string{boot},
			listenAddrs:    []string{"/ip4/127.0.0.1/tcp/0", "/ip4/127.0.0.1/udp/0/quic-v1"},
		})
		if err != nil {
			t.Fatalf("start %s: %v", dir, err)
		}
		return svc
	}
	uDir, pDir, qDir, tDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	u, p := start(uDir), start(pDir)
	var q, r, tab *Service
	defer func() {
		for _, s := range []*Service{u, p, q, r, tab} {
			if s != nil {
				_ = s.Close()
			}
		}
	}()
	members := 2
	if c == guardLinkedDevice {
		// U's tablet, linked into U's account before the guild exists.
		code, err := u.LinkOffer()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := RedeemLink(ctx, tDir, code, "test-pass", ""); err != nil {
			t.Fatalf("link U's tablet: %v", err)
		}
		tab = start(tDir)
		members = 3
	}

	g, err := u.CreateGuild("guarded")
	if err != nil {
		t.Fatal(err)
	}
	code, err := u.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.JoinViaInvite(code); err != nil {
		t.Fatalf("P join: %v", err)
	}
	waitMemberCount(t, 90*time.Second, g.ID, members, u, p)
	if c == guardLinkedDevice {
		// The tablet is offline when U comes back, so it re-admits U only when
		// the test brings it back.
		_ = tab.Close()
		tab = nil
	}
	if c == guardOtherCommitter || c == guardCommitterLeft {
		// R holds manage-members. R is offline when U comes back, so nobody
		// re-admits U before the test asks.
		r = start(t.TempDir())
		code, err := u.InviteCode(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.JoinViaInvite(code); err != nil {
			t.Fatalf("R join: %v", err)
		}
		waitMemberCount(t, 90*time.Second, g.ID, 3, u, p, r)
		rid, err := u.UpsertRole(g.ID, "", "Mod", "", PermManageMembers, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := u.AssignRole(g.ID, r.Fingerprint(), rid, true); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 60*time.Second, func() bool {
			return u.memberHasPerm(g.ID, r.Fingerprint(), PermManageMembers)
		}, "the manage-members grant never landed")
		if c == guardCommitterLeft {
			if err := r.LeaveGuild(g.ID); err != nil {
				t.Fatal(err)
			}
			waitMemberCount(t, 90*time.Second, g.ID, 2, u, p)
		} else {
			members = 3
		}
		_ = r.Close()
		r = nil
	}

	// The backup: U's data dir as it stood before it admitted Q.
	_ = u.Close()
	u = nil
	snap := t.TempDir()
	copyDataDir(t, uDir, snap)
	u = start(uDir)

	q = start(qDir)
	code, err = u.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.JoinViaInvite(code); err != nil {
		t.Fatalf("Q join: %v", err)
	}
	waitMemberCount(t, 90*time.Second, g.ID, members+1, u, p, q)

	// The restore.
	_ = u.Close()
	u = nil
	if err := os.RemoveAll(uDir); err != nil {
		t.Fatal(err)
	}
	copyDataDir(t, snap, uDir)
	u = start(uDir)
	waitFor(t, 120*time.Second, func() bool {
		return u.OutOfSync(g.ID) && len(u.forkedWith(g.ID)) > 0
	}, "the restored owner never saw its members ahead of it")

	switch c {
	case guardOtherCommitter:
		// R could re-admit U: today's behaviour stands.
		if err := u.ResolveStranding(g.ID); err != nil {
			t.Fatalf("ResolveStranding with another committer in the guild: %v", err)
		}
		if u.OutOfSync(g.ID) || u.CannotRepairHere(g.ID) {
			t.Fatal("Continue from here left the guild flagged although another committer exists")
		}
		return
	case guardLinkedDevice:
		if !u.behindLatched(g.ID) {
			t.Fatal("U saw its members ahead but did not latch it")
		}
		if u.CannotRepairHere(g.ID) {
			t.Fatal("the view says nothing can repair U although U's own tablet can re-admit it")
		}
		// The tablet comes back and re-admits U: the copy is genuinely repaired.
		tab = start(tDir)
		waitFor(t, 120*time.Second, func() bool { return !u.OutOfSync(g.ID) }, "U's tablet never re-admitted it")
		assertBehindCleared(t, u, g.ID)
		return
	}

	refused := func(when string) {
		t.Helper()
		if err := u.ResolveStranding(g.ID); !errors.Is(err, ErrBehindOnlyCommitter) {
			t.Fatalf("%s: ResolveStranding on a sole committer's dead copy: err=%v, want ErrBehindOnlyCommitter", when, err)
		}
		if !u.OutOfSync(g.ID) {
			t.Fatalf("%s: the refused Continue cleared the flag", when)
		}
		if !u.CannotRepairHere(g.ID) {
			t.Fatalf("%s: the guild's view does not say the copy cannot be repaired here", when)
		}
	}
	refused("with members online")
	if c != guardSole {
		return
	}
	// The members who are ahead close the app. The recovery pass drops the
	// fork evidence against peers that went away; the refusal must not go with it.
	_ = p.Close()
	p = nil
	_ = q.Close()
	q = nil
	waitFor(t, 30*time.Second, func() bool { return len(u.host.Peers()) <= 1 }, "the members never disconnected")
	u.recoverOutOfSync(g.ID)
	refused("after the members went offline")
	// And a restart keeps it.
	_ = u.Close()
	u = nil
	u = start(uDir)
	refused("after a restart")
}

// copyDataDir copies a stopped device's data directory, the way restoring a
// backup does.
func copyDataDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o700)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(filepath.Join(dst, rel), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
}

// assertBehindCleared checks that a device that genuinely caught up no longer
// carries the "seen behind" verdict.
func assertBehindCleared(t *testing.T, s *Service, guildID string) {
	t.Helper()
	waitFor(t, 30*time.Second, func() bool { return !s.behindLatched(guildID) }, "a device that caught up still carries the seen-behind latch")
	if v, _ := s.store.GetSetting(behindSeenPrefix + guildID); v != "" {
		t.Fatalf("the persisted seen-behind latch survived the catch-up: %q", v)
	}
	if s.CannotRepairHere(guildID) {
		t.Fatal("a device that caught up still says it cannot be repaired")
	}
}
