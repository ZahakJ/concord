package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
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

func ownerSetup(t *testing.T, ctx context.Context) (owner, mod, member *Service, guildID string, gid []byte) {
	t.Helper()
	if testing.Short() {
		t.Skip("networked")
	}
	owner, mod, member = startService(t, ctx), startService(t, ctx), startService(t, ctx)
	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := mod.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, owner, mod)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 3, owner, mod, member)
	gid = theGuild(t, owner).GroupID
	roleID, err := owner.UpsertRole(g.ID, "", "Mod", "#5865f2", PermManageMembers, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AssignRole(g.ID, mod.Fingerprint(), roleID, true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return mod.canManageMembers(g.ID) && member.authorizedCommitter(g.ID, mod.myCredential) && owner.authorizedCommitter(g.ID, mod.myCredential)
	}, "grant never arrived")
	return owner, mod, member, g.ID, gid
}

func ownerCredCount(ctx context.Context, s *Service, gid, cred []byte) int {
	ms, _ := s.mls.Members(ctx, gid)
	n := 0
	for _, m := range ms {
		if bytes.Equal(m, cred) {
			n++
		}
	}
	return n
}

// afterRemoval: the owner records no removal, and is let back in.
func afterRemoval(t *testing.T, ctx context.Context, owner, member *Service, guildID string, gid []byte, want int) {
	t.Helper()
	// The guild carries on talking; the owner's device meets traffic it
	// cannot read.
	ch := theGuild(t, member).Channels[0].ID
	if !ownerPoll(45*time.Second, func() bool {
		_, _ = member.SendMessage(ch, "still here", "", "")
		return ownerCredCount(ctx, member, gid, owner.myCredential) == want
	}) {
		oe, _ := owner.mls.Epoch(ctx, gid)
		me, _ := member.mls.Epoch(ctx, gid)
		t.Fatalf("the owner was not let back in (EvictedFrom=%q, %d leaves under its credential at the member, owner epoch %d out-of-sync %v, member epoch %d)",
			owner.EvictedFrom(guildID), ownerCredCount(ctx, member, gid, owner.myCredential), oe, owner.OutOfSync(guildID), me)
	}
	if v := owner.EvictedFrom(guildID); v != "" {
		t.Fatalf("the owner recorded itself removed (%q)", v)
	}
	if !ownerPoll(20*time.Second, func() bool {
		_, err := owner.CreateChannel(guildID, "after", "text", "")
		return err == nil
	}) {
		t.Fatal("the owner cannot create a channel")
	}
}

// A manage-members holder's removal of the owner's leaf: the members apply
// it, the owner does not take it as its removal and is let back in.
func TestOwnerTakenOutAsksBackIn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner, mod, member, guildID, gid := ownerSetup(t, ctx)
	e0, _ := member.mls.Epoch(ctx, gid)
	rm, err := mod.mls.Remove(ctx, gid, owner.myCredential)
	if err != nil {
		t.Fatal(err)
	}
	mod.logCommit(gid, rm)
	_ = mod.ps.Publish(ctx, domain.ControlTopicID(gid), rm)
	if !ownerPoll(20*time.Second, func() bool {
		e, _ := member.mls.Epoch(ctx, gid)
		return e > e0
	}) {
		t.Fatal("the member did not apply the removal")
	}
	afterRemoval(t, ctx, owner, member, guildID, gid, 1)
}

// The same, by a leaf the mod seated with the owner's credential. That leaf
// removes by credential, which both leaves carry, so the engine may pick
// either; a run where it took out its own leaf is run again.
func TestOwnerReplacedAsksBackIn(t *testing.T) {
	for attempt := 1; attempt <= 6; attempt++ {
		if ownerReplacedOnce(t, attempt) {
			return
		}
	}
	t.Fatal("the seated leaf never removed the owner's leaf in 6 runs")
}

func ownerReplacedOnce(t *testing.T, attempt int) bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner, mod, member, guildID, gid := ownerSetup(t, ctx)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	e, err := mls.New(owner.myCredential, key)
	if err != nil {
		t.Fatal(err)
	}
	kp, err := e.KeyPackage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	commit, welcome, _, err := mod.mls.AddMembers(ctx, gid, [][]byte{kp})
	if err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	mod.logCommit(gid, commit)
	_ = mod.ps.Publish(ctx, domain.ControlTopicID(gid), commit)
	if _, err := e.Join(ctx, welcome); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if !ownerPoll(20*time.Second, func() bool {
		return ownerCredCount(ctx, member, gid, owner.myCredential) == 2 && ownerCredCount(ctx, owner, gid, owner.myCredential) == 2
	}) {
		t.Fatal("setup: add not applied")
	}
	e0, _ := member.mls.Epoch(ctx, gid)
	rm, err := e.Remove(ctx, gid, owner.myCredential)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	_ = mod.ps.Publish(ctx, domain.ControlTopicID(gid), rm)
	if err := mod.mls.ApplyCommit(ctx, gid, rm); err != nil {
		t.Fatalf("mod apply: %v", err)
	}
	mod.logCommit(gid, rm)
	if !ownerPoll(20*time.Second, func() bool { e, _ := member.mls.Epoch(ctx, gid); return e > e0 }) {
		t.Fatal("the member did not apply the removal")
	}
	time.Sleep(time.Second)
	if oe, _ := owner.mls.Epoch(ctx, gid); oe == e0+1 {
		t.Logf("run %d: the seated leaf removed itself; again", attempt)
		return false
	}
	afterRemoval(t, ctx, owner, member, guildID, gid, 2)
	return true
}

// A heal answered with the removed, banned or never-admitted sentence leaves
// the owner in the guild; a member that is not the owner takes each.
func TestRefusalTextsDoNotSettleTheOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner, _, member, guildID, _ := ownerSetup(t, ctx)
	for _, text := range []string{removedRefusal, bannedRefusal, unseatedRefusal} {
		for _, svc := range []*Service{owner, member} {
			var asked atomic.Int32
			refuse := func(context.Context, peer.AddrInfo, []byte) ([]byte, error) {
				asked.Add(1)
				return json.Marshal(inviteResponse{Error: text})
			}
			svc.requestInvite.Store(&refuse)
			svc.setOutOfSync(guildID, true)
			svc.healOutOfSync(guildID)
			svc.requestInvite.Store(nil)
			got := svc.EvictedFrom(guildID)
			svc.clearEvicted(guildID)
			svc.setOutOfSync(guildID, false)
			if asked.Load() == 0 {
				t.Fatalf("%q: no heal was asked", text)
			}
			if svc == owner && got != "" {
				t.Errorf("%q: the owner recorded %q", text, got)
			}
			if svc == member && got == "" {
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
	owner, mod, _, guildID, gid := ownerSetup(t, ctx)
	rm, err := mod.mls.Remove(ctx, gid, owner.myCredential)
	if err != nil {
		t.Fatal(err)
	}
	mod.logCommit(gid, rm)
	mod.forgetMemberSet(guildID)
	if mod.guildHasMember(guildID, owner.Fingerprint()) {
		t.Fatal("setup: the mod's tree still seats the owner")
	}
	ask := func(from *Service) inviteResponse {
		kp, err := from.mls.KeyPackage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := json.Marshal(inviteRequest{GuildID: guildID, KeyPackage: kp, Credential: from.myCredential, Heal: true})
		raw, err := mod.handleInviteRequest(ctx, from.host.PeerID(), req)
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
	if r := ask(owner); r.Error != "" || len(r.Welcome) == 0 {
		t.Fatalf("the owner's heal: error %q, welcome %d bytes", r.Error, len(r.Welcome))
	}
}
