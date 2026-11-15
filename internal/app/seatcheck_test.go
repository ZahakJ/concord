package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// seatSetup is a guild of three: an owner, a member granted manage-members
// ("mod"), and an ordinary member.
type seatSetup struct {
	owner, mod, member *Service
	g                  domain.Guild
	gid                []byte
	roleID             string
}

func newSeatSetup(t *testing.T, ctx context.Context) *seatSetup {
	t.Helper()
	if testing.Short() {
		t.Skip("networked")
	}
	s := &seatSetup{owner: startService(t, ctx), mod: startService(t, ctx), member: startService(t, ctx)}
	g, err := s.owner.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	s.g = g
	code, _ := s.owner.InviteCode(g.ID)
	if _, err := s.mod.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, s.owner, s.mod)
	if _, err := s.member.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 3, s.owner, s.mod, s.member)
	s.gid = theGuild(t, s.owner).GroupID
	s.roleID, err = s.owner.UpsertRole(g.ID, "", "Mod", "#5865f2", PermManageMembers, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.owner.AssignRole(g.ID, s.mod.Fingerprint(), s.roleID, true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return s.mod.canManageMembers(g.ID) && s.member.authorizedCommitter(g.ID, s.mod.myCredential)
	}, "grant never arrived")
	// Honest joins and a role grant flag nothing anywhere.
	for _, svc := range []*Service{s.owner, s.mod, s.member} {
		if f := svc.SeatFlags(g.ID); len(f) != 0 {
			t.Fatalf("flags after honest joins: %+v", f)
		}
	}
	return s
}

// addLeafWithCredential has `by` commit an Add of a key package that carries
// cred under key, and returns the engine holding that leaf.
func addLeafWithCredential(t *testing.T, by *Service, gid, cred []byte, key ed25519.PrivateKey) mls.Engine {
	t.Helper()
	e, err := mls.New(cred, key)
	if err != nil {
		t.Fatal(err)
	}
	kp, err := e.KeyPackage(by.ctx)
	if err != nil {
		t.Fatal(err)
	}
	commit, welcome, _, err := by.mls.AddMembers(by.ctx, gid, [][]byte{kp})
	if err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	by.logCommit(gid, commit)
	if err := by.ps.Publish(by.ctx, domain.ControlTopicID(gid), commit); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Join(by.ctx, welcome); err != nil {
		t.Fatalf("Join: %v", err)
	}
	return e
}

func freshKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func leavesWith(s *Service, gid, cred []byte) []mls.Leaf {
	ls, _ := s.mls.Leaves(s.ctx, gid)
	var out []mls.Leaf
	for _, l := range ls {
		if bytes.Equal(l.Credential, cred) {
			out = append(out, l)
		}
	}
	return out
}

func flagFor(s *Service, guildID string, key []byte) (SeatFlag, bool) {
	for _, f := range s.SeatFlags(guildID) {
		if f.Key == hex.EncodeToString(key) {
			return f, true
		}
	}
	return SeatFlag{}, false
}

func publishMetaFrom(t *testing.T, e mls.Engine, via *Service, gid []byte, m guildMeta) {
	t.Helper()
	b, _ := json.Marshal(m)
	ct, err := e.Encrypt(via.ctx, gid, b)
	if err != nil {
		t.Fatal(err)
	}
	_ = via.ps.Publish(via.ctx, domain.GuildMetaTopicID(gid), ct)
}

func holdSeatDefence(t *testing.T) {
	seatDefenceHold.Store(true)
	t.Cleanup(func() { seatDefenceHold.Store(false) })
}

// The owner's own device takes out a leaf that carries the owner's account
// key under another signing key, and tells the owner who added it.
func TestOwnDeviceRemovesLeafUnderItsCredential(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newSeatSetup(t, ctx)
	key := freshKey(t)
	addLeafWithCredential(t, s.mod, s.gid, s.owner.myCredential, key)
	pub := key.Public().(ed25519.PublicKey)

	waitUntil(t, 20*time.Second, func() bool {
		ls := leavesWith(s.member, s.gid, s.owner.myCredential)
		return len(ls) == 1 && !bytes.Equal(ls[0].Key, pub) && len(leavesWith(s.owner, s.gid, s.owner.myCredential)) == 1
	}, "the leaf under the owner's credential was not removed")
	waitUntil(t, 10*time.Second, func() bool {
		for _, n := range s.owner.SeatNotices(s.g.ID) {
			if n.Kind == "removed" && n.Adder == s.mod.Fingerprint() {
				return true
			}
		}
		return false
	}, "no notice naming who added the leaf")
	if s.owner.EvictedFrom(s.g.ID) != "" || s.member.EvictedFrom(s.g.ID) != "" {
		t.Fatal("an honest device read the removal as its own")
	}
}

// A commit from the second leaf that removes the owner's own leaf is read as
// a replacement, not as the owner leaving. A member cannot tell the owner's
// two leaves apart (its record of the owner's key proves nothing), so it flags
// both and withdraws authority from both; once one is left, it flags that one
// as unlike its record, and only flags it.
func TestRemovalWithSameCredentialLeftBehindIsNotEviction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	key := freshKey(t)
	pub := key.Public().(ed25519.PublicKey)
	e := addLeafWithCredential(t, s.mod, s.gid, s.owner.myCredential, key)
	waitUntil(t, 20*time.Second, func() bool {
		return len(leavesWith(s.member, s.gid, s.owner.myCredential)) == 2 && len(leavesWith(s.owner, s.gid, s.owner.myCredential)) == 2
	}, "the add was not applied")
	waitUntil(t, 10*time.Second, func() bool {
		f, ok := flagFor(s.member, s.g.ID, pub)
		return ok && f.Kind == seatAmbiguous && !f.Provable
	}, "member does not flag the second leaf as one it cannot decide")
	waitUntil(t, 10*time.Second, func() bool {
		f, ok := flagFor(s.owner, s.g.ID, pub)
		return ok && f.Own && f.Provable
	}, "owner does not flag the leaf under its credential")
	// The genuine leaf of the same credential is flagged the same way: the
	// member has nothing that proves which of the two is the owner's.
	if f := s.member.SeatFlags(s.g.ID); len(f) != 2 || f[0].Provable || f[1].Provable {
		t.Fatalf("member flags %+v, want both leaves, neither provable", f)
	}

	ownIdx, err := s.owner.mls.OwnLeaf(ctx, s.gid)
	if err != nil {
		t.Fatal(err)
	}
	ownKey := s.owner.ownAccountSigningKey()
	rm, err := e.RemoveLeaf(ctx, s.gid, ownIdx, ownKey)
	if err != nil {
		t.Fatalf("RemoveLeaf: %v", err)
	}
	_ = s.mod.ps.Publish(ctx, domain.ControlTopicID(s.gid), rm)
	waitUntil(t, 20*time.Second, func() bool {
		ls := leavesWith(s.member, s.gid, s.owner.myCredential)
		return len(ls) == 1 && bytes.Equal(ls[0].Key, pub)
	}, "member did not apply the removal")
	waitUntil(t, 10*time.Second, func() bool {
		for _, n := range s.owner.SeatNotices(s.g.ID) {
			if n.Kind == "replaced" {
				return true
			}
		}
		return false
	}, "owner was not told its leaf was replaced")
	if r := s.owner.EvictedFrom(s.g.ID); r != "" {
		t.Fatalf("owner settled as evicted (%q)", r)
	}
	waitUntil(t, 10*time.Second, func() bool {
		f, ok := flagFor(s.member, s.g.ID, pub)
		return ok && f.Kind == seatKeyDiffers && !f.Provable
	}, "member does not flag the leaf that stayed as unlike its record")
}

// A leaf carrying a genuine certificate of one of the owner's devices, under
// a key that certificate does not name, is not a device of the owner: its
// frames change nothing, and it does not count as another device.
func TestCertificateLeafOnAnotherKeyIsNotASibling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	devPub, _, _ := ed25519.GenerateKey(rand.Reader)
	cert := s.owner.id.IssueDeviceCertFor(devPub, "phone", time.Now().Unix())
	key := freshKey(t)
	pub := key.Public().(ed25519.PublicKey)
	e := addLeafWithCredential(t, s.mod, s.gid, cert.Marshal(), key)
	waitUntil(t, 20*time.Second, func() bool {
		_, a := flagFor(s.owner, s.g.ID, pub)
		_, b := flagFor(s.member, s.g.ID, pub)
		return a && b
	}, "the leaf was not flagged")
	if n, _ := s.owner.otherOwnDevices(s.gid); n != 0 {
		t.Fatalf("owner counts %d other own devices", n)
	}
	if n := s.owner.accountLeafCount(s.gid); n != 1 {
		t.Fatalf("accountLeafCount = %d", n)
	}

	publishMetaFrom(t, e, s.mod, s.gid, guildMeta{Type: "member_left", Fingerprint: s.owner.Fingerprint()})
	publishMetaFrom(t, e, s.mod, s.gid, guildMeta{Type: "guild_renamed", Name: "other name"})
	time.Sleep(3 * time.Second)
	s.owner.mu.RLock()
	_, still := s.owner.guilds[s.g.ID]
	s.owner.mu.RUnlock()
	if !still {
		t.Fatal("owner left its guild on a frame from that leaf")
	}
	if theGuild(t, s.member).Name == "other name" {
		t.Fatal("member applied a rename from that leaf")
	}

	// With the role taken back, the owner is the only one who can repair
	// the group: the leaf is not a device that could.
	if err := s.owner.AssignRole(s.g.ID, s.mod.Fingerprint(), s.roleID, false); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool { return s.owner.onlyPossibleCommitter(s.g.ID) },
		"the leaf counted as a device that could repair the owner's copy")

	// This release flags commits from such a leaf and still applies them, so
	// nobody who checks falls behind those who do not.
	modLeaf := leavesWith(s.owner, s.gid, s.mod.myCredential)
	if len(modLeaf) != 1 {
		t.Fatal("mod leaf not found")
	}
	rm, err := e.RemoveLeaf(ctx, s.gid, modLeaf[0].Index, modLeaf[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.mod.ps.Publish(ctx, domain.ControlTopicID(s.gid), rm)
	waitUntil(t, 20*time.Second, func() bool {
		return len(leavesWith(s.member, s.gid, s.mod.myCredential)) == 0
	}, "the commit was not applied")

	// And the owner's device removes it once it may.
	seatDefenceHold.Store(false)
	go s.owner.defendSeats(s.g.ID)
	waitUntil(t, 20*time.Second, func() bool {
		_, a := flagFor(s.member, s.g.ID, pub)
		return !a && len(leavesWith(s.member, s.gid, cert.Marshal())) == 0
	}, "owner did not remove the leaf")
}

// Removing a leaf that carries a member's credential under another key, by
// index, does not make that member read itself as removed. A third party
// cannot prove such a bare-key leaf is not the member's, so it is not offered
// for removal; the removal here is committed directly. An admin who is not the
// owner may remove a leaf that provably is not the owner's (a certificate of
// the owner on another key); two leaves nobody can tell apart are not offered
// for removal.
func TestLeafRemovalByIndexKeepsMemberWithSameCredential(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)

	key := freshKey(t)
	pub := key.Public().(ed25519.PublicKey)
	addLeafWithCredential(t, s.mod, s.gid, s.member.myCredential, key)
	var idx uint32
	waitUntil(t, 20*time.Second, func() bool {
		f, ok := flagFor(s.owner, s.g.ID, pub)
		idx = f.Index
		m, onMember := flagFor(s.member, s.g.ID, pub)
		return ok && !f.Provable && onMember && m.Provable && m.Own
	}, "leaf under the member's credential not flagged")
	if err := s.owner.RemoveSeatLeaf(s.g.ID, idx, hex.EncodeToString(pub)); !errors.Is(err, ErrSeatNotProvable) {
		t.Fatalf("RemoveSeatLeaf on a leaf the owner cannot disprove: %v", err)
	}
	if err := s.owner.publishLeafRemoval(s.gid, idx, pub); err != nil {
		t.Fatalf("publishLeafRemoval: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return len(leavesWith(s.member, s.gid, s.member.myCredential)) == 1
	}, "member did not apply the removal")
	time.Sleep(2 * time.Second)
	if r := s.member.EvictedFrom(s.g.ID); r != "" {
		t.Fatalf("member read itself as removed (%q)", r)
	}

	// A certificate of the owner's under another key, removed by the mod
	// past the owner guard: it is provably not the owner's device.
	devPub, _, _ := ed25519.GenerateKey(rand.Reader)
	ownerCert := s.owner.id.IssueDeviceCertFor(devPub, "phone", time.Now().Unix()).Marshal()
	key2 := freshKey(t)
	pub2 := key2.Public().(ed25519.PublicKey)
	addLeafWithCredential(t, s.mod, s.gid, ownerCert, key2)
	var idx2 uint32
	waitUntil(t, 20*time.Second, func() bool {
		f, ok := flagFor(s.mod, s.g.ID, pub2)
		idx2 = f.Index
		return ok && f.Provable
	}, "mod does not flag the leaf")
	if err := s.mod.RemoveSeatLeaf(s.g.ID, idx2, hex.EncodeToString(pub2)); err != nil {
		t.Fatalf("mod RemoveSeatLeaf: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return len(leavesWith(s.member, s.gid, ownerCert)) == 0
	}, "removal by the mod not applied")

	// One account key nobody has seen, on two leaves in one commit.
	strangerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	ea, _ := mls.New(strangerPub, freshKey(t))
	eb, _ := mls.New(strangerPub, freshKey(t))
	kpa, _ := ea.KeyPackage(ctx)
	kpb, _ := eb.KeyPackage(ctx)
	commit, _, acc, err := s.mod.mls.AddMembers(ctx, s.gid, [][]byte{kpa, kpb})
	if err != nil || len(acc) != 2 {
		t.Fatalf("AddMembers: %v %v", err, acc)
	}
	s.mod.logCommit(s.gid, commit)
	_ = s.mod.ps.Publish(ctx, domain.ControlTopicID(s.gid), commit)
	var amb SeatFlag
	waitUntil(t, 20*time.Second, func() bool {
		for _, f := range s.owner.SeatFlags(s.g.ID) {
			if f.Account == accountFingerprintOf(strangerPub) {
				amb = f
				return true
			}
		}
		return false
	}, "two leaves of one key not flagged")
	if amb.Provable {
		t.Fatal("a leaf nobody can disprove is marked provable")
	}
	if err := s.owner.RemoveSeatLeaf(s.g.ID, amb.Index, amb.Key); !errors.Is(err, ErrSeatNotProvable) {
		t.Fatalf("removal of an undecidable leaf: %v", err)
	}
}

// A device that has not caught up since it started, or knows it is behind,
// mints no removal.
func TestNoRemovalBeforeCatchUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if testing.Short() {
		t.Skip("networked")
	}
	owner := startService(t, ctx)
	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	if !owner.mayDefendSeats(g.ID) {
		t.Fatal("a device that created its guild may not act in it")
	}
	st := owner.seatsState()
	st.mu.Lock()
	st.caughtUp[g.ID] = false
	st.mu.Unlock()
	if owner.mayDefendSeats(g.ID) {
		t.Fatal("a device that has not caught up may act")
	}
	owner.markCaughtUp(g.ID)
	if !owner.mayDefendSeats(g.ID) {
		t.Fatal("caught up, still may not act")
	}
	owner.mu.Lock()
	owner.outOfSync[g.ID] = true
	owner.mu.Unlock()
	if owner.mayDefendSeats(g.ID) {
		t.Fatal("a device that is behind may act")
	}
}

// Unsigned live messages: shown unverified, never allowed to change state,
// and dropped once their account has been seen signing.
func TestUnsignedLiveMessages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if testing.Short() {
		t.Skip("networked")
	}
	owner, member := startService(t, ctx), startService(t, ctx)
	ro := &recorder{}
	owner.OnMessage(ro.add)
	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, owner, member)
	gid := theGuild(t, owner).GroupID
	ch := g.Channels[0].ID
	unsigned := func(m domain.Message) {
		m.ChannelID, m.Sender, m.Sent = ch, member.PublicKey(), time.Now()
		b, _ := json.Marshal(m)
		ct, err := member.mls.Encrypt(ctx, gid, b)
		if err != nil {
			t.Fatal(err)
		}
		_ = member.ps.Publish(ctx, domain.TopicID(gid, ch), ct)
	}
	find := func(content string) (domain.Message, bool) {
		ro.mu.Lock()
		defer ro.mu.Unlock()
		for _, m := range ro.msgs {
			if m.Content == content {
				return m, true
			}
		}
		return domain.Message{}, false
	}
	// An account never seen signing (a build before 0.58 never signs). The
	// member's own join line was signed, so forget that here.
	st := owner.seatsState()
	waitUntil(t, 20*time.Second, func() bool {
		st.mu.Lock()
		delete(st.signers, member.Fingerprint())
		st.mu.Unlock()
		unsigned(domain.Message{ID: "u1", Content: "plain unsigned"})
		_, ok := find("plain unsigned")
		return ok
	}, "unsigned plain message never shown")
	if m, _ := find("plain unsigned"); !m.Unverified {
		t.Fatal("an unsigned message was not marked unverified")
	}
	unsigned(domain.Message{ID: "u2", Kind: "delete", ReplyTo: "u1"})
	time.Sleep(2 * time.Second)
	msgs, _ := owner.Messages(ch, 50)
	for _, m := range msgs {
		if m.ID == "u1" && m.Deleted {
			t.Fatal("an unsigned delete was applied")
		}
	}
	sendUntilReceived(t, member, ch, "signed one", ro)
	unsigned(domain.Message{ID: "u3", Content: "after signing"})
	time.Sleep(2 * time.Second)
	if _, ok := find("after signing"); ok {
		t.Fatal("an unsigned message was shown for an account seen signing")
	}
}

// The checks on single leaves.
func TestOwnAccountLeafPredicate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	devPub, _, _ := ed25519.GenerateKey(rand.Reader)
	cert := s.id.IssueDeviceCertFor(devPub, "phone", 1)
	canonical := cert.Marshal()
	var raw map[string]any
	_ = json.Unmarshal(canonical, &raw)
	raw["later"] = 1
	extended, _ := json.Marshal(raw)
	if _, ok := identity.ParseDeviceCert(extended); !ok {
		t.Fatal("extended certificate does not parse")
	}
	other := freshKey(t).Public().(ed25519.PublicKey)
	own := s.ownAccountSigningKey()
	cases := []struct {
		name      string
		l         mls.Leaf
		ownDevice bool
		notTheirs bool
	}{
		{"certificate on its key", mls.Leaf{Credential: canonical, Key: devPub}, true, false},
		{"certificate with a field this build does not know, on its key", mls.Leaf{Credential: extended, Key: devPub}, true, false},
		{"certificate on another key", mls.Leaf{Credential: canonical, Key: other}, false, true},
		{"account key on its signing key", mls.Leaf{Credential: s.myCredential, Key: own}, true, false},
		{"account key on another key", mls.Leaf{Credential: s.myCredential, Key: other}, false, true},
	}
	for _, c := range cases {
		if got := s.ownAccountLeaf(c.l); got != c.ownDevice {
			t.Errorf("%s: ownAccountLeaf = %v", c.name, got)
		}
		if got := s.leafNotTheirs(c.l); got != c.notTheirs {
			t.Errorf("%s: leafNotTheirs = %v", c.name, got)
		}
	}
}

// rewriteEngine reports a commit that rewrites the sender's leaf, and a
// member list that disagrees with the tree, without building either.
type rewriteEngine struct {
	mls.Engine
	cl      *mls.CommitLeaves
	members map[uint32][]byte
}

func (r *rewriteEngine) ReadCommitLeaves(ctx context.Context, gid mls.GroupID, c []byte) (*mls.CommitLeaves, error) {
	if r.cl != nil {
		return r.cl, nil
	}
	return r.Engine.ReadCommitLeaves(ctx, gid, c)
}

func (r *rewriteEngine) MemberLeaves(ctx context.Context, gid mls.GroupID) (map[uint32][]byte, error) {
	if r.members != nil {
		return r.members, nil
	}
	return r.Engine.MemberLeaves(ctx, gid)
}

// A commit that rewrites a leaf's credential is flagged at the commit. A
// member present at the add keeps flagging it on a later audit (after a
// restart, when the commit's own flags are gone), because the credential it
// recorded at the add differs from the tree. A member that joins by Welcome
// after the rewrite cannot see it (see auditSeats); nothing here models one.
func TestRewrittenLeafIsFlagged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if testing.Short() {
		t.Skip("networked")
	}
	var wrap *rewriteEngine
	owner, err := Start(ctx, Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true,
		wrapMLS: func(e mls.Engine) mls.Engine { wrap = &rewriteEngine{Engine: e}; return wrap }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	member := startService(t, ctx)
	g, _ := owner.CreateGuild("g")
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, owner, member)
	gid := theGuild(t, owner).GroupID
	ml := leavesWith(owner, gid, member.myCredential)
	if len(ml) != 1 {
		t.Fatal("member leaf")
	}
	// The member's leaf, rewritten to carry the owner's credential.
	wrap.cl = &mls.CommitLeaves{Sender: ml[0].Index, Replaced: []mls.Leaf{{Index: ml[0].Index, Credential: owner.myCredential, Key: ml[0].Key}}}
	owner.checkSeats(g.ID, gid, nil)
	wrap.cl = nil
	owner.auditSeats(g.ID, gid)
	f, ok := flagFor(owner, g.ID, ml[0].Key)
	if !ok || f.Kind != seatChanged {
		t.Fatalf("rewrite not flagged: %+v %v", f, ok)
	}
	if !owner.keyFlagged(g.ID, ml[0].Key) {
		t.Fatal("frames from the rewritten leaf would still carry authority")
	}

	// The audit alone, with the commit's flags gone, against a record of the
	// credential at the add that differs from the tree.
	members, _ := wrap.Engine.MemberLeaves(ctx, gid)
	members[ml[0].Index] = owner.myCredential
	wrap.members = members
	st := owner.seatsState()
	st.mu.Lock()
	delete(st.committed, g.ID)
	delete(st.flags, g.ID)
	st.mu.Unlock()
	owner.auditSeats(g.ID, gid)
	if f, ok := flagFor(owner, g.ID, ml[0].Key); !ok || f.Kind != seatChanged {
		t.Fatalf("audit does not flag a leaf whose credential changed: %+v %v", f, ok)
	}
}

// SCC-01. The key a device records beside another account's key is learned on
// first sight, in any group, so a leaf somebody else seated can set it. Here
// the member seats a leaf carrying the owner's account key, under a key of its
// own, in a guild of its own that the mod joins first: the mod's record of the
// owner's key is that leaf's. In the owner's own guild the mod then flags the
// owner's genuine leaf as unlike its record, and nothing more: the leaf is
// not provable, its removal is not offered, and its frames still count.
func TestAKeyRecordFromOneGuildDecidesNothingInAnother(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if testing.Short() {
		t.Skip("networked")
	}
	holdSeatDefence(t)
	owner, mod, member := startService(t, ctx), startService(t, ctx), startService(t, ctx)

	// The member's guild, with a leaf under the owner's account key.
	other, err := member.CreateGuild("elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := member.InviteCode(other.ID)
	if _, err := mod.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMemberCount(t, 30*time.Second, other.ID, 2, member, mod)
	otherGid := rhGroup(member, other.ID)
	key := freshKey(t)
	seated := key.Public().(ed25519.PublicKey)
	addLeafWithCredential(t, member, otherGid, owner.myCredential, key)
	waitUntil(t, 20*time.Second, func() bool {
		return bytes.Equal(mod.recordedAccountKey(owner.myCredential), seated)
	}, "setup: the mod never recorded the seated leaf's key for the owner")

	// The owner's guild, where the mod may manage members.
	g, err := owner.CreateGuild("home")
	if err != nil {
		t.Fatal(err)
	}
	code, _ = owner.InviteCode(g.ID)
	if _, err := mod.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMemberCount(t, 30*time.Second, g.ID, 2, owner, mod)
	roleID, err := owner.UpsertRole(g.ID, "", "Mod", "#5865f2", PermManageMembers, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AssignRole(g.ID, mod.Fingerprint(), roleID, true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool { return mod.canManageMembers(g.ID) }, "setup: the grant never arrived")
	gid := rhGroup(owner, g.ID)
	genuine := owner.ownAccountSigningKey()
	var f SeatFlag
	waitUntil(t, 20*time.Second, func() bool {
		var ok bool
		f, ok = flagFor(mod, g.ID, genuine)
		return ok && f.Kind == seatKeyDiffers
	}, "the mod does not flag the owner's leaf as unlike its record")
	if f.Provable {
		t.Fatalf("a record from another guild made the owner's genuine leaf provable: %+v", f)
	}
	if err := mod.RemoveSeatLeaf(g.ID, f.Index, f.Key); !errors.Is(err, ErrSeatNotProvable) {
		t.Fatalf("removal of the owner's genuine leaf: err=%v, want ErrSeatNotProvable", err)
	}
	if mod.keyFlagged(g.ID, genuine) {
		t.Fatal("frames from the owner's genuine leaf would be dropped")
	}
	if err := owner.SetNickname(g.ID, "still counts"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool { return mod.NickOf(g.ID, owner.Fingerprint()) == "still counts" },
		"the mod dropped a frame from the owner's genuine leaf")
	if n := len(leavesWith(mod, gid, owner.myCredential)); n != 1 {
		t.Fatalf("the owner's leaf count in its own guild is %d", n)
	}
}
