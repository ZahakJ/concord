package app

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
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
	payload, _ := json.Marshal(guildMeta{Type: "hide_marker", Hide: map[string][]string{ch: {msg.ID}}})
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
		Hide:       map[string][]string{ch: {msg.ID}},
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
