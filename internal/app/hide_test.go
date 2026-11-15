package app

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/store"
)

func hasMsg(s *Service, ch, id string) bool {
	msgs, err := s.Messages(ch, 0)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if m.ID == id {
			return true
		}
	}
	return false
}

// TestDeleteForMeSyncsToMyOtherDeviceAndNoOneElse: hiding a message on one
// device hides it on the account's other device, over the Notes self-group; a
// second account in the same channel keeps the message and is told nothing.
func TestDeleteForMeSyncsToMyOtherDeviceAndNoOneElse(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, ch, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	// A second account joins the same guild.
	bob := startServiceOn(t, ctx, t.TempDir(), boot)
	g, ok := desk.channelGuild(ch)
	if !ok {
		t.Fatal("no guild for channel")
	}
	code, err := desk.InviteCode(g)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := bob.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 45*time.Second, 3, desk, bob)

	var bobEvents atomic.Int32
	bob.OnHidden(func(string, string, bool) { bobEvents.Add(1) })

	msg, err := bob.SendMessage(ch, "hide me on my devices only", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		return hasMsg(desk, ch, msg.ID) && hasMsg(phone, ch, msg.ID)
	}, "the message never reached both devices")
	var phoneEvents atomic.Int32
	phone.OnHidden(func(_, id string, hidden bool) {
		if id == msg.ID && hidden {
			phoneEvents.Add(1)
		}
	})

	if err := desk.HideMessage(ch, msg.ID); err != nil {
		t.Fatalf("HideMessage: %v", err)
	}
	if hasMsg(desk, ch, msg.ID) {
		t.Fatal("the hiding device still lists the message")
	}
	waitUntil(t, 30*time.Second, func() bool { return !hasMsg(phone, ch, msg.ID) },
		"the hide never reached the account's other device")
	if phoneEvents.Load() == 0 {
		t.Fatal("the other device's UIs were not told")
	}
	// The other account keeps it, and heard nothing.
	time.Sleep(3 * time.Second)
	if !hasMsg(bob, ch, msg.ID) {
		t.Fatal("the other account lost the message")
	}
	if bob.store.IsHidden(msg.ID) || bobEvents.Load() != 0 {
		t.Fatal("the other account was told about a private hide")
	}

	// Undo travels too.
	if err := desk.UnhideMessage(ch, msg.ID); err != nil {
		t.Fatalf("UnhideMessage: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool { return hasMsg(phone, ch, msg.ID) },
		"the undo never reached the other device")
}

// TestHideOpsOnlyComeFromMyOwnDevices: a guild member can neither hide a
// message for us over the guild's meta topic nor smuggle ops in on a hello.
func TestHideOpsOnlyComeFromMyOwnDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g, err := owner.CreateGuild("Private Hiding")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)
	ch := g.Channels[0].ID
	msg, err := owner.SendMessage(ch, "mine", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// A hide_marker forged by a member into the guild's own meta topic.
	payload, _ := json.Marshal(guildMeta{Type: "hide_marker", Hides: []store.HideRec{{Channel: ch, ID: msg.ID, Hidden: true, At: time.Now().UnixMilli()}}})
	ct, err := member.mls.Encrypt(ctx, g.GroupID, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	owner.receiveGuildMeta(g.ID, g.GroupID, ct)
	if owner.store.IsHidden(msg.ID) {
		t.Fatal("a guild member hid a message on the owner's devices")
	}

	// And on a hello.
	req, _ := json.Marshal(helloFrame{
		Credential: member.myCredential,
		Hides:      []store.HideRec{{Channel: ch, ID: msg.ID, Hidden: true, At: time.Now().UnixMilli()}},
	})
	owner.answered.release(member.host.PeerID())
	if _, err := owner.handleHello(ctx, member.host.PeerID(), req); err != nil {
		t.Fatalf("handleHello: %v", err)
	}
	time.Sleep(time.Second)
	if owner.store.IsHidden(msg.ID) {
		t.Fatal("a guild member's hello hid a message on the owner's devices")
	}
}

// TestAnOfflineDeviceCatchesUpOnHides: the phone is stopped while the desktop
// hides X, undoes an earlier hide of Y and hides Z. When the phone is back, X
// and Z are hidden and Y is visible: state is exchanged on the hello, with the
// unhide kept as a tombstone, so nothing is replayed and nothing resurrects.
func TestAnOfflineDeviceCatchesUpOnHides(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	phoneDir := t.TempDir()
	desk, phone, ch, _ := linkedPair(t, ctx, t.TempDir(), phoneDir, boot)

	var ids []string
	for _, body := range []string{"x", "y", "z"} {
		m, err := desk.SendMessage(ch, body, "", "")
		if err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		ids = append(ids, m.ID)
	}
	x, y, z := ids[0], ids[1], ids[2]
	waitUntil(t, 30*time.Second, func() bool {
		return hasMsg(phone, ch, x) && hasMsg(phone, ch, y) && hasMsg(phone, ch, z)
	}, "the messages never reached the phone")

	// Y is hidden while both are online, and the phone knows.
	if err := desk.HideMessage(ch, y); err != nil {
		t.Fatalf("HideMessage y: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool { return phone.store.IsHidden(y) },
		"the first hide of Y never reached the phone")

	// The phone goes away; its copy of the world is frozen with Y hidden.
	_ = phone.Close()

	if err := desk.HideMessage(ch, x); err != nil {
		t.Fatal(err)
	}
	if err := desk.UnhideMessage(ch, y); err != nil {
		t.Fatal(err)
	}
	if err := desk.HideMessage(ch, z); err != nil {
		t.Fatal(err)
	}

	phone2 := startServiceOn(t, ctx, phoneDir, boot)
	waitUntil(t, 60*time.Second, func() bool {
		return phone2.store.IsHidden(x) && phone2.store.IsHidden(z) && !phone2.store.IsHidden(y)
	}, "the returning phone did not converge: want X and Z hidden, Y visible")
	if hasMsg(phone2, ch, x) || hasMsg(phone2, ch, z) || !hasMsg(phone2, ch, y) {
		t.Fatal("the returning phone's history does not match its hide state")
	}
}

// TestHideRegistersLastWriterWinsAndBounded: a stale hide cannot beat a newer
// unhide, a fast clock is capped, and a catch-up is served in bounded pages.
func TestHideRegistersLastWriterWinsAndBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startServiceInDir(t, ctx, t.TempDir())
	now := time.Now().UnixMilli()
	svc.applyRemoteHides([]store.HideRec{{Channel: "c", ID: "m", Hidden: false, At: now}})
	svc.applyRemoteHides([]store.HideRec{{Channel: "c", ID: "m", Hidden: true, At: now - 5000}})
	if svc.store.IsHidden("m") {
		t.Fatal("a stale hide beat a newer unhide")
	}
	svc.applyRemoteHides([]store.HideRec{{Channel: "c", ID: "f", Hidden: true, At: now + int64(24*time.Hour/time.Millisecond)}})
	recs, _, _, _ := svc.store.HideRecordsAfter(0, 500)
	for _, r := range recs {
		if r.At > now+int64(readSkew/time.Millisecond)+1000 {
			t.Fatalf("a record from the future kept its stamp: %+v", r)
		}
	}
	for i := 0; i < 400; i++ {
		svc.applyRemoteHides([]store.HideRec{{Channel: "c", ID: "bulk" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Hidden: true, At: now}})
	}
	var from int64
	pages, total := 0, 0
	for {
		recs, last, more, err := svc.store.HideRecordsAfter(from, hidePageSize)
		if err != nil {
			t.Fatal(err)
		}
		if len(recs) > hidePageSize {
			t.Fatalf("page of %d exceeds the cap", len(recs))
		}
		pages++
		total += len(recs)
		from = last
		if !more {
			break
		}
	}
	if pages < 3 || total < 400 {
		t.Fatalf("pages=%d total=%d: the catch-up was not paged", pages, total)
	}
}
