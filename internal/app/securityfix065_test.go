package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// A kind that names something this device writes for itself — the notice
// that a key off a contact's chain is in a 1:1, the old device lines, or any
// kind no build sends — is not taken from the wire. A peer that could send
// one would be writing Concord's own security notice in its own words.
func TestLocalKindsAreNotTakenFromTheWire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("wire")
	if err != nil {
		t.Fatal(err)
	}
	ch := g.Channels[0].ID
	send := func(kind string) bool {
		m := domain.Message{ID: domain.NewID(), ChannelID: ch, Sender: s.PublicKey(), Name: "x", Kind: kind,
			Content: "Concord: your contact's key changed, send your phrase to …", Sent: time.Now().UTC()}
		m.Sig = s.signMessage(m)
		s.deliverMessage(g.ID, m, s.myCredential, nil, false)
		_, ok, _ := s.store.MessageByID(m.ID)
		return ok
	}
	if !send("") {
		t.Fatal("an ordinary message was not taken: the test is not reaching the store")
	}
	for _, kind := range []string{"keychange", "device", "bogus", "app"} {
		if send(kind) {
			t.Errorf("a %q message from the wire was stored", kind)
		}
	}
}

// One sender's action id is not another's. A member who replays an id they
// saw (or guessed) under their own name must not use up the action that id
// stands for when its real sender's copy arrives.
func TestAnActionIDIsHeldPerSender(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("acts")
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.SendMessage(g.Channels[0].ID, "the target", "", "")
	if err != nil {
		t.Fatal(err)
	}
	alice, _ := identity.Generate()
	mallory, _ := identity.Generate()
	if err := s.applyReaction("act-1", target.ID, "👍", mallory.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := s.applyReaction("act-1", target.ID, "👍", alice.PublicKey()); err != nil {
		t.Fatal(err)
	}
	row, _, _ := s.store.MessageByID(target.ID)
	got := map[string]bool{}
	for _, f := range row.Reactions["👍"] {
		got[f] = true
	}
	if !got[alice.Fingerprint()] {
		t.Fatalf("Alice's reaction was burned by Mallory's copy of its id: %v", row.Reactions)
	}
	// Still once per sender: her own copy again changes nothing.
	_ = s.applyReaction("act-1", target.ID, "👍", alice.PublicKey())
	row, _, _ = s.store.MessageByID(target.ID)
	if n := len(row.Reactions["👍"]); n != 2 {
		t.Fatalf("a re-delivered reaction toggled again: %v", row.Reactions)
	}
	// An edit id another sender used first is still the author's to apply.
	_ = s.applyEdit("edit-1", target.ID, "mallory's words", mallory.PublicKey(), nil, "")
	if err := s.applyEdit("edit-1", target.ID, "my words", s.PublicKey(), nil, ""); err != nil {
		t.Fatal(err)
	}
	if row, _, _ := s.store.MessageByID(target.ID); row.Content != "my words" {
		t.Fatalf("the author's edit was burned: %q", row.Content)
	}
}

// A contact's devices in their details are the ones on their account now:
// a device that left, or that their account unlinked, is not counted. A
// verified contact who adds a device carries a mark, outside the chat, until
// they are verified again.
func TestContactDevicesAreCurrentAndANewOneMarksAVerifiedContact(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("devices")
	if err != nil {
		t.Fatal(err)
	}
	acct, _ := identity.Generate()
	cert := func(name string) ([]byte, []byte) {
		dev, _ := identity.Generate()
		return acct.IssueDeviceCertFor(dev.PublicKey(), name, time.Now().Unix()).Marshal(), dev.PublicKey()
	}
	desk, _ := cert("Desktop")
	phone, phoneKey := cert("Pixel")
	tablet, _ := cert("Tab")
	// The contact's devices, as leaves in our guild's group. Passed to the
	// scan rather than swapped into s.mls, which the service's own loops read.
	set := func(leaves ...[]byte) {
		s.scanDeviceLeaves(func(gid []byte) ([][]byte, error) {
			m, err := s.mls.Members(s.ctx, gid)
			if err == nil && string(gid) == string(g.GroupID) {
				m = append(append([][]byte(nil), m...), leaves...)
			}
			return m, err
		})
	}
	fpr := acct.Fingerprint()

	set(desk)
	if err := s.VerifyFingerprint(fpr); err != nil {
		t.Fatal(err)
	}
	if s.NewDeviceSinceVerified(fpr) {
		t.Fatal("verifying marked the contact at once")
	}
	set(desk, phone)
	if n, added := s.ContactDevices(fpr); n != 2 || len(added) != 1 {
		t.Fatalf("after the phone: %d devices, %d added; want 2, 1", n, len(added))
	}
	if !s.NewDeviceSinceVerified(fpr) {
		t.Fatal("a device linked since the contact was verified left no mark")
	}
	if err := s.VerifyFingerprint(fpr); err != nil {
		t.Fatal(err)
	}
	if s.NewDeviceSinceVerified(fpr) {
		t.Fatal("verifying again did not clear the mark")
	}

	// Their account unlinks the phone: it is not one of theirs any more.
	s.deviceMu.Lock()
	if s.peerRevoked == nil {
		s.peerRevoked = map[string]bool{}
	}
	s.peerRevoked[peerRevKey(acct.PublicKey(), phoneKey)] = true
	s.deviceMu.Unlock()
	set(desk, phone)
	if n, added := s.ContactDevices(fpr); n != 1 || len(added) != 0 {
		t.Fatalf("an unlinked device still counted: %d devices, %d added", n, len(added))
	}
	// A device that left every group we share is not counted either.
	set(desk, tablet)
	set(desk)
	if n, _ := s.ContactDevices(fpr); n != 1 {
		t.Fatalf("a device no longer in any shared group counted: %d", n)
	}

	// An unverified contact gets no mark, whatever they link.
	other, _ := identity.Generate()
	d1, _ := identity.Generate()
	d2, _ := identity.Generate()
	set(desk, other.IssueDeviceCertFor(d1.PublicKey(), "a", 1).Marshal())
	set(desk, other.IssueDeviceCertFor(d1.PublicKey(), "a", 1).Marshal(), other.IssueDeviceCertFor(d2.PublicKey(), "b", 2).Marshal())
	if s.NewDeviceSinceVerified(other.Fingerprint()) {
		t.Fatal("an unverified contact was marked")
	}
}

// The notices for keys off a contact's chain are bounded per 1:1: a peer that
// can put keys into a conversation cannot fill it, or this device's settings,
// with lines.
func TestStrangerNoticesAreCappedPerDM(t *testing.T) {
	noted := map[string]bool{}
	n := 0
	for i := 0; i < 50; i++ {
		if admitStranger(noted, "dm-1", fmt.Sprintf("key-%d", i)) {
			n++
		}
	}
	if n != strangerNoticesPerDM {
		t.Fatalf("%d notices in one DM, want the cap %d", n, strangerNoticesPerDM)
	}
	if !admitStranger(noted, "dm-2", "key-0") {
		t.Fatal("another DM's first notice was refused")
	}
	if admitStranger(noted, "dm-2", "key-0") {
		t.Fatal("the same key in the same DM was noted twice")
	}
}
