package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
)

// countRemovals counts the leaf removals an engine commits.
type countRemovals struct {
	mls.Engine
	n *atomic.Int32
}

func (c *countRemovals) RemoveLeaf(ctx context.Context, gid mls.GroupID, index uint32, key []byte) ([]byte, error) {
	commit, err := c.Engine.RemoveLeaf(ctx, gid, index, key)
	if err == nil {
		c.n.Add(1)
	}
	return commit, err
}

// SCC-05. Two devices of one account, both allowed to manage members, flag
// the same leaf under the account's key. Only the account's lowest genuine
// leaf removes it at once; the other waits, finds it gone, and mints nothing.
// Exactly one removal commit results, and no copy falls out of step.
func TestOneOwnDeviceRemovesAFlaggedLeaf(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	prevDelay := seatDefenceDelay
	seatDefenceDelay = 3 * time.Second
	t.Cleanup(func() { seatDefenceDelay = prevDelay })

	boot := testRendezvous(t, ctx)
	var removals atomic.Int32
	start := func(dir string) *Service {
		t.Helper()
		s, err := Start(ctx, Config{
			DataDir: dir, Passphrase: "test-pass", DisableMDNS: true,
			BootstrapPeers: []string{boot},
			wrapMLS:        func(e mls.Engine) mls.Engine { return &countRemovals{Engine: e, n: &removals} },
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
	desk := start(t.TempDir())
	mod := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := desk.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := desk.InviteCode(g.ID)
	if _, err := mod.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMemberCount(t, 60*time.Second, g.ID, 2, desk, mod)

	link, err := desk.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	phoneDir := t.TempDir()
	res, err := RedeemLink(ctx, phoneDir, link, "test-pass", "")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := start(phoneDir)
	for _, ic := range res.GuildInvites {
		if _, err := phone.JoinViaInvite(ic); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	gid := rhGroup(desk, g.ID)
	waitUntil(t, 60*time.Second, func() bool {
		n, err := desk.otherOwnDevices(gid)
		return err == nil && n == 1
	}, "setup: the phone never took its seat")

	roleID, err := desk.UpsertRole(g.ID, "", "Mod", "#5865f2", PermManageMembers, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := desk.AssignRole(g.ID, mod.Fingerprint(), roleID, true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return mod.canManageMembers(g.ID) && phone.canManageMembers(g.ID) },
		"setup: the grant never arrived")

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)
	addLeafWithCredential(t, mod, gid, desk.myCredential, priv)
	waitUntil(t, 30*time.Second, func() bool {
		a, okA := flagFor(desk, g.ID, pub)
		b, okB := flagFor(phone, g.ID, pub)
		return okA && okB && a.Own && a.Provable && b.Own && b.Provable
	}, "setup: both own devices did not flag the leaf")
	for _, s := range []*Service{desk, phone} {
		s.markCaughtUp(g.ID)
		if !s.mayDefendSeats(g.ID) {
			t.Fatal("setup: an own device may not act")
		}
	}
	if n := removals.Load(); n != 0 {
		t.Fatalf("setup: %d removals before the devices were let act", n)
	}

	// Both devices are let act at the same moment.
	seatDefenceHold.Store(false)
	go desk.defendSeats(g.ID)
	go phone.defendSeats(g.ID)
	waitUntil(t, 30*time.Second, func() bool {
		for _, l := range leavesWith(mod, gid, desk.myCredential) {
			if bytes.Equal(l.Key, pub) {
				return false
			}
		}
		return true
	}, "the flagged leaf was never removed")
	time.Sleep(seatDefenceDelay + 4*time.Second)
	if n := removals.Load(); n != 1 {
		t.Fatalf("%d removal commits for one leaf, want exactly 1", n)
	}
	for name, s := range map[string]*Service{"desk": desk, "phone": phone, "mod": mod} {
		if s.OutOfSync(g.ID) {
			t.Errorf("%s is out of sync after the removal", name)
		}
	}
}

// fixedLeaves serves one made-up group's tree to auditSeats.
type fixedLeaves struct {
	mls.Engine
	gid    []byte
	leaves []mls.Leaf
}

func (f *fixedLeaves) Leaves(ctx context.Context, gid mls.GroupID) ([]mls.Leaf, error) {
	if bytes.Equal(gid, f.gid) {
		return f.leaves, nil
	}
	return f.Engine.Leaves(ctx, gid)
}

func (f *fixedLeaves) MemberLeaves(ctx context.Context, gid mls.GroupID) (map[uint32][]byte, error) {
	if bytes.Equal(gid, f.gid) {
		out := map[uint32][]byte{}
		for _, l := range f.leaves {
			out[l.Index] = l.Credential
		}
		return out, nil
	}
	return f.Engine.MemberLeaves(ctx, gid)
}

// M15. A leaf this device cannot decide is never provable: another account's
// key on two leaves, with or without a record of that account's key, is
// flagged ambiguous on both and offers no removal; another account's key on
// one leaf unlike the record is flagged key_differs only.
func TestALeafThisDeviceCannotDecideIsNeverProvable(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a service")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &fixedLeaves{gid: []byte("made-up group")}
	s, err := Start(ctx, Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true,
		wrapMLS: func(e mls.Engine) mls.Engine { fake.Engine = e; return fake }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	stranger, _, _ := ed25519.GenerateKey(rand.Reader)
	k1, _, _ := ed25519.GenerateKey(rand.Reader)
	k2, _, _ := ed25519.GenerateKey(rand.Reader)
	k3, _, _ := ed25519.GenerateKey(rand.Reader)
	audit := func(name string, leaves ...mls.Leaf) []SeatFlag {
		fake.leaves = leaves
		s.auditSeats(name, fake.gid)
		return s.SeatFlags(name)
	}

	// Two leaves, no record.
	two := []mls.Leaf{{Index: 0, Credential: s.myCredential, Key: s.ownAccountSigningKey()},
		{Index: 1, Credential: stranger, Key: k1}, {Index: 2, Credential: stranger, Key: k2}}
	for _, f := range audit("two-no-record", two...) {
		if f.Provable || f.Kind != seatAmbiguous {
			t.Fatalf("two leaves of one key, no record: %+v", f)
		}
	}
	// One leaf, which becomes the record; then two leaves, one matching it.
	if f := audit("one", two[0], two[1]); len(f) != 0 {
		t.Fatalf("a first sighting was flagged: %+v", f)
	}
	if !bytes.Equal(s.recordedAccountKey(stranger), k1) {
		t.Fatal("setup: the first sighting was not recorded")
	}
	flags := audit("two-with-record", two...)
	if len(flags) != 2 {
		t.Fatalf("two leaves of one key with a record: %+v, want both flagged", flags)
	}
	for _, f := range flags {
		if f.Provable || f.Kind != seatAmbiguous {
			t.Fatalf("two leaves of one key with a record: %+v", f)
		}
	}
	// One leaf unlike the record.
	flags = audit("unlike", two[0], mls.Leaf{Index: 1, Credential: stranger, Key: k3})
	if len(flags) != 1 || flags[0].Provable || flags[0].Kind != seatKeyDiffers {
		t.Fatalf("one leaf unlike the record: %+v", flags)
	}
	if s.keyFlagged("unlike", k3) {
		t.Fatal("a leaf unlike the record lost its authority")
	}
}
