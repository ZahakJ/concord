package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/ZahakJ/concord/internal/identity"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// TestContactDeviceLinkRaisesNotice is the acceptance test for the missing half
// of safety numbers: a contact links a second device, our shared DM says so,
// and their verification is left alone (nothing about their identity changed).
func TestContactDeviceLinkRaisesNotice(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	friendDir := t.TempDir()
	friend := startServiceInDir(t, ctx, friendDir)
	me := startService(t, ctx)

	// A shared guild connects them; a DM is where the notice belongs.
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

	// Seed: their existing device is not news.
	me.noteDeviceLeaves()
	if n := countKind(t, me, dmChannel, "device"); n != 0 {
		t.Fatalf("first sight of a contact raised %d device notices, want 0", n)
	}

	// The friend links a phone, which joins every group they're in — including
	// our DM, which is exactly why it is worth telling us about.
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
		_, _ = phone.JoinViaInvite(ic) // guild + DM; a failure shows up as no notice
	}

	// The notice is raised on the same scan the heal loop runs.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		me.noteDeviceLeaves()
		if countKind(t, me, dmChannel, "device") > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if n := countKind(t, me, dmChannel, "device"); n != 1 {
		t.Fatalf("a linked device raised %d notices in the DM, want exactly 1", n)
	}

	// Idempotent: a device already reported is not reported again every tick.
	me.noteDeviceLeaves()
	me.noteDeviceLeaves()
	if n := countKind(t, me, dmChannel, "device"); n != 1 {
		t.Fatalf("repeat scans raised the notice %d times, want 1", n)
	}

	// Their safety number did not change — the account key signed the new
	// device — so the checkmark must survive. Un-verifying here would be a lie
	// and would train people to click through the alert that matters.
	if !me.VerifiedFingerprints()[friend.Fingerprint()] {
		t.Fatal("a linked device dropped the contact's verification")
	}

	// The notice is ours: it must not travel back to them through sync.
	if n := countKind(t, friend, dmChannel, "device"); n != 0 {
		t.Fatalf("our local device notice reached the other side (%d rows)", n)
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

func TestDeviceNoticeText(t *testing.T) {
	issued := time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local).Unix()
	cases := []struct {
		who, dev string
		at       int64
		want     string
	}{
		{"Ann", "iPhone", issued, "Ann added a new device (iPhone, 11-14). If this wasn't Ann, verify."},
		{"Ann", "", issued, "Ann added a new device (11-14). If this wasn't Ann, verify."},
		{"Ann", "iPhone", 0, "Ann added a new device (iPhone). If this wasn't Ann, verify."},
		{"Ann", "", 0, "Ann added a new device. If this wasn't Ann, verify."},
		{"", "Pixel 7", issued, "They added a new device (Pixel 7, 11-14). If this wasn't them, verify."},
		{"Ann", "Phone\n\x00 two", 0, "Ann added a new device (Phone two). If this wasn't Ann, verify."},
	}
	for _, c := range cases {
		if got := deviceNoticeText(c.who, c.dev, c.at); got != c.want {
			t.Errorf("deviceNoticeText(%q,%q,%d) = %q, want %q", c.who, c.dev, c.at, got, c.want)
		}
	}
	long := strings.Repeat("x", 100)
	if got := cleanDeviceLabel(long); len(got) != 32 {
		t.Errorf("label length %d, want 32", len(got))
	}
	for _, banned := range []string{"linked another", "now sign in"} {
		if strings.Contains(deviceNoticeText("Ann", "x", 1), banned) {
			t.Errorf("notice still carries old jargon %q", banned)
		}
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
