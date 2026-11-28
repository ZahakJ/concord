package app

import (
	"context"
	"fmt"
	"strings"
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
	if !s.NewDeviceSinceVerifiedAmong(fpr, s.VerifiedFingerprints()) || s.NewDeviceSinceVerifiedAmong(fpr, map[string]bool{}) {
		t.Fatal("the mark does not follow the verified set it is handed")
	}
	if err := s.VerifyFingerprint(fpr); err != nil {
		t.Fatal(err)
	}
	if s.NewDeviceSinceVerified(fpr) {
		t.Fatal("verifying again did not clear the mark")
	}
	// The same by peer id (the console's verify): it is the same act, so it
	// takes the same snapshot and clears the same mark.
	set(desk, phone, tablet)
	if !s.NewDeviceSinceVerified(fpr) {
		t.Fatal("the tablet left no mark")
	}
	if err := s.store.RecordContact("peer-of-acct", fpr); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyContact("peer-of-acct"); err != nil {
		t.Fatal(err)
	}
	if s.NewDeviceSinceVerified(fpr) {
		t.Fatal("verifying by peer id did not clear the mark")
	}
	set(desk, phone)

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

	// The cap is on the keys still there: once they leave the 1:1, a new one
	// is told about again.
	if !pruneStrangers(noted, "dm-1", map[string]bool{"key-0": true}) {
		t.Fatal("pruning keys that left reported no change")
	}
	if !admitStranger(noted, "dm-1", "key-new") {
		t.Fatal("a new key was refused after the earlier ones left the DM")
	}
	if !noted["dm-2|key-0"] {
		t.Fatal("pruning one DM touched another's notes")
	}
}

// The device count and the new-device mark survive a group this device
// cannot read just now, and a restart before the first scan: the last live
// set carries forward rather than reading as "no devices".
func TestLiveDevicesCarryOverAnUnreadableGroupAndARestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("carry")
	if err != nil {
		t.Fatal(err)
	}
	acct, _ := identity.Generate()
	leaf := func(name string) []byte {
		dev, _ := identity.Generate()
		return acct.IssueDeviceCertFor(dev.PublicKey(), name, time.Now().Unix()).Marshal()
	}
	desk, phone := leaf("Desktop"), leaf("Pixel")
	fpr := acct.Fingerprint()
	scan := func(fail bool, leaves ...[]byte) {
		s.scanDeviceLeaves(func(gid []byte) ([][]byte, error) {
			if string(gid) == string(g.GroupID) {
				if fail {
					return nil, fmt.Errorf("unreadable")
				}
				m, err := s.mls.Members(s.ctx, gid)
				return append(append([][]byte(nil), m...), leaves...), err
			}
			return s.mls.Members(s.ctx, gid)
		})
	}
	scan(false, desk)
	if err := s.VerifyFingerprint(fpr); err != nil {
		t.Fatal(err)
	}
	scan(false, desk, phone)
	if n, _ := s.ContactDevices(fpr); n != 2 || !s.NewDeviceSinceVerified(fpr) {
		t.Fatalf("setup: %d devices, marked %v", n, s.NewDeviceSinceVerified(fpr))
	}
	scan(true)
	if n, _ := s.ContactDevices(fpr); n != 2 {
		t.Fatalf("an unreadable group dropped the count to %d", n)
	}
	if !s.NewDeviceSinceVerified(fpr) {
		t.Fatal("an unreadable group cleared the new-device mark")
	}
	// A restart: nothing in memory until the stored set is loaded.
	s.mu.Lock()
	s.deviceLive, s.deviceLiveGroups = nil, nil
	s.mu.Unlock()
	s.loadDeviceLive()
	if n, _ := s.ContactDevices(fpr); n != 2 || !s.NewDeviceSinceVerified(fpr) {
		t.Fatalf("after a restart: %d devices, marked %v", n, s.NewDeviceSinceVerified(fpr))
	}
	// Read again, the group's real leaves win.
	scan(false, desk)
	if n, _ := s.ContactDevices(fpr); n != 1 {
		t.Fatalf("a readable group did not replace the carried set: %d", n)
	}
}

// The announcement gate and slow mode hold for every row a member can post,
// not only plain messages: a "system", "guest", "invite" or "call-missed" row
// is as visible in the channel, so it was a way round both.
func TestAnnouncementAndSlowModeHoldForEveryPostedKind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g := guildWith(t, owner, member)
	ch := g.Channels[0].ID
	owner.mu.Lock()
	owner.guilds[g.ID].Channels[0].Type = "announcement"
	owner.mu.Unlock()

	n := 0
	post := func(kind string, at time.Time) bool {
		n++
		m := domain.Message{ID: domain.NewID(), ChannelID: ch, Sender: member.PublicKey(), Name: "Sam", Kind: kind,
			Content: fmt.Sprintf("post %d", n), Sent: at}
		m.Sig = member.signMessage(m)
		owner.deliverMessage(g.ID, m, member.myCredential, nil, false)
		_, ok, _ := owner.store.MessageByID(m.ID)
		return ok
	}
	kinds := []string{"", "system", "guest", "invite", "call-missed"}
	for _, k := range kinds {
		if post(k, time.Now().UTC()) {
			t.Errorf("a %q row from a member without the right reached an announcement channel", k)
		}
	}

	// Slow mode: a plain channel again, one post a minute.
	owner.mu.Lock()
	owner.guilds[g.ID].Channels[0].Type = ""
	st := owner.govState[g.ID]
	if st.SlowMode == nil {
		st.SlowMode = map[string]int64{}
	}
	st.SlowMode[ch] = 60
	owner.govState[g.ID] = st
	owner.mu.Unlock()
	base := time.Now().UTC().Add(time.Hour)
	if !post("", base) {
		t.Fatal("the first post under slow mode was refused: the test is not reaching the store")
	}
	if post("", base.Add(time.Second)) {
		t.Error("a second message inside the interval was kept")
	}
	// Each kind is paced on its own, so a join notice does not use up the
	// slot for the first message; a second row of a kind is held.
	for i, k := range kinds[1:] {
		at := base.Add(time.Duration(10*i+2) * time.Second)
		if !post(k, at) {
			t.Errorf("the first %q row under slow mode was refused", k)
		}
		if post(k, at.Add(time.Second)) {
			t.Errorf("a %q row skipped slow mode", k)
		}
	}
}

// Only a member's own messages teach their name. A system row from an event
// reminder carries the guild's name and a guest row the guest's, so a name
// learned from either would then show as that member's own.
func TestOnlyPlainMessagesTeachAName(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("hints")
	if err != nil {
		t.Fatal(err)
	}
	ch := g.Channels[0].ID
	who, _ := identity.Generate()
	save := func(kind, name string) {
		m := domain.Message{ID: domain.NewID(), ChannelID: ch, Sender: who.PublicKey(), Name: name, Kind: kind,
			Content: "x", Sent: time.Now().UTC()}
		if _, err := s.store.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	save("system", "Concord")
	save("guest", "Sam")
	if _, err := s.Messages(ch, 50); err != nil {
		t.Fatal(err)
	}
	if n := s.ProfileName(who.Fingerprint()); n != "" {
		t.Fatalf("a %q name was learned from a system or guest row", n)
	}
	save("", "Bashir")
	_, _ = s.Messages(ch, 50)
	if n := s.ProfileName(who.Fingerprint()); n != "Bashir" {
		t.Fatalf("a plain message's name was not learned: %q", n)
	}
}

// The Markdown export names a system row by who sent it, as the app does,
// not by the name the row asserts.
func TestExportNamesASystemRowBySender(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("export")
	if err != nil {
		t.Fatal(err)
	}
	who, _ := identity.Generate()
	m := domain.Message{ID: domain.NewID(), ChannelID: g.Channels[0].ID, Sender: who.PublicKey(), Name: "Concord",
		Kind: "system", Content: "Your keys changed", Sent: time.Now().UTC()}
	if _, err := s.store.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	md, err := s.ExportMarkdown(g.ID, g.Channels[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "Concord Your keys changed") || !strings.Contains(md, who.Fingerprint()) {
		t.Fatalf("the export trusted the row's own name:\n%s", md)
	}
}
