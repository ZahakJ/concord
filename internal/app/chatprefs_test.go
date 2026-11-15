package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// TestChatPrefsSyncOnlyToOwnDevices: a pin and an archive on one device show
// on the account's other device, and a second account sharing a chat with the
// account receives nothing about them.
func TestChatPrefsSyncOnlyToOwnDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	other := startServiceOn(t, ctx, t.TempDir(), boot)

	code, err := desk.InviteCode(theGuild(t, desk).ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := other.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := desk.MemberCount(theGuild(t, desk).ID)
		return n == 3
	}, "the second account never joined")

	dm, err := desk.StartDM(other.Fingerprint())
	if err != nil {
		t.Fatalf("StartDM: %v", err)
	}
	chat := dm.Channels[0].ID

	var events int
	desk.OnChatPrefs(func() { events++ })
	if err := desk.SetChatPinned(chat, true); err != nil {
		t.Fatalf("SetChatPinned: %v", err)
	}
	if err := desk.SetChatArchived(chat, true); err != nil {
		t.Fatalf("SetChatArchived: %v", err)
	}
	if events != 2 {
		t.Fatalf("a UI session heard %d chat-prefs events, want 2", events)
	}

	waitUntil(t, 30*time.Second, func() bool {
		m, err := phone.ChatPrefs()
		return err == nil && m[chat].Pinned && m[chat].Archived
	}, "the pin and the archive never reached the account's other device")

	// An unpin on the phone travels back.
	if err := phone.SetChatPinned(chat, false); err == nil {
		// The phone may not hold the DM yet; if it does, the unpin must arrive.
		waitUntil(t, 30*time.Second, func() bool {
			m, _ := desk.ChatPrefs()
			return !m[chat].Pinned
		}, "the unpin never reached the first device")
	}

	// Privacy: nothing for the other account, by any path.
	time.Sleep(3 * time.Second)
	if m, _ := other.ChatPrefs(); len(m) != 0 {
		t.Fatalf("the other account holds chat prefs: %v", m)
	}
	if m := desk.chatPrefsFor(other.host.PeerID()); m != nil {
		t.Fatalf("the hello would offer the other account our prefs: %v", m)
	}
	notes, err := desk.NotesDM()
	if err != nil {
		t.Fatalf("NotesDM: %v", err)
	}
	for _, p := range desk.ps.TopicPeers(domain.GuildMetaTopicID(notes.GroupID)) {
		if p == other.host.PeerID() {
			t.Fatal("the other account listens on the Notes meta topic")
		}
	}
	if err := desk.SetChatPinned("not-a-chat", true); err == nil {
		t.Fatal("pinned a chat that does not exist")
	}
}

// TestChatPinsAreCapped: the cap is enforced with a clear message, and a
// remote (own-device) record is taken even over it.
func TestChatPinsAreCapped(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startService(t, ctx)
	var ids []string
	for i := 0; i < MaxPinnedChats+1; i++ {
		other := startService(t, ctx)
		dm, err := svc.StartDM(other.Fingerprint())
		if err != nil {
			t.Fatalf("StartDM: %v", err)
		}
		ids = append(ids, dm.Channels[0].ID)
	}
	for _, id := range ids[:MaxPinnedChats] {
		if err := svc.SetChatPinned(id, true); err != nil {
			t.Fatalf("pin: %v", err)
		}
	}
	if err := svc.SetChatPinned(ids[MaxPinnedChats], true); err == nil {
		t.Fatal("pinned past the cap")
	}
	// Reorder: reverse the pins.
	rev := make([]string, 0, MaxPinnedChats)
	for i := MaxPinnedChats - 1; i >= 0; i-- {
		rev = append(rev, ids[i])
	}
	if err := svc.SetChatPinOrder(rev); err != nil {
		t.Fatalf("SetChatPinOrder: %v", err)
	}
	m, _ := svc.ChatPrefs()
	if !(m[rev[0]].PinKey < m[rev[1]].PinKey) {
		t.Fatalf("order not applied: %+v", m)
	}
	// Leaving drops the record.
	if err := svc.SetChatPinned(ids[0], false); err != nil {
		t.Fatal(err)
	}
}
