package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/ZahakJ/concord/internal/identity"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
)

// TestContactDeviceLinkIsSilentAndChained: a contact links a second device
// through their account (the account key signs it, after their own device
// approved it). Our client checks that chain and stays quiet: nothing in the
// DM, their verification kept, and the device in their details. The scan is
// the same one the heal loop runs.
func TestContactDeviceLinkIsSilentAndChained(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	friendDir := t.TempDir()
	friend := startServiceInDir(t, ctx, friendDir)
	me := startService(t, ctx)

	g, err := friend.CreateGuild("shared")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := friend.InviteCode(g.ID)
	if _, err := me.JoinViaInvite(code); err != nil {
		t.Fatalf("join shared: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, friend, me)
	if err := me.VerifyFingerprint(friend.Fingerprint()); err != nil {
		t.Fatalf("VerifyFingerprint: %v", err)
	}
	dm, err := friend.StartDM(me.Fingerprint())
	if err != nil {
		t.Fatalf("StartDM: %v", err)
	}
	waitMemberCount(t, 20*time.Second, dm.ID, 2, friend, me)
	dmChannel := dm.Channels[0].ID
	me.noteDeviceLeaves() // seeds: their first device is not news

	linkCode, err := friend.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	phoneDir := t.TempDir()
	res, err := RedeemLink(ctx, phoneDir, linkCode, "test-pass", "")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := startServiceInDir(t, ctx, phoneDir)
	for _, ic := range res.GuildInvites {
		_, _ = phone.JoinViaInvite(ic)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		me.noteDeviceLeaves()
		if n, _ := me.ContactDevices(friend.Fingerprint()); n >= 2 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	n, added := me.ContactDevices(friend.Fingerprint())
	if n != 2 || len(added) != 1 {
		t.Fatalf("their details hold %d devices, %d added; want 2 and 1", n, len(added))
	}
	if added[0].Key != hex.EncodeToString(phone.id.DevicePublicKey()) {
		t.Fatalf("the added device is %s, want the phone's key", added[0].Key)
	}
	me.noteDeviceLeaves()
	for _, kind := range []string{"device", "keychange", "system"} {
		if c := countKind(t, me, dmChannel, kind); c != 0 {
			t.Fatalf("a linked device wrote %d %q rows into the DM, want none", c, kind)
		}
	}
	if !me.VerifiedFingerprints()[friend.Fingerprint()] {
		t.Fatal("a linked device dropped the contact's verification")
	}
}

// strangerEngine is the MLS engine with one more member in one group: a key
// a test puts in a 1:1 that neither of its people's accounts signed.
type strangerEngine struct {
	mls.Engine
	group []byte
	extra [][]byte
}

func (e strangerEngine) Members(ctx context.Context, gid mls.GroupID) ([][]byte, error) {
	m, err := e.Engine.Members(ctx, gid)
	if err == nil && bytes.Equal(gid, e.group) {
		m = append(append([][]byte(nil), m...), e.extra...)
	}
	return m, err
}

// TestAKeyOffTheChainInADMIsSaidOnce: a member in a 1:1 whose credential does
// not chain to the contact's account is the change that matters, and the DM
// says it, once per key. Two strangers: a bare key, and a certificate that
// names the contact's account but was signed by some other key, which must
// not pass for theirs.
func TestAKeyOffTheChainInADMIsSaidOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	friend := startService(t, ctx)
	me := startService(t, ctx)
	g, err := friend.CreateGuild("shared")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := friend.InviteCode(g.ID)
	if _, err := me.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, friend, me)
	dm, err := friend.StartDM(me.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	waitMemberCount(t, 20*time.Second, dm.ID, 2, friend, me)
	deadline := time.Now().Add(20 * time.Second)
	for me.findPeerDM(friend.Fingerprint()) == nil && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	held := me.findPeerDM(friend.Fingerprint())
	if held == nil {
		t.Fatal("no DM with the friend on my side")
	}
	me.noteDeviceLeaves()
	if c := countKind(t, me, held.Channels[0].ID, "keychange"); c != 0 {
		t.Fatalf("an ordinary DM raised %d notices", c)
	}

	stranger, _ := identity.Generate()
	forger, _ := identity.Generate()
	forged := forger.IssueDeviceCertFor(stranger.PublicKey(), "Pixel", time.Now().Unix())
	forged.AccountPub = append([]byte(nil), friend.PublicKey()...) // claims the friend; signed by the forger
	me.mls = strangerEngine{Engine: me.mls, group: held.GroupID, extra: [][]byte{append([]byte(nil), stranger.PublicKey()...), forged.Marshal()}}

	me.noteDeviceLeaves()
	me.noteDeviceLeaves()
	ch := held.Channels[0].ID
	if c := countKind(t, me, ch, "keychange"); c != 2 {
		t.Fatalf("two keys off the chain raised %d notices, want 2 (one each)", c)
	}
	msgs, _ := me.Messages(ch, 0)
	for _, m := range msgs {
		if m.Kind == "keychange" && !strings.Contains(m.Content, "isn't linked to") {
			t.Fatalf("notice reads %q", m.Content)
		}
	}
	if n, _ := me.ContactDevices(friend.Fingerprint()); n != 1 {
		t.Fatalf("a forged certificate counted as the friend's device (%d devices)", n)
	}
}

// countKind returns how many messages of a kind a service holds in a channel.
func countKind(t *testing.T, s *Service, channelID, kind string) int {
	t.Helper()
	msgs, err := s.Messages(channelID, 0)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	n := 0
	for _, m := range msgs {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

// TestDeviceRosterSurvivesRestart: the roster is what makes "new" mean new. If
// it didn't persist, every launch would announce every device again.
func TestDeviceRosterSurvivesRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	s.mu.Lock()
	s.devices["fpr-friend-0"] = map[string]bool{"aa": true, "bb": true}
	s.mu.Unlock()
	s.persistDeviceRoster()
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again := startServiceInDir(t, ctx, dir)
	again.mu.RLock()
	set := again.devices["fpr-friend-0"]
	again.mu.RUnlock()
	if len(set) != 2 || !set["aa"] || !set["bb"] {
		t.Fatalf("device roster after restart = %v, want {aa,bb}", set)
	}
}

// A "device" notice must stay local: the sync ingest already drops every kind
// except "" and "system", which is what keeps our observation off their screen.
// Pinning it here means a future kind filter can't quietly start relaying it.
func TestDeviceNoticeIsNotSyncable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	notes, err := s.NotesDM()
	if err != nil {
		t.Fatalf("NotesDM: %v", err)
	}
	channel := notes.Channels[0].ID

	m, err := domain.NewMessage(channel, s.PublicKey(), "linked another device")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	m.Kind = "device"
	feedSync(t, s, notes.ID, channel, m)
	if n := countKind(t, s, channel, "device"); n != 0 {
		t.Fatalf("a synced device notice was accepted (%d rows)", n)
	}

	// Control: the same row as an ordinary message DOES land, so the assertion
	// above is about the kind and not about a broken harness.
	m.Kind = ""
	feedSync(t, s, notes.ID, channel, m)
	if n := countKind(t, s, channel, ""); n == 0 {
		t.Fatal("the sync harness rejected an ordinary message too")
	}
}

// feedSync pushes one message row through the sync ingest — the path a peer's
// served history takes into the store.
func feedSync(t *testing.T, s *Service, guildID, channelID string, m domain.Message) {
	t.Helper()
	s.mu.RLock()
	g := *s.guilds[guildID]
	s.mu.RUnlock()
	raw, err := json.Marshal(syncPayload{Guild: g, Messages: map[string][]domain.Message{channelID: {m}}})
	if err != nil {
		t.Fatalf("marshal sync payload: %v", err)
	}
	ct, err := s.mls.Encrypt(s.ctx, g.GroupID, raw)
	if err != nil {
		t.Fatalf("encrypt sync payload: %v", err)
	}
	s.applySyncPayload(guildID, g.GroupID, ct, s.Fingerprint())
}

func TestDeviceLabelIsCleanedAndCapped(t *testing.T) {
	if got := cleanDeviceLabel(strings.Repeat("x", 100)); len(got) != 32 {
		t.Errorf("label length %d, want 32", len(got))
	}
	if got := cleanDeviceLabel("Phone\n\x00 two"); got != "Phone two" {
		t.Errorf("label %q", got)
	}
}

// Old "added a new device" lines go at start, and only they do.
func TestOldNewDeviceLinesAreRemovedAtStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	notes, err := s.NotesDM()
	if err != nil {
		t.Fatal(err)
	}
	ch := notes.Channels[0].ID
	for _, kind := range []string{"device", ""} {
		m, _ := domain.NewMessage(ch, s.PublicKey(), "row of kind "+kind)
		m.Kind = kind
		if _, err := s.store.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again := startServiceInDir(t, ctx, dir)
	if c := countKind(t, again, ch, "device"); c != 0 {
		t.Fatalf("%d old device lines survived a start", c)
	}
	if c := countKind(t, again, ch, ""); c == 0 {
		t.Fatal("an ordinary message went with them")
	}
}

func TestFoldDevicesDedupe(t *testing.T) {
	acct, _ := identity.Generate()
	mk := func(name string, at int64) []byte {
		dev, _ := identity.Generate()
		return acct.IssueDeviceCertFor(dev.PublicKey(), name, at).Marshal()
	}
	known := map[string]map[string]bool{}
	old := mk("Desktop", 100)
	seen := func(creds ...[]byte) map[string]map[string][]byte {
		m := map[string][]byte{}
		for _, c := range creds {
			m[hex.EncodeToString(mailboxKeyOf(c))] = c
		}
		return map[string]map[string][]byte{"fpr": m}
	}
	// First sight seeds silently.
	if fresh, ch := foldDevices(known, seen(old)); len(fresh) != 0 || !ch {
		t.Fatalf("first sight: fresh=%d changed=%v, want 0,true", len(fresh), ch)
	}
	// Nothing new: no notice, no change.
	if fresh, ch := foldDevices(known, seen(old)); len(fresh) != 0 || ch {
		t.Fatalf("rescan: fresh=%d changed=%v, want 0,false", len(fresh), ch)
	}
	// Two devices arrive together: one notice each, with their own cert data.
	a, b := mk("iPhone", 200), mk("iPad", 300)
	fresh, _ := foldDevices(known, seen(old, a, b))
	if len(fresh) != 2 {
		t.Fatalf("got %d notices, want 2", len(fresh))
	}
	names := map[string]int64{}
	for _, d := range fresh {
		names[d.name] = d.issued
		if string(d.account) != string(acct.PublicKey()) {
			t.Error("notice carries the wrong account key")
		}
	}
	if names["iPhone"] != 200 || names["iPad"] != 300 {
		t.Errorf("cert data lost: %v", names)
	}
	// Exactly once.
	if fresh, _ := foldDevices(known, seen(old, a, b)); len(fresh) != 0 {
		t.Fatalf("repeat scan re-announced %d devices", len(fresh))
	}
}

// A scan that read the roster before a commit can fold after one that read it
// after: the late, stale fold must neither re-announce nor forget the device.
func TestFoldDevicesStaleAfterFresh(t *testing.T) {
	acct, _ := identity.Generate()
	mk := func(name string) []byte {
		dev, _ := identity.Generate()
		return acct.IssueDeviceCertFor(dev.PublicKey(), name, 1).Marshal()
	}
	old, phone := mk("Desktop"), mk("iPhone")
	scan := func(creds ...[]byte) map[string]map[string][]byte {
		m := map[string][]byte{}
		for _, c := range creds {
			m[hex.EncodeToString(mailboxKeyOf(c))] = c
		}
		return map[string]map[string][]byte{"fpr": m}
	}
	known := map[string]map[string]bool{}
	foldDevices(known, scan(old))
	total := 0
	f, _ := foldDevices(known, scan(old, phone)) // fresh scan
	total += len(f)
	f, _ = foldDevices(known, scan(old)) // stale scan lands late
	total += len(f)
	f, _ = foldDevices(known, scan(old, phone)) // next scan
	total += len(f)
	if total != 1 {
		t.Fatalf("got %d notices, want exactly 1", total)
	}
}
