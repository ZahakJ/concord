package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thomas-vilte/mls-go/keypackages"

	"github.com/ZahakJ/concord/internal/domain"
)

// A device linked again in place keeps its device key (RedeemLink reuses the
// device seed) but gets a new certificate, with a new issue time. Its old leaf
// is still in every guild it was in. Committers used to evict that leaf by
// exact credential bytes, found nothing, and refused the join with "invite
// failed", for good, in guilds the account owns and guilds it merely belongs to.

// relinkWorld: O, another account, owns "Theirs"; desk, the account's original
// device, owns "Ours" and is a member of Theirs; phone is linked to desk's
// account and seated in both.
type relinkWorld struct {
	ctx            context.Context
	boot           string
	o, desk, phone *Service
	phoneDir       string
	ours, theirs   domain.Guild
}

// distinctCertTimes makes every certificate issued during the test carry its
// own issue time, as two links of one device a minute apart do, without
// depending on the clock's second turning between them.
func distinctCertTimes(t *testing.T) {
	t.Helper()
	var n atomic.Int64
	n.Store(time.Now().Unix())
	prev := linkCertTime
	linkCertTime = func() int64 { return n.Add(1) }
	t.Cleanup(func() { linkCertTime = prev })
}

func setupRelinkWorld(t *testing.T, ctx context.Context) *relinkWorld {
	t.Helper()
	w := &relinkWorld{ctx: ctx, boot: testRendezvous(t, ctx)}
	w.o = startServiceOn(t, ctx, t.TempDir(), w.boot)
	w.desk = startServiceOn(t, ctx, t.TempDir(), w.boot)
	var err error
	if w.theirs, err = w.o.CreateGuild("Theirs"); err != nil {
		t.Fatalf("O CreateGuild: %v", err)
	}
	code, err := w.o.InviteCode(w.theirs.ID)
	if err != nil {
		t.Fatalf("O InviteCode: %v", err)
	}
	if _, err := w.desk.JoinViaInvite(code); err != nil {
		t.Fatalf("desk joins Theirs: %v", err)
	}
	if w.ours, err = w.desk.CreateGuild("Ours"); err != nil {
		t.Fatalf("desk CreateGuild: %v", err)
	}
	w.phoneDir = t.TempDir()
	var failed []LinkJoinFailure
	w.phone, failed = w.link(t)
	if len(failed) != 0 {
		t.Fatalf("setup: the first link could not join %+v", failed)
	}
	waitUntil(t, 30*time.Second, func() bool {
		a, _ := w.desk.MemberCount(w.ours.ID)
		b, _ := w.o.MemberCount(w.theirs.ID)
		return a == 2 && b == 3 && w.phone.holdsGuild(w.ours.ID) && w.phone.holdsGuild(w.theirs.ID)
	}, "setup: the phone was not seated in both guilds")
	return w
}

// link does what bridge.RedeemLinkCode does with the phone's directory,
// confirming the replacement when the install already holds the account.
func (w *relinkWorld) link(t *testing.T) (*Service, []LinkJoinFailure) {
	t.Helper()
	code, err := w.desk.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	res, err := RedeemLink(w.ctx, w.phoneDir, code, "test-pass", "")
	var refused *ReplaceRefusedError
	if errors.As(err, &refused) {
		code, _ = w.desk.LinkOffer()
		res, err = RedeemLink(w.ctx, w.phoneDir, code, "test-pass", refused.Confirm)
	}
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	p := startServiceOn(t, w.ctx, w.phoneDir, w.boot)
	return p, p.AdoptLinkedGuilds(res.GuildInvites)
}

// relinkInPlace closes the phone and links the same install again.
func (w *relinkWorld) relinkInPlace(t *testing.T) (*Service, []LinkJoinFailure) {
	t.Helper()
	oldKey := deviceKeyOf(t, w.phone)
	_ = w.phone.Close()
	p, failed := w.link(t)
	if deviceKeyOf(t, p) != oldKey {
		t.Fatal("the relink did not keep the device key; the case under test did not arise")
	}
	return p, failed
}

// The regression, for a guild the account owns (desk commits) and one it is
// only a member of (O, another account, commits).
func TestARelinkedDeviceRejoinsGuildsHoldingItsOldLeaf(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	distinctCertTimes(t)
	w := setupRelinkWorld(t, ctx)

	p, failed := w.relinkInPlace(t)
	if len(failed) != 0 {
		t.Fatalf("the relinked device could not join: %+v", failed)
	}
	for _, g := range []domain.Guild{w.ours, w.theirs} {
		if !p.holdsGuild(g.ID) {
			t.Errorf("the relinked device does not hold %s", g.Name)
		}
	}
	// The old leaf was replaced, not joined by a second one, and nobody
	// else's leaf went: desk keeps its leaf in both guilds, O in Theirs.
	waitUntil(t, 30*time.Second, func() bool {
		a, _ := w.desk.MemberCount(w.ours.ID)
		b, _ := w.o.MemberCount(w.theirs.ID)
		return a == 2 && b == 3
	}, "leaf counts after the relink are not desk+phone and O+desk+phone")
	if w.desk.accountLeafCount(w.ours.GroupID) != 2 {
		t.Error("desk's own leaf is gone from Ours")
	}
	// And the relinked device can be read: a message from O reaches it.
	if _, err := w.o.SendMessage(w.theirs.Channels[0].ID, "after-relink", "", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		msgs, _ := p.Messages(w.theirs.Channels[0].ID, 50)
		for _, m := range msgs {
			if m.Content == "after-relink" {
				return true
			}
		}
		return false
	}, "the relinked device cannot read Theirs")
}

// A committer evicts only the leaf holding the joiner's own, proven key. A key
// package naming another member's key without that member's signature, or a
// joiner whose key is in no leaf, evicts nobody.
func TestALeafWithAnotherKeyIsNeverEvicted(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	member := startServiceOn(t, ctx, t.TempDir(), boot)
	stranger := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	owner.mu.RLock()
	gg := owner.guilds[g.ID]
	owner.mu.RUnlock()

	leaves, err := owner.mls.Leaves(ctx, g.GroupID)
	if err != nil || len(leaves) != 2 {
		t.Fatalf("leaves: %v %d", err, len(leaves))
	}
	var memberKey []byte
	for _, l := range leaves {
		if accountFingerprintOf(l.Credential) == member.Fingerprint() {
			memberKey = l.Key
		}
	}
	if memberKey == nil {
		t.Fatal("no leaf for the member")
	}

	// A joiner whose key is in no leaf.
	kp, err := stranger.mls.KeyPackage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.evictStaleLeaf(gg, kp); !errors.Is(err, errNoStaleLeaf) {
		t.Errorf("a key in no leaf: got %v, want errNoStaleLeaf", err)
	}

	// The stranger's key package with the member's key written in. Its
	// signature does not verify under that key.
	parsed, err := keypackages.UnmarshalKeyPackage(kp)
	if err != nil {
		t.Fatal(err)
	}
	parsed.LeafNode.SignatureKeyBytes = append([]byte(nil), memberKey...)
	parsed.LeafNode.SignatureKey = nil
	forged := parsed.Marshal()
	if _, err := owner.evictStaleLeaf(gg, forged); err == nil {
		t.Error("a key package naming the member's key, unsigned by it, evicted a leaf")
	}

	after, _ := owner.mls.Leaves(ctx, g.GroupID)
	if len(after) != 2 {
		t.Fatalf("a leaf went: %d leaves left", len(after))
	}
	found := false
	for _, l := range after {
		found = found || bytes.Equal(l.Key, memberKey)
	}
	if !found {
		t.Fatal("the member's leaf was evicted")
	}
}

// When a committer refuses the join (here: the older eviction, by credential
// bytes), the link reports which guilds failed and keeps trying them, so they
// arrive once the committer can admit the device.
func TestALinkReportsAndRetriesTheGuildsItCouldNotJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	distinctCertTimes(t)
	prevBackoff := linkRetryBackoff
	linkRetryBackoff = []time.Duration{3 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second}
	t.Cleanup(func() { linkRetryBackoff = prevBackoff })
	w := setupRelinkWorld(t, ctx)

	staleLeafByCredential.Store(true)
	t.Cleanup(func() { staleLeafByCredential.Store(false) })
	p, failed := w.relinkInPlace(t)
	got := map[string]LinkJoinFailure{}
	for _, f := range failed {
		got[f.GuildID] = f
	}
	for _, g := range []domain.Guild{w.ours, w.theirs} {
		f, ok := got[g.ID]
		if !ok {
			t.Fatalf("%s is not reported as failed: %+v", g.Name, failed)
		}
		if f.Name != g.Name || !f.Refused || !strings.Contains(f.Err, "invite failed") {
			t.Errorf("%s reported as %+v", g.Name, f)
		}
	}

	// Long enough for the hello the two devices exchange on connecting to
	// offer the same guilds and be refused too, so what brings them in below
	// is the retry and not that first offer.
	time.Sleep(6 * time.Second)
	if p.holdsGuild(w.ours.ID) || p.holdsGuild(w.theirs.ID) {
		t.Fatal("a guild was joined while every committer still refused")
	}
	// The committers can now replace the old leaf; the retries bring both in.
	staleLeafByCredential.Store(false)
	waitUntil(t, 60*time.Second, func() bool {
		return p.holdsGuild(w.ours.ID) && p.holdsGuild(w.theirs.ID)
	}, "the guilds that failed at link time were not retried")
}
