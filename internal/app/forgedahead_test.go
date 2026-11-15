package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

// NR-01. A seated member that is not a committer answers the owner's sync with
// a made-up claim to be far ahead. Neither form of the claim may freeze the
// owner's copy:
//   - an EpochGap with an inflated epoch (no commit can come with it);
//   - an unreadable payload at an inflated epoch, served after a replayed,
//     genuine commit of the owner's from an earlier epoch, so that the owner's
//     own leaf is named as its author.
//
// Each is still recorded as a claim (the copy does not call itself alone, and
// a heal may ask that peer), and a readable answer drops the claim.
//
// Ported from the reviewer's probe (notesheal-review.md, NR-01).
func TestForgedAheadDoesNotFreezeOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, gid, ch := guardedPair(t, ctx, boot)
	groupID := rhGroup(u, gid)
	cur, err := u.mls.Epoch(u.ctx, groupID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := u.store.CommitsAfter(groupID, 0)
	if err != nil || len(rows) == 0 {
		t.Fatalf("setup: the owner logged no commit of its own to replay (%v)", err)
	}
	replayed := rows[0].Commit
	if !u.commitAuthorized(gid, groupID, replayed) {
		t.Fatal("setup: the replayed commit does not name an authorized committer")
	}

	forgeries := map[string]syncResponse{
		"an EpochGap far ahead": {EpochGap: true, Epoch: cur + 1000},
		"an unreadable payload far ahead, after a replayed owner commit": {
			Commits: [][]byte{replayed}, Epoch: cur + 1000, Payload: []byte("not a ciphertext"),
		},
	}
	for name, forged := range forgeries {
		lie := syncAskFunc(func(ctx context.Context, to peer.ID, req []byte) ([]byte, error) {
			return json.Marshal(forged)
		})
		u.requestSync.Store(&lie)
		_ = u.syncGuildFromPeerOnce(gid, p.host.PeerID())
		u.requestSync.Store(nil)

		if !u.behindLatched(gid) {
			t.Errorf("%s: not even recorded as a claim", name)
		}
		if u.behindProven(gid) || u.CannotRepairHere(gid) {
			t.Errorf("%s: the owner's copy is barred on a member's word", name)
		}
		if v, _ := u.store.GetSetting(behindSeenPrefix + gid); v != "" {
			t.Errorf("%s: a claim was persisted as the latch: %q", name, v)
		}
		if _, err := u.SendMessage(ch, "after "+name, "", ""); err != nil {
			t.Errorf("%s: the owner's send was refused: %v", name, err)
		}
		if _, err := u.UpsertRole(gid, "", "after "+name, "", 0, 1); err != nil {
			t.Errorf("%s: the owner's governance op was refused: %v", name, err)
		}
		if err := u.ResolveStranding(gid); errors.Is(err, ErrBehindOnlyCommitter) {
			t.Errorf("%s: Continue was refused: %v", name, err)
		}

		// Claim again and let the same peer answer honestly: the claim goes.
		u.noteBehindClaim(gid, cur+1000)
		if err := u.syncGuildFromPeerOnce(gid, p.host.PeerID()); err != nil {
			t.Fatalf("%s: honest sync: %v", name, err)
		}
		if u.behindLatched(gid) {
			t.Errorf("%s: a readable answer left the unproven claim standing", name)
		}
	}
	if err := u.KickMember(gid, p.Fingerprint(), ""); err != nil {
		t.Errorf("the owner cannot kick the forger: %v", err)
	}
}

// An unproven claim is still a latch for what it is for: it keeps a copy that
// is alone by its own tree from resolving by itself and from calling itself
// alone (the Notes case of a backup that predates the phone's leaf), and it
// does not survive a restart.
func TestClaimHoldsRecoveryButNotARestart(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	u := startServiceInDir(t, ctx, dir)
	g, err := u.CreateGuild("Only me here")
	if err != nil {
		t.Fatal(err)
	}
	cur, err := u.mls.Epoch(u.ctx, rhGroup(u, g.ID))
	if err != nil {
		t.Fatal(err)
	}
	u.noteBehindClaim(g.ID, cur+1)
	u.setOutOfSync(g.ID, true)
	u.recoverOutOfSync(g.ID)
	if !u.OutOfSync(g.ID) || !u.behindLatched(g.ID) {
		t.Fatal("a claimed-behind copy, alone by its own tree, resolved by itself")
	}
	if u.AloneInGuild(g.ID) {
		t.Fatal("the view calls a claimed-behind copy alone")
	}
	_ = u.Close()
	u = startServiceInDir(t, ctx, dir)
	if u.behindLatched(g.ID) {
		t.Fatal("an unproven claim survived a restart")
	}
}
