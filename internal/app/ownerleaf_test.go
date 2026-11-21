package app

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/libp2p/go-libp2p/core/peer"
)

func ownerPoll(d time.Duration, f func() bool) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if f() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return f()
}

func guildHasChannel(s *Service, guildID, name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return false
	}
	for _, c := range g.Channels {
		if c.Name == name {
			return true
		}
	}
	return false
}

// A manage-members holder's removal of the owner's leaf applies at the other
// members, as it does at older ones. The owner's device does not take it as
// its removal: it asks to be let back in, a committer that applied the
// removal re-admits it, and the owner carries on.
func TestOwnerTakenOutAsksBackIn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	epoch, _ := s.member.mls.Epoch(ctx, s.gid)

	rm, err := s.mod.mls.RemoveLeaf(ctx, s.gid, ownerIdx, s.owner.ownAccountSigningKey())
	if err != nil {
		t.Fatal(err)
	}
	s.mod.logCommit(s.gid, rm)
	_ = s.mod.ps.Publish(ctx, domain.ControlTopicID(s.gid), rm)
	if !ownerPoll(20*time.Second, func() bool {
		e, _ := s.member.mls.Epoch(ctx, s.gid)
		return e == epoch+1 && len(leavesWith(s.member, s.gid, s.owner.myCredential)) == 0
	}) {
		t.Fatal("the member did not apply the removal")
	}
	if !ownerPoll(45*time.Second, func() bool {
		return len(leavesWith(s.member, s.gid, s.owner.myCredential)) == 1
	}) {
		t.Fatalf("the owner was not let back in (EvictedFrom=%q)", s.owner.EvictedFrom(s.g.ID))
	}
	if v := s.owner.EvictedFrom(s.g.ID); v != "" {
		t.Fatalf("the owner recorded itself removed (%q)", v)
	}
	if !ownerPoll(20*time.Second, func() bool {
		_, err := s.owner.CreateChannel(s.g.ID, "after", "text", "")
		return err == nil
	}) {
		t.Fatal("the owner cannot create a channel")
	}
	if !ownerPoll(20*time.Second, func() bool { return guildHasChannel(s.member, s.g.ID, "after") }) {
		t.Fatal("the member never saw the owner's channel")
	}
}

// A heal answered with the removed, banned or never-admitted sentence leaves
// the owner's device in the guild; a member that is not the owner still takes
// each as the guild's answer.
func TestRefusalTextsDoNotSettleTheOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	for _, text := range []string{removedRefusal, bannedRefusal, unseatedRefusal} {
		for _, svc := range []*Service{s.owner, s.member} {
			var asked atomic.Int32
			var refuse inviteAskFunc = func(context.Context, peer.AddrInfo, []byte) ([]byte, error) {
				asked.Add(1)
				return json.Marshal(inviteResponse{Error: text})
			}
			svc.requestInvite.Store(&refuse)
			svc.setOutOfSync(s.g.ID, true)
			svc.healOutOfSync(s.g.ID)
			svc.requestInvite.Store(nil)
			got := svc.EvictedFrom(s.g.ID)
			svc.clearEvicted(s.g.ID)
			svc.setOutOfSync(s.g.ID, false)
			if asked.Load() == 0 {
				t.Fatalf("%q: no heal was asked", text)
			}
			if svc == s.owner && got != "" {
				t.Errorf("%q: the owner recorded %q", text, got)
			}
			if svc == s.member && got == "" {
				t.Errorf("%q: the member recorded nothing", text)
			}
		}
	}
}

// A committer whose tree no longer seats the owner still serves the owner's
// heal, and still refuses a heal from someone it never seated.
func TestHealDoorServesTheOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	// Taken out at the mod only.
	rm, err := s.mod.mls.RemoveLeaf(ctx, s.gid, ownerIdx, s.owner.ownAccountSigningKey())
	if err != nil {
		t.Fatal(err)
	}
	s.mod.logCommit(s.gid, rm)
	s.mod.forgetMemberSet(s.g.ID)
	if s.mod.guildHasMember(s.g.ID, s.owner.Fingerprint()) {
		t.Fatal("setup: the mod's tree still seats the owner")
	}
	ask := func(from *Service) inviteResponse {
		kp, err := from.mls.KeyPackage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := json.Marshal(inviteRequest{GuildID: s.g.ID, KeyPackage: kp, Credential: from.myCredential, Heal: true})
		raw, err := s.mod.handleInviteRequest(ctx, from.host.PeerID(), req)
		if err != nil {
			t.Fatal(err)
		}
		var resp inviteResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}
	stranger := startService(t, ctx)
	if r := ask(stranger); r.Error != unseatedRefusal {
		t.Fatalf("a heal from someone never seated: %q, want the never-admitted refusal", r.Error)
	}
	if r := ask(s.owner); r.Error != "" || len(r.Welcome) == 0 {
		t.Fatalf("the owner's heal: error %q, welcome %d bytes", r.Error, len(r.Welcome))
	}
}

// After a leaf carrying the owner's credential, seated by the mod, removes
// the owner's leaf, the owner's heal is answered with the removed sentence.
// The owner does not settle on it.
func TestOwnerAfterReplacementIgnoresRemovedText(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	key := freshKey(t)
	e := addLeafWithCredential(t, s.mod, s.gid, s.owner.myCredential, key)
	if !ownerPoll(20*time.Second, func() bool {
		return len(leavesWith(s.member, s.gid, s.owner.myCredential)) == 2 && len(leavesWith(s.owner, s.gid, s.owner.myCredential)) == 2
	}) {
		t.Fatal("setup: add not applied")
	}
	var asked atomic.Int32
	var refuse inviteAskFunc = func(context.Context, peer.AddrInfo, []byte) ([]byte, error) {
		asked.Add(1)
		return json.Marshal(inviteResponse{Error: removedRefusal})
	}
	s.owner.requestInvite.Store(&refuse)
	defer s.owner.requestInvite.Store(nil)
	ownIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	rm, err := e.RemoveLeaf(ctx, s.gid, ownIdx, s.owner.ownAccountSigningKey())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.mod.ps.Publish(ctx, domain.ControlTopicID(s.gid), rm)
	if err := s.mod.mls.ApplyCommit(ctx, s.gid, rm); err != nil {
		t.Fatalf("mod apply: %v", err)
	}
	s.mod.logCommit(s.gid, rm)
	if !ownerPoll(60*time.Second, func() bool { return asked.Load() > 0 }) {
		t.Fatal("the owner never asked to be let back in")
	}
	time.Sleep(2 * time.Second)
	if v := s.owner.EvictedFrom(s.g.ID); v != "" {
		t.Fatalf("the owner recorded itself removed (%q) after %d asks", v, asked.Load())
	}
}

// The order that left the owner out for good, forced: both other members have
// applied the removal of the owner's leaf, and so decline its catch-up, before
// the owner reads the commit. Reading it is what flags the guild. A member
// still at the old epoch answering readably does not lower the flag, and a
// restart does not lose it. The first committer asked lets the owner back in.
func TestOwnerTakenOutFlagsItselfOnTheCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)

	// Every heal the owner asks for waits here until the test lets it through.
	asked := make(chan struct{}, 16)
	release := make(chan struct{})
	var hold inviteAskFunc = func(ctx context.Context, to peer.AddrInfo, req []byte) ([]byte, error) {
		asked <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return s.owner.host.RequestInvite(ctx, to, req)
	}
	s.owner.requestInvite.Store(&hold)
	defer s.owner.requestInvite.Store(nil)

	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	epoch, _ := s.owner.mls.Epoch(ctx, s.gid)
	rm, err := s.mod.mls.RemoveLeaf(ctx, s.gid, ownerIdx, s.owner.ownAccountSigningKey())
	if err != nil {
		t.Fatal(err)
	}
	s.mod.logCommit(s.gid, rm)
	// Not published: each device meets the commit when the test hands it over.
	if s.owner.OutOfSync(s.g.ID) {
		t.Fatal("setup: the owner is out of sync before the removal")
	}
	if err := s.owner.syncGuildFromPeer(s.g.ID, s.mod.host.PeerID()); err != errSyncDeclined {
		t.Fatalf("the mod's answer to the owner's catch-up: %v, want declined", err)
	}
	if s.owner.OutOfSync(s.g.ID) {
		t.Fatal("a declined catch-up flagged the guild")
	}

	verdict, _ := s.owner.screenCommit(s.g.ID, s.gid, rm)
	if verdict != commitProceed {
		t.Fatalf("verdict %d, want commitProceed", verdict)
	}
	if v := s.owner.EvictedFrom(s.g.ID); v != "" {
		t.Fatalf("the owner recorded itself removed (%q)", v)
	}
	if !s.owner.OutOfSync(s.g.ID) || !s.owner.takenOutLatched(s.g.ID) {
		t.Fatalf("after reading the removal: OutOfSync=%v latched=%v, want both",
			s.owner.OutOfSync(s.g.ID), s.owner.takenOutLatched(s.g.ID))
	}

	// The recovery that the flag started reaches the door and waits there.
	select {
	case <-asked:
	case <-time.After(90 * time.Second):
		t.Fatal("the owner never asked to be let back in")
	}
	// The member has not met the removal: it seats the owner at the old epoch
	// and answers readably.
	if e, _ := s.member.mls.Epoch(ctx, s.gid); e != epoch {
		t.Fatalf("setup: the member is at epoch %d, want %d", e, epoch)
	}
	if err := s.owner.syncGuildFromPeer(s.g.ID, s.member.host.PeerID()); err != nil {
		t.Fatalf("the member's answer to the owner's catch-up: %v", err)
	}
	if !s.owner.OutOfSync(s.g.ID) {
		t.Fatal("a readable answer from the epoch the owner was taken out of lowered the flag")
	}
	// A restart: nothing in memory, the latch in the store.
	s.owner.mu.Lock()
	delete(s.owner.takenOut, s.g.ID)
	delete(s.owner.outOfSync, s.g.ID)
	s.owner.mu.Unlock()
	s.owner.loadTakenOut()
	if !s.owner.OutOfSync(s.g.ID) || !s.owner.takenOutLatched(s.g.ID) {
		t.Fatal("the latch did not come back from the store")
	}

	close(release)
	if !ownerPoll(90*time.Second, func() bool {
		oe, _ := s.owner.mls.Epoch(ctx, s.gid)
		me, _ := s.mod.mls.Epoch(ctx, s.gid)
		return oe == me && oe > epoch+1 && len(leavesWith(s.mod, s.gid, s.owner.myCredential)) == 1 &&
			!s.owner.OutOfSync(s.g.ID)
	}) {
		t.Fatalf("the owner was not let back in (EvictedFrom=%q)", s.owner.EvictedFrom(s.g.ID))
	}
	if s.owner.takenOutLatched(s.g.ID) {
		t.Fatal("the latch outlived the heal")
	}
	if v, _ := s.owner.store.GetSetting(takenOutPrefix + s.g.ID); v != "" {
		t.Fatalf("the stored latch outlived the heal (%q)", v)
	}
	if v := s.owner.EvictedFrom(s.g.ID); v != "" {
		t.Fatalf("the owner recorded itself removed (%q)", v)
	}
}

// The same commit against a member that does not own the guild settles as its
// removal: no flag, no latch, and a committer refuses the heal it could still
// send by hand.
func TestTakenOutMemberGetsNoWayBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	idx, _ := s.member.mls.OwnLeaf(ctx, s.gid)
	rm, err := s.mod.mls.RemoveLeaf(ctx, s.gid, idx, s.member.ownAccountSigningKey())
	if err != nil {
		t.Fatal(err)
	}
	s.mod.logCommit(s.gid, rm)
	if verdict, _ := s.member.screenCommit(s.g.ID, s.gid, rm); verdict != commitRemovesThisDevice {
		t.Fatalf("verdict %d, want commitRemovesThisDevice", verdict)
	}
	if s.member.OutOfSync(s.g.ID) || s.member.takenOutLatched(s.g.ID) {
		t.Fatalf("a removed member took the owner's path: OutOfSync=%v latched=%v",
			s.member.OutOfSync(s.g.ID), s.member.takenOutLatched(s.g.ID))
	}
	// Even latched by a client that ignores the rule above, it only asks.
	kp, err := s.member.mls.KeyPackage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := json.Marshal(inviteRequest{GuildID: s.g.ID, KeyPackage: kp, Credential: s.member.myCredential, Heal: true})
	raw, err := s.mod.handleInviteRequest(ctx, s.member.host.PeerID(), req)
	if err != nil {
		t.Fatal(err)
	}
	var resp inviteResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != unseatedRefusal || len(resp.Welcome) != 0 {
		t.Fatalf("the mod's answer to a removed member's heal: error %q, welcome %d bytes", resp.Error, len(resp.Welcome))
	}
	if len(leavesWith(s.mod, s.gid, s.member.myCredential)) != 0 {
		t.Fatal("the removed member is seated again")
	}
}
