package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/store"
)

// ZW-06 / SCC-03. The own-device lanes on the MLS meta topic (hides, the
// receipts and typing settings, read markers, and a sibling's member_left)
// are taken only from a current device of this account. A device unlinked
// here whose leaf is still seated speaks for the account no more, exactly as
// on the hello lanes.
func TestAnUnlinkedDeviceStillSeatedChangesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, ch, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	shared, ok := desk.channelGuild(ch)
	if !ok {
		t.Fatal("setup: no guild for the channel")
	}
	var notes struct {
		id    string
		group []byte
	}
	waitUntil(t, 60*time.Second, func() bool {
		d, ok1 := desk.findNotesDM()
		p, ok2 := phone.findNotesDM()
		if !ok1 || !ok2 || d.ID != p.ID || memberCount(desk, d.ID) != 2 {
			return false
		}
		notes.id, notes.group = d.ID, d.GroupID
		return true
	}, "setup: the two devices never shared one Notes")

	frame := func(groupID []byte, m guildMeta) []byte {
		t.Helper()
		b, _ := json.Marshal(m)
		ct, err := phone.mls.Encrypt(ctx, groupID, b)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		return ct
	}
	hide := func(id string) guildMeta {
		return guildMeta{Type: "hide_marker", Hides: []store.HideRec{{Channel: ch, ID: id, Hidden: true, At: time.Now().UnixMilli()}}}
	}
	m1, err := desk.SendMessage(ch, "one", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := desk.SendMessage(ch, "two", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// While the phone is a current device, its hide is taken.
	desk.receiveGuildMeta(notes.id, notes.group, frame(notes.group, hide(m1.ID)))
	if !desk.store.IsHidden(m1.ID) {
		t.Fatal("setup: a hide from a current own device was not taken")
	}

	// Unlinked here, its leaf left seated (as where no committer could remove
	// it): this device holds its revocation, nothing more.
	key := hex.EncodeToString(devicePubOf(t, phone))
	desk.deviceMu.Lock()
	desk.revokedDevices[key] = true
	delete(desk.deviceAccounts, key)
	desk.deviceMu.Unlock()
	if memberCount(desk, notes.id) != 2 {
		t.Fatal("setup: the phone's leaf is no longer seated in Notes")
	}

	on := true
	now := time.Now().Add(time.Second).UnixMilli()
	desk.receiveGuildMeta(notes.id, notes.group, frame(notes.group, hide(m2.ID)))
	desk.receiveGuildMeta(notes.id, notes.group, frame(notes.group, guildMeta{Type: "typing_pref", TypingOn: &on, UpdatedAt: now}))
	desk.receiveGuildMeta(notes.id, notes.group, frame(notes.group, guildMeta{Type: "receipts_pref", Receipts: &receiptsPref{On: true, At: now}}))
	desk.receiveGuildMeta(notes.id, notes.group, frame(notes.group, guildMeta{Type: "read_marker", Markers: map[string]int64{"elsewhere": now}}))
	sharedGroup := rhGroup(desk, shared)
	desk.receiveGuildMeta(shared, sharedGroup, frame(sharedGroup, guildMeta{Type: "member_left", Fingerprint: phone.Fingerprint()}))
	time.Sleep(2 * time.Second)

	if desk.store.IsHidden(m2.ID) {
		t.Error("an unlinked device hid a message")
	}
	if desk.TypingEnabled() {
		t.Error("an unlinked device turned typing on")
	}
	if desk.ReadReceiptsEnabled() {
		t.Error("an unlinked device turned read receipts on")
	}
	if at, _ := desk.store.ReadMark("elsewhere"); at != 0 {
		t.Error("an unlinked device moved a read mark")
	}
	desk.mu.RLock()
	_, still := desk.guilds[shared]
	desk.mu.RUnlock()
	if !still {
		t.Error("an unlinked device's member_left made this device leave the guild")
	}
}
