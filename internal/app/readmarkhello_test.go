package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// TestAReadMarkSetBeforeLinkingReachesTheNewDevice: read marks travelled only
// as a live publish on the Notes meta topic, with no catch-up, so a channel
// read on the desktop before the phone was linked stayed unread on the phone
// until somebody read it again. The own-device hello now carries the marks.
func TestAReadMarkSetBeforeLinkingReachesTheNewDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)

	desk := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := desk.CreateGuild("Read Before")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID
	at := time.Now().Add(-time.Minute).UnixMilli()
	if _, err := desk.MarkRead(ch, at); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	// Let the (unheard) publish go out before there is anyone to hear it.
	time.Sleep(2 * time.Second)

	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	phoneDir := t.TempDir()
	res, err := RedeemLink(ctx, phoneDir, code, "test-pass", "")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	phone := startServiceOn(t, ctx, phoneDir, boot)
	for _, ic := range res.GuildInvites {
		if _, err := phone.JoinViaInvite(ic); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitUntil(t, 30*time.Second, func() bool {
		rs, err := phone.ReadState()
		return err == nil && rs[ch] >= at
	}, "a read mark set before linking never reached the new device")
}

// TestAReadMarkReachesADeviceNotYetListeningOnNotes: the flake in
// TestLinkedDevicesShareOneNotesGroup. A marker published while the other
// device is not yet in the Notes meta topic's mesh was lost. Here the phone
// leaves that topic outright, so the publish cannot reach it at all; the
// marker must arrive anyway, over the hello.
func TestAReadMarkReachesADeviceNotYetListeningOnNotes(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	notes, err := desk.NotesDM()
	if err != nil {
		t.Fatalf("NotesDM: %v", err)
	}
	topic := domain.GuildMetaTopicID(notes.GroupID)
	phone.ps.Unsubscribe(topic)
	phoneID := phone.host.PeerID()
	waitUntil(t, 20*time.Second, func() bool {
		for _, p := range desk.ps.TopicPeers(topic) {
			if p == phoneID {
				return false
			}
		}
		return true
	}, "the desktop still lists the phone on the Notes meta topic")

	at := time.Now().UnixMilli()
	if _, err := desk.MarkRead(textCh, at); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		rs, err := phone.ReadState()
		return err == nil && rs[textCh] >= at
	}, "the read marker never reached a device that was not listening on Notes")
}

// TestReadMarksStayInsideTheAccount: the hello also goes to guild members and
// contacts, and read marks must never ride it to them, nor be taken from them.
func TestReadMarksStayInsideTheAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g, err := owner.CreateGuild("Private Reading")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)
	ch := g.Channels[0].ID
	if _, err := owner.MarkRead(ch, time.Now().UnixMilli()); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	if m := owner.readMarksFor(member.host.PeerID()); m != nil {
		t.Fatalf("the owner would offer a guild member its read marks: %v", m)
	}
	// The member's own hello, answered by the owner, carrying marks of its own.
	req, _ := json.Marshal(helloFrame{
		Credential: member.myCredential,
		ReadMarks:  map[string]int64{"planted": time.Now().UnixMilli()},
	})
	owner.answered.release(member.host.PeerID()) // a full answer, not the per-connection short one
	raw, err := owner.handleHello(ctx, member.host.PeerID(), req)
	if err != nil {
		t.Fatalf("handleHello: %v", err)
	}
	var out helloFrame
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if len(out.ReadMarks) != 0 {
		t.Fatalf("the owner's hello answer to a guild member carried read marks: %v", out.ReadMarks)
	}
	time.Sleep(time.Second) // adoption runs off the hello's goroutine
	if rs, _ := owner.ReadState(); rs["planted"] != 0 {
		t.Fatal("the owner adopted a read mark from a guild member's hello")
	}
}
