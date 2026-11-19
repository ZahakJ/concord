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
