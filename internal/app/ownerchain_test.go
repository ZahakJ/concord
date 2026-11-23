package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/store"
	"github.com/libp2p/go-libp2p/core/peer"
)

// offlineRemoval has the mod commit c1 (an add of a stranger) and c2 (the
// removal of the owner's leaf), and hands both to the member directly. Nothing
// is published, so the owner, as if it had been offline, never meets either:
// both members stand two epochs past it and no longer seat it.
func offlineRemoval(t *testing.T, ctx context.Context, s *seatSetup) (c1, c2 []byte, epoch uint64) {
	t.Helper()
	epoch, _ = s.owner.mls.Epoch(ctx, s.gid)
	key := freshKey(t)
	e, err := mls.New([]byte(key.Public().(ed25519.PublicKey)), key)
	if err != nil {
		t.Fatal(err)
	}
	kp, err := e.KeyPackage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c1, _, _, err = s.mod.mls.AddMembers(ctx, s.gid, [][]byte{kp})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	s.mod.logCommit(s.gid, c1)
	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	c2, err = s.mod.mls.RemoveLeaf(ctx, s.gid, ownerIdx, s.owner.ownAccountSigningKey())
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	s.mod.logCommit(s.gid, c2)
	s.mod.forgetMemberSet(s.g.ID)
	for _, c := range [][]byte{c1, c2} {
		if err := s.member.mls.ApplyCommit(ctx, s.gid, c); err != nil {
			t.Fatalf("member apply: %v", err)
		}
		s.member.logCommit(s.gid, c)
	}
	s.member.forgetMemberSet(s.g.ID)
	for _, m := range []*Service{s.mod, s.member} {
		if m.guildHasMember(s.g.ID, s.owner.Fingerprint()) {
			t.Fatal("setup: a member still seats the owner")
		}
		if e, _ := m.mls.Epoch(ctx, s.gid); e != epoch+2 {
			t.Fatalf("setup: a member is at epoch %d, want %d", e, epoch+2)
		}
	}
	return c1, c2, epoch
}

// askChain puts a catch-up to `to` as the peer `from`, straight into its
// handler, and returns the answer (zero for the empty body).
func askChain(t *testing.T, to *Service, from peer.ID, guildID string, epoch uint64, ownerChain bool) syncResponse {
	t.Helper()
	req, _ := json.Marshal(syncRequest{GuildID: guildID, Epoch: epoch, OwnerChain: ownerChain})
	raw, err := to.handleSyncRequest(to.ctx, from, req)
	if err != nil {
		t.Fatal(err)
	}
	var resp syncResponse
	if len(raw) == 0 {
		return resp
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Epoch == 0 {
		t.Fatal("a served answer carries the responder's epoch")
	}
	return resp
}

// holdCatchUps declines every catch-up the owner sends until the returned
// release is called. The owner's own background catch-up (the heal tick)
// would otherwise be served the chain and heal before a test looks.
func holdCatchUps(svc *Service) (release func()) {
	var decline syncAskFunc = func(context.Context, peer.ID, []byte) ([]byte, error) { return []byte{}, nil }
	svc.requestSync.Store(&decline)
	return func() { svc.requestSync.Store(nil) }
}

// declined reports whether a sync answer is the empty body.
func declined(r syncResponse) bool {
	return r.Epoch == 0 && len(r.Commits) == 0 && !r.EpochGap && len(r.Payload) == 0
}

// The known limit of 0.61, closed: an owner's device that was away when a
// member removed it is served the commits by a member that no longer seats it,
// reads its own removal in them, and is let back in, with no hand on it.
func TestOfflineOwnerRejoinsFromTheChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	release := holdCatchUps(s.owner)
	_, _, epoch := offlineRemoval(t, ctx, s)

	// What every build before this one did, and what this one still does for
	// a requester that does not ask for the chain: decline.
	if r := askChain(t, s.mod, s.owner.host.PeerID(), s.g.ID, epoch, false); !declined(r) {
		t.Fatalf("a request without OwnerChain was answered: %+v", r)
	}

	// The owner's ordinary catch-up, the one the beat runs.
	release()
	s.owner.syncGuildFromAnyPeer(s.g.ID)
	if !ownerPoll(30*time.Second, func() bool { return s.owner.takenOutLatched(s.g.ID) }) {
		oe, _ := s.owner.mls.Epoch(ctx, s.gid)
		t.Fatalf("the owner never read its removal (epoch %d, want past %d)", oe, epoch)
	}
	notices := s.owner.SeatNotices(s.g.ID)
	if len(notices) == 0 || notices[len(notices)-1].Kind != "owner_removed" || notices[len(notices)-1].Adder != s.mod.Fingerprint() {
		t.Fatalf("notices: %+v, want owner_removed by the mod", notices)
	}
	if !ownerPoll(90*time.Second, func() bool {
		oe, _ := s.owner.mls.Epoch(ctx, s.gid)
		me, _ := s.mod.mls.Epoch(ctx, s.gid)
		return oe == me && oe > epoch+2 && len(leavesWith(s.mod, s.gid, s.owner.myCredential)) == 1 &&
			!s.owner.OutOfSync(s.g.ID)
	}) {
		t.Fatalf("the owner was not let back in (EvictedFrom=%q, latched=%v)", s.owner.EvictedFrom(s.g.ID), s.owner.takenOutLatched(s.g.ID))
	}
	if s.owner.takenOutLatched(s.g.ID) || s.owner.EvictedFrom(s.g.ID) != "" {
		t.Fatalf("after the heal: latched=%v evicted=%q", s.owner.takenOutLatched(s.g.ID), s.owner.EvictedFrom(s.g.ID))
	}
	if !ownerPoll(20*time.Second, func() bool {
		_, err := s.owner.CreateChannel(s.g.ID, "back", "text", "")
		return err == nil
	}) {
		t.Fatal("the owner cannot create a channel after rejoining")
	}
	if !ownerPoll(20*time.Second, func() bool { return guildHasChannel(s.member, s.g.ID, "back") }) {
		t.Fatal("the member never saw the owner's channel")
	}
}

// Who is served, and what. Only the guild's owner, asking for it, behind this
// device's epoch, and not again inside the window unless it paged forward; a
// stranger and a removed member that is not the owner are declined as before,
// and no answer carries a payload.
func TestOwnerChainResponderRules(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	defer holdCatchUps(s.owner)()
	c1, c2, epoch := offlineRemoval(t, ctx, s)

	stranger := startService(t, ctx)
	if r := askChain(t, s.mod, stranger.host.PeerID(), s.g.ID, epoch, true); !declined(r) {
		t.Fatalf("a stranger asking for the chain was answered: %+v", r)
	}

	op := s.owner.host.PeerID()
	// Not behind: nothing to serve.
	if r := askChain(t, s.mod, op, s.g.ID, epoch+2, true); !declined(r) {
		t.Fatalf("an owner level with the responder was answered: %+v", r)
	}
	// Behind the log's reach (the mod joined after epoch 0, so it holds no
	// commit from there): a gap, said as one, with nothing else.
	r := askChain(t, s.mod, op, s.g.ID, 0, true)
	if !r.EpochGap || len(r.Commits) != 0 || len(r.Payload) != 0 {
		t.Fatalf("an unbridgeable owner: %+v, want EpochGap alone", r)
	}
	// The chain itself: both commits, in order, and no payload.
	r = askChain(t, s.mod, op, s.g.ID, epoch, true)
	if len(r.Commits) != 2 || !bytes.Equal(r.Commits[0], c1) || !bytes.Equal(r.Commits[1], c2) || len(r.Payload) != 0 || r.EpochGap || r.Epoch != epoch+2 {
		t.Fatalf("the owner's chain: %d commits, payload %d bytes, gap %v, epoch %d", len(r.Commits), len(r.Payload), r.EpochGap, r.Epoch)
	}
	// The same page again at once: declined; and from one epoch into it.
	if r := askChain(t, s.mod, op, s.g.ID, epoch, true); !declined(r) {
		t.Fatalf("the same page was served twice inside the window: %+v", r)
	}
	if r := askChain(t, s.mod, op, s.g.ID, epoch+1, true); !declined(r) {
		t.Fatalf("a request inside the last page was served inside the window: %+v", r)
	}
	// Past the window, the same page is served again.
	s.mod.mu.Lock()
	for k, v := range s.mod.ownerChainServed {
		v.at = v.at.Add(-ownerChainWindow)
		s.mod.ownerChainServed[k] = v
	}
	s.mod.mu.Unlock()
	if r := askChain(t, s.mod, op, s.g.ID, epoch+1, true); len(r.Commits) != 1 || !bytes.Equal(r.Commits[0], c2) {
		t.Fatalf("after the window, from epoch+1: %d commits, want c2 alone", len(r.Commits))
	}
}

// A hostile member answering the owner's catch-up. Whatever it sends, the
// owner acts only on commits that verify against its own tree at its own epoch
// and come from a member allowed to commit them: a forged, cut, reordered,
// replayed or foreign commit moves nothing, a chain cut short moves the owner
// along the honest prefix and no further, and a removal replayed after the
// owner is back in does not take it out again.
func TestOwnerChainFromAHostilePeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	holdCatchUps(s.owner)
	// A commit the owner has already applied, from its own log, for the replay.
	rows, _ := s.owner.store.CommitsAfter(s.gid, 0)
	if len(rows) == 0 {
		t.Fatal("setup: the owner's commit log is empty")
	}
	old := rows[len(rows)-1].Commit
	c1, c2, epoch := offlineRemoval(t, ctx, s)

	// A commit from a group that is not this one.
	other := startService(t, ctx)
	if _, err := other.CreateGuild("elsewhere"); err != nil {
		t.Fatal(err)
	}
	ogid := theGuild(t, other).GroupID
	key := freshKey(t)
	e, _ := mls.New([]byte(key.Public().(ed25519.PublicKey)), key)
	kp, _ := e.KeyPackage(ctx)
	foreign, _, _, err := other.mls.AddMembers(ctx, ogid, [][]byte{kp})
	if err != nil {
		t.Fatal(err)
	}

	var serve atomic.Pointer[[][]byte]
	var hostile syncAskFunc = func(ctx context.Context, to peer.ID, req []byte) ([]byte, error) {
		return json.Marshal(syncResponse{Commits: *serve.Load(), Epoch: epoch + 2})
	}
	s.owner.requestSync.Store(&hostile)
	defer s.owner.requestSync.Store(nil)
	// The door is held while the hostile peer answers: a chain the owner
	// cannot use, from a peer that claims to be ahead, raises the flag (as
	// EpochGap does), and the heal would otherwise re-add the owner before
	// the test looks at what the chain itself moved.
	var noDoor inviteAskFunc = func(context.Context, peer.AddrInfo, []byte) ([]byte, error) {
		return nil, fmt.Errorf("door held by the test")
	}
	s.owner.requestInvite.Store(&noDoor)
	mod := s.mod.host.PeerID()

	flip := func(b []byte, at int) []byte {
		c := append([]byte(nil), b...)
		c[at] ^= 0x01
		return c
	}
	nothingMoved := func(name string, want uint64) {
		t.Helper()
		if s.owner.takenOutLatched(s.g.ID) {
			t.Fatalf("%s: the owner latched itself taken out", name)
		}
		if v := s.owner.EvictedFrom(s.g.ID); v != "" {
			t.Fatalf("%s: the owner recorded itself removed (%q)", name, v)
		}
		if oe, _ := s.owner.mls.Epoch(ctx, s.gid); oe != want {
			t.Fatalf("%s: the owner moved to epoch %d, want %d", name, oe, want)
		}
		for _, n := range s.owner.SeatNotices(s.g.ID) {
			if n.Kind == "owner_removed" {
				t.Fatalf("%s: the owner was told it was removed", name)
			}
		}
	}
	for _, tc := range []struct {
		name  string
		chain [][]byte
	}{
		{"signature byte flipped", [][]byte{flip(c1, len(c1)-8), c2}},
		{"framing byte flipped", [][]byte{flip(c1, 4), c2}},
		{"removal cut in half", [][]byte{c2[:len(c2)/2]}},
		{"removal before its epoch", [][]byte{c2}},
		{"reordered", [][]byte{c2, c1}},
		{"replayed from the owner's own past", [][]byte{old, c1, c2}},
		{"from another group", [][]byte{foreign}},
		{"empty", nil},
	} {
		chain := tc.chain
		serve.Store(&chain)
		if err := s.owner.syncGuildFromPeer(s.g.ID, mod); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		nothingMoved(tc.name, epoch)
	}

	// Cut short of the removal: the honest prefix applies, and nothing more.
	prefix := [][]byte{c1}
	serve.Store(&prefix)
	if err := s.owner.syncGuildFromPeer(s.g.ID, mod); err != nil {
		t.Fatal(err)
	}
	nothingMoved("truncated chain", epoch+1)
	// The removal with a flipped byte, at the epoch it belongs to.
	bad := [][]byte{flip(c2, len(c2)-8)}
	serve.Store(&bad)
	if err := s.owner.syncGuildFromPeer(s.g.ID, mod); err != nil {
		t.Fatal(err)
	}
	nothingMoved("forged removal at its epoch", epoch+1)

	// The honest member answers: the removal is read, and the owner heals.
	s.owner.requestInvite.Store(nil)
	s.owner.requestSync.Store(nil)
	s.owner.syncGuildFromAnyPeer(s.g.ID)
	if !ownerPoll(30*time.Second, func() bool { return s.owner.takenOutLatched(s.g.ID) }) {
		t.Fatal("the honest chain did not reach the owner")
	}
	if !ownerPoll(90*time.Second, func() bool {
		oe, _ := s.owner.mls.Epoch(ctx, s.gid)
		me, _ := s.mod.mls.Epoch(ctx, s.gid)
		return oe == me && oe > epoch+2 && !s.owner.takenOutLatched(s.g.ID) && len(leavesWith(s.mod, s.gid, s.owner.myCredential)) == 1
	}) {
		t.Fatal("the owner was not let back in")
	}
	healed, _ := s.owner.mls.Epoch(ctx, s.gid)

	// The removal replayed now that the owner is back: nothing.
	again := [][]byte{c2}
	serve.Store(&again)
	s.owner.requestSync.Store(&hostile)
	if err := s.owner.syncGuildFromPeer(s.g.ID, mod); err != nil {
		t.Fatal(err)
	}
	if s.owner.takenOutLatched(s.g.ID) || s.owner.EvictedFrom(s.g.ID) != "" {
		t.Fatalf("a replayed removal took the healed owner out again (latched=%v evicted=%q)",
			s.owner.takenOutLatched(s.g.ID), s.owner.EvictedFrom(s.g.ID))
	}
	if oe, _ := s.owner.mls.Epoch(ctx, s.gid); oe < healed {
		t.Fatalf("the owner went back to epoch %d from %d", oe, healed)
	}
	if len(leavesWith(s.mod, s.gid, s.owner.myCredential)) != 1 {
		t.Fatal("the owner is no longer seated at the mod")
	}
}

// Races. Many catch-ups at once from the owner against both members, the
// removal also arriving as a live commit, and many requests at once at one
// responder: one page is served per window, the owner latches once, says so
// once, and is let back in once.
func TestOwnerChainRace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	release := holdCatchUps(s.owner)
	_, c2, epoch := offlineRemoval(t, ctx, s)

	// The responder: sixteen requests for the same page at once, one served.
	op := s.owner.host.PeerID()
	req, _ := json.Marshal(syncRequest{GuildID: s.g.ID, Epoch: epoch, OwnerChain: true})
	var served atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := s.member.handleSyncRequest(ctx, op, req)
			if err == nil && len(raw) > 0 {
				served.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := served.Load(); n != 1 {
		t.Fatalf("%d of 16 simultaneous requests for one page were served, want 1", n)
	}

	// The owner: catch-ups against both members and the removal on the live
	// path, all at once.
	release()
	for i := 0; i < 4; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _ = s.owner.syncGuildFromPeer(s.g.ID, s.mod.host.PeerID()) }()
		go func() { defer wg.Done(); s.owner.syncGuildFromAnyPeer(s.g.ID) }()
		go func() { defer wg.Done(); s.owner.screenCommit(s.g.ID, s.gid, c2) }()
	}
	wg.Wait()
	if !ownerPoll(30*time.Second, func() bool { return s.owner.takenOutLatched(s.g.ID) }) {
		oe, _ := s.owner.mls.Epoch(ctx, s.gid)
		t.Fatalf("the owner never latched (epoch %d)", oe)
	}
	n := 0
	for _, x := range s.owner.SeatNotices(s.g.ID) {
		if x.Kind == "owner_removed" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d owner_removed notices, want 1", n)
	}
	if !ownerPoll(90*time.Second, func() bool {
		oe, _ := s.owner.mls.Epoch(ctx, s.gid)
		me, _ := s.mod.mls.Epoch(ctx, s.gid)
		ue, _ := s.member.mls.Epoch(ctx, s.gid)
		return oe == me && me == ue && oe > epoch+2 && !s.owner.takenOutLatched(s.g.ID) &&
			len(leavesWith(s.mod, s.gid, s.owner.myCredential)) == 1 &&
			len(leavesWith(s.member, s.gid, s.owner.myCredential)) == 1
	}) {
		t.Fatalf("the owner was not let back in exactly once (mod sees %d leaves, member %d)",
			len(leavesWith(s.mod, s.gid, s.owner.myCredential)), len(leavesWith(s.member, s.gid, s.owner.myCredential)))
	}
	if v, _ := s.owner.store.GetSetting(takenOutPrefix + s.g.ID); v != "" {
		t.Fatalf("the stored latch outlived the heal (%q)", v)
	}
}

func TestOwnerChainPageBounds(t *testing.T) {
	mk := func(n, size int) []store.CommitRow {
		rows := make([]store.CommitRow, n)
		for i := range rows {
			rows[i] = store.CommitRow{Epoch: uint64(i + 1), Commit: make([]byte, size)}
		}
		return rows
	}
	if got := len(ownerChainPage(mk(100, 1<<10))); got != ownerChainMaxCommits {
		t.Errorf("100 small commits: page of %d, want %d", got, ownerChainMaxCommits)
	}
	if got := len(ownerChainPage(mk(5, 200<<10))); got != 2 {
		t.Errorf("200 KiB commits: page of %d, want 2", got)
	}
	if got := len(ownerChainPage(mk(3, 600<<10))); got != 1 {
		t.Errorf("one commit over the byte bound: page of %d, want it alone", got)
	}
	if got := len(ownerChainPage(nil)); got != 0 {
		t.Errorf("no rows: page of %d", got)
	}
}

func TestClaimOwnerChainThrottle(t *testing.T) {
	s := &Service{}
	p := peer.ID("p")
	if !s.claimOwnerChain("g", string(p), 10, 5) {
		t.Fatal("first page refused")
	}
	for _, e := range []uint64{0, 10, 14} {
		if s.claimOwnerChain("g", string(p), e, 5) {
			t.Fatalf("epoch %d inside or before the last page was served again", e)
		}
	}
	if !s.claimOwnerChain("g", string(p), 15, 5) {
		t.Fatal("the next page was refused")
	}
	if s.claimOwnerChain("g", string(p), 15, 0) {
		t.Fatal("a gap answer was repeated inside the window")
	}
	if !s.claimOwnerChain("h", string(p), 15, 5) || !s.claimOwnerChain("g", "q", 15, 5) {
		t.Fatal("another guild or peer was throttled by this one")
	}
	// One guild holds at most its share, whatever number of keys asks.
	for i := 0; i < 3*ownerChainPerGuild; i++ {
		if !s.claimOwnerChain("x", fmt.Sprintf("p%d", i), 0, 1) {
			t.Fatalf("key %d of guild x refused instead of replacing the oldest", i)
		}
	}
	if n := countOwnerChainGuild(s, "x"); n != ownerChainPerGuild {
		t.Fatalf("guild x holds %d entries, want its share of %d", n, ownerChainPerGuild)
	}
	// The table is bounded, and a full one takes a new key in place of its
	// oldest entry rather than refusing it.
	for i := 0; len(s.ownerChainServed) < ownerChainTrack; i++ {
		s.claimOwnerChain(fmt.Sprintf("fill%d", i/ownerChainPerGuild), fmt.Sprintf("f%d", i), 0, 1)
	}
	if !s.claimOwnerChain("new", string(p), 0, 1) {
		t.Fatal("a full table of fresh entries refused a new owner")
	}
	if n := len(s.ownerChainServed); n != ownerChainTrack {
		t.Fatalf("table at %d entries, want %d", n, ownerChainTrack)
	}
	s.mu.Lock()
	for k, v := range s.ownerChainServed {
		v.at = v.at.Add(-ownerChainWindow)
		s.ownerChainServed[k] = v
	}
	s.mu.Unlock()
	if !s.claimOwnerChain("new2", string(p), 0, 1) || len(s.ownerChainServed) != 1 {
		t.Fatalf("stale entries were not pruned (%d left)", len(s.ownerChainServed))
	}
}

func countOwnerChainGuild(s *Service, guildID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, v := range s.ownerChainServed {
		if v.guild == guildID {
			n++
		}
	}
	return n
}
