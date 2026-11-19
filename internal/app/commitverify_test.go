package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/thomas-vilte/mls-go/framing"
)

// commitTap passes everything through to the real engine and notes which
// commits reached CommitSender (the first thing the control topic does with a
// commit) and how many ApplyCommit calls were made. failApply makes
// ApplyCommit fail without touching the group.
type commitTap struct {
	mls.Engine
	mu        sync.Mutex
	seen      map[string]bool
	applies   atomic.Int32
	failApply atomic.Bool
}

func (c *commitTap) CommitSender(ctx context.Context, gid mls.GroupID, commit []byte) ([]byte, error) {
	c.mu.Lock()
	c.seen[string(commit)] = true
	c.mu.Unlock()
	return c.Engine.CommitSender(ctx, gid, commit)
}

func (c *commitTap) ApplyCommit(ctx context.Context, gid mls.GroupID, commit []byte) error {
	c.applies.Add(1)
	if c.failApply.Load() {
		return errors.New("test: apply held")
	}
	return c.Engine.ApplyCommit(ctx, gid, commit)
}

func (c *commitTap) arrived(commit []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[string(commit)]
}

// newTappedSeatSetup is newSeatSetup with a commitTap under each service.
func newTappedSeatSetup(t *testing.T, ctx context.Context) (*seatSetup, map[*Service]*commitTap) {
	t.Helper()
	taps := map[*Service]*commitTap{}
	s := newSeatSetupWith(t, ctx, func() *Service {
		var tap *commitTap
		svc, err := Start(ctx, Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true,
			wrapMLS: func(e mls.Engine) mls.Engine { tap = &commitTap{Engine: e, seen: map[string]bool{}}; return tap }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		taps[svc] = tap
		return svc
	})
	return s, taps
}

// reframeCommit returns a commit with its cleartext sender leaf and epoch set
// to the given values and its signature and membership tag left as they were,
// so it no longer verifies.
func reframeCommit(t *testing.T, commit []byte, sender uint32, epoch uint64) []byte {
	t.Helper()
	msg, err := framing.UnmarshalMLSMessage(commit)
	if err != nil {
		t.Fatal(err)
	}
	pub, ok := msg.AsPublic()
	if !ok {
		t.Fatal("not a public message")
	}
	pub.Content.Sender.LeafIndex = sender
	pub.Content.Epoch = epoch
	return framing.NewMLSMessagePublic(pub).Marshal()
}

func committedCount(s *Service, guildID string) int {
	st := s.seatsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.committed[guildID])
}

// waitArrived waits until the commits have reached the tapped service's
// control-topic handler, then gives the handler a moment to finish with them.
func waitArrived(t *testing.T, tap *commitTap, commits ...[]byte) {
	t.Helper()
	for _, c := range commits {
		waitUntil(t, 20*time.Second, func() bool { return tap.arrived(c) }, "a commit never reached the control topic")
	}
	time.Sleep(2 * time.Second)
}

// A removal commit whose signature does not check out changes nothing: the
// device it names does not record itself removed, keeps its epoch and keeps
// acting, and genuine commits after it still apply.
func TestUnverifiedRemovalChangesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, taps := newTappedSeatSetup(t, ctx)
	holdSeatDefence(t)
	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	modIdx, _ := s.mod.mls.OwnLeaf(ctx, s.gid)
	modLeaf, _, _ := s.member.mls.LeafAt(ctx, s.gid, modIdx)
	ownerLeaf, _, _ := s.member.mls.LeafAt(ctx, s.gid, ownerIdx)
	epoch, _ := s.mod.mls.Epoch(ctx, s.gid)

	// A removal of the mod's leaf framed as the owner's.
	raw, err := s.member.mls.RemoveLeaf(ctx, s.gid, modIdx, modLeaf.Key)
	if err != nil {
		t.Fatal(err)
	}
	bad := reframeCommit(t, raw, ownerIdx, epoch)
	if s.mod.mls.VerifyCommit(ctx, s.gid, bad) == nil {
		t.Fatal("setup: the reframed commit verifies")
	}
	_ = s.member.ps.Publish(ctx, domain.ControlTopicID(s.gid), bad)
	waitArrived(t, taps[s.mod], bad)
	if v := s.mod.EvictedFrom(s.g.ID); v != "" {
		t.Fatalf("the mod recorded itself removed (%q) on a commit that does not verify", v)
	}
	if e, _ := s.mod.mls.Epoch(ctx, s.gid); e != epoch {
		t.Fatalf("the mod's epoch moved: %d -> %d", epoch, e)
	}

	// A removal of the owner's leaf framed as the mod's.
	raw2, err := s.member.mls.RemoveLeaf(ctx, s.gid, ownerIdx, ownerLeaf.Key)
	if err != nil {
		t.Fatal(err)
	}
	bad2 := reframeCommit(t, raw2, modIdx, epoch)
	_ = s.member.ps.Publish(ctx, domain.ControlTopicID(s.gid), bad2)
	waitArrived(t, taps[s.owner], bad2)
	if v := s.owner.EvictedFrom(s.g.ID); v != "" {
		t.Fatalf("the owner recorded itself removed (%q) on a commit that does not verify", v)
	}
	ch, err := s.owner.CreateChannel(s.g.ID, "after", "text", "")
	if err != nil {
		t.Fatalf("the owner can no longer act: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		s.mod.mu.RLock()
		defer s.mod.mu.RUnlock()
		_, ok := s.mod.channelToGuild[ch.ID]
		return ok
	}, "the owner's new channel never reached the mod")

	// A genuine removal still applies.
	if err := s.owner.KickMember(s.g.ID, s.member.Fingerprint(), ""); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return len(leavesWith(s.mod, s.gid, s.member.myCredential)) == 0
	}, "the genuine removal never applied at the mod")
	if v := s.mod.EvictedFrom(s.g.ID); v != "" {
		t.Fatalf("the mod reads a removal of another member as its own: %q", v)
	}
}

// An owner commit replayed under another sender and epoch does not verify,
// so it flags no key and puts nothing on the banner.
func TestUnverifiedReplayFlagsNoKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, taps := newTappedSeatSetup(t, ctx)
	holdSeatDefence(t)
	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	modIdx, _ := s.mod.mls.OwnLeaf(ctx, s.gid)
	ownerLeaf, _, _ := s.member.mls.LeafAt(ctx, s.gid, ownerIdx)

	// An honest add and removal by the owner; the removal is the commit
	// replayed below.
	cred := make([]byte, 32)
	_, _ = rand.Read(cred)
	addLeafWithCredential(t, s.owner, s.gid, cred, freshKey(t))
	waitUntil(t, 20*time.Second, func() bool {
		return len(leavesWith(s.member, s.gid, cred)) == 1 && len(leavesWith(s.mod, s.gid, cred)) == 1
	}, "the add never arrived")
	x := leavesWith(s.owner, s.gid, cred)[0]
	old, err := s.owner.mls.RemoveLeaf(ctx, s.gid, x.Index, x.Key)
	if err != nil {
		t.Fatal(err)
	}
	s.owner.logCommit(s.gid, old)
	_ = s.owner.ps.Publish(ctx, domain.ControlTopicID(s.gid), old)
	waitUntil(t, 20*time.Second, func() bool {
		return len(leavesWith(s.member, s.gid, cred)) == 0 && len(leavesWith(s.mod, s.gid, cred)) == 0
	}, "the removal never arrived")
	for _, svc := range []*Service{s.owner, s.mod, s.member} {
		if f := svc.SeatFlags(s.g.ID); len(f) != 0 {
			t.Fatalf("setup: flags before the replay: %+v", f)
		}
	}

	epoch, _ := s.mod.mls.Epoch(ctx, s.gid)
	bad := reframeCommit(t, old, modIdx, epoch)
	if s.mod.mls.VerifyCommit(ctx, s.gid, bad) == nil {
		t.Fatal("setup: the reframed commit verifies")
	}
	_ = s.member.ps.Publish(ctx, domain.ControlTopicID(s.gid), bad)
	waitArrived(t, taps[s.mod], bad)
	if s.mod.keyFlagged(s.g.ID, ownerLeaf.Key) {
		t.Fatal("the owner's key is flagged on a commit that does not verify")
	}
	if n := committedCount(s.mod, s.g.ID); n != 0 {
		t.Fatalf("a commit that does not verify left %d seat flags", n)
	}
	s.mod.auditSeats(s.g.ID, s.gid)
	if f := s.mod.SeatFlags(s.g.ID); len(f) != 0 {
		t.Fatalf("banner flags after a commit that does not verify: %+v", f)
	}
}

// Commits that do not verify leave nothing in memory, however many arrive.
func TestUnverifiedCommitsKeepNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, taps := newTappedSeatSetup(t, ctx)
	holdSeatDefence(t)
	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	epoch, _ := s.mod.mls.Epoch(ctx, s.gid)
	const n = 20
	var sent [][]byte
	for i := 0; i < n; i++ {
		e, err := mls.New(s.owner.myCredential, freshKey(t))
		if err != nil {
			t.Fatal(err)
		}
		kp, err := e.KeyPackage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		raw, _, _, err := s.member.mls.AddMembers(ctx, s.gid, [][]byte{kp})
		if err != nil {
			t.Fatal(err)
		}
		bad := reframeCommit(t, raw, ownerIdx, epoch)
		sent = append(sent, bad)
		_ = s.member.ps.Publish(ctx, domain.ControlTopicID(s.gid), bad)
	}
	waitArrived(t, taps[s.mod], sent...)
	if c := committedCount(s.mod, s.g.ID); c != 0 {
		t.Fatalf("%d commits that do not verify left %d seat flags", n, c)
	}
	if f := s.mod.SeatFlags(s.g.ID); len(f) != 0 {
		t.Fatalf("banner flags after commits that do not verify: %+v", f)
	}
}

// A commit's seat flags are kept once it applies, not before: a genuine
// commit that seats a second leaf under the owner's account leaves no flag at
// a member whose apply fails, and is flagged, naming the mod as adder, where
// it applies.
func TestCommitSeatFlagsKeptOnlyOnceApplied(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, taps := newTappedSeatSetup(t, ctx)
	holdSeatDefence(t)
	tap := taps[s.member]
	tap.failApply.Store(true)
	before := tap.applies.Load()
	key := freshKey(t)
	addLeafWithCredential(t, s.mod, s.gid, s.owner.myCredential, key)
	waitUntil(t, 20*time.Second, func() bool { return tap.applies.Load() > before }, "the commit never reached the member")
	time.Sleep(time.Second)
	if c := committedCount(s.member, s.g.ID); c != 0 {
		t.Fatalf("a commit that did not apply left %d seat flags", c)
	}
	tap.failApply.Store(false)

	pub := key.Public().(ed25519.PublicKey)
	waitUntil(t, 20*time.Second, func() bool {
		st := s.owner.seatsState()
		st.mu.Lock()
		defer st.mu.Unlock()
		f, ok := st.committed[s.g.ID][hex.EncodeToString(pub)]
		return ok && f.Adder == s.mod.Fingerprint()
	}, "the owner, which applied the commit, kept no flag naming the mod")
}

// History sync applies the same check: a served commit that does not verify
// changes nothing, and a genuine one served after it applies and keeps its
// seat flags.
func TestSyncedCommitVerifiedBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newSeatSetup(t, ctx)
	holdSeatDefence(t)
	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	modIdx, _ := s.mod.mls.OwnLeaf(ctx, s.gid)
	modLeaf, _, _ := s.member.mls.LeafAt(ctx, s.gid, modIdx)
	epoch, _ := s.mod.mls.Epoch(ctx, s.gid)

	serve := func(commits ...[]byte) {
		served := false
		reply := syncAskFunc(func(ctx context.Context, to peer.ID, req []byte) ([]byte, error) {
			if served {
				return json.Marshal(syncResponse{Epoch: epoch + 1})
			}
			served = true
			return json.Marshal(syncResponse{Commits: commits, Epoch: epoch + 1})
		})
		s.mod.requestSync.Store(&reply)
		defer s.mod.requestSync.Store(nil)
		_ = s.mod.syncGuildFromPeerOnce(s.g.ID, s.owner.host.PeerID())
	}

	raw, err := s.member.mls.RemoveLeaf(ctx, s.gid, modIdx, modLeaf.Key)
	if err != nil {
		t.Fatal(err)
	}
	serve(reframeCommit(t, raw, ownerIdx, epoch))
	if v := s.mod.EvictedFrom(s.g.ID); v != "" {
		t.Fatalf("a synced commit that does not verify recorded a removal: %q", v)
	}
	if e, _ := s.mod.mls.Epoch(ctx, s.gid); e != epoch {
		t.Fatalf("the mod's epoch moved: %d -> %d", epoch, e)
	}

	// A genuine owner commit, held back from the control topic so that sync
	// is the only way it arrives.
	key := freshKey(t)
	e, err := mls.New(s.owner.myCredential, key)
	if err != nil {
		t.Fatal(err)
	}
	kp, err := e.KeyPackage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	good, _, _, err := s.owner.mls.AddMembers(ctx, s.gid, [][]byte{kp})
	if err != nil {
		t.Fatal(err)
	}
	serve(good)
	if e, _ := s.mod.mls.Epoch(ctx, s.gid); e != epoch+1 {
		t.Fatalf("the genuine commit did not apply: epoch %d, want %d", e, epoch+1)
	}
	pub := key.Public().(ed25519.PublicKey)
	if _, ok := flagFor(s.mod, s.g.ID, pub); !ok {
		t.Fatalf("the genuine commit's seat flag is missing: %+v", s.mod.SeatFlags(s.g.ID))
	}
	if c := committedCount(s.mod, s.g.ID); c == 0 {
		t.Fatal("the genuine commit kept no seat flag")
	}
}
