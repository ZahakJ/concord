package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// The core stamps a chat's timer on everything sent into it, whatever path
// sent it, and leaves content that already carries a stamp alone.
func TestTheCoreStampsEverySendIntoATimedChat(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Timed")
	if err != nil {
		t.Fatal(err)
	}
	ch := g.Channels[0].ID

	if err := svc.SetChatTimer(ch, 1234); err == nil {
		t.Fatal("a timer the app does not offer was taken")
	}
	if err := svc.SetChatTimer("no-such-chat", 3600); err == nil {
		t.Fatal("a timer for a chat this device does not have was taken")
	}
	if err := svc.SetChatTimer(ch, 3600); err != nil {
		t.Fatalf("SetChatTimer: %v", err)
	}

	stamped := func(m domain.Message) bool {
		at := store.EphemeralExpiry(m.Content)
		now := time.Now().Unix()
		return at >= now+3600-60 && at <= now+3600+5 && hasDeadline(t, svc, m.ID)
	}
	text, err := svc.SendMessage(ch, "plain words, as a notification reply sends them", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !stamped(text) {
		t.Fatalf("a text sent with no stamp went out without the chat's timer: %q", text.Content)
	}
	pic, err := svc.SendAttachment(ch, tinyPNG(), 1, 1, "", false, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !stamped(pic) {
		t.Fatal("a picture went out without the chat's timer")
	}
	own := "[eph](concord://eph/v1/" + "1900000000" + ")mine"
	kept, err := svc.SendMessage(ch, own, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Content != own {
		t.Fatalf("content that carried its own stamp was changed: %q", kept.Content)
	}

	// A scheduled send is stamped when it goes out.
	if _, err := svc.ScheduleSend(ch, "later", "", time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatalf("ScheduleSend: %v", err)
	}
	svc.fireDueScheduledSends(time.Now().Unix())
	msgs, err := svc.Messages(ch, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if strings.HasSuffix(m.Content, "later") {
			found = true
			if !stamped(m) {
				t.Fatalf("a scheduled send went out without the chat's timer: %q", m.Content)
			}
		}
	}
	if !found {
		t.Fatal("the scheduled send never went out")
	}

	// Off means off.
	if err := svc.SetChatTimer(ch, 0); err != nil {
		t.Fatal(err)
	}
	off, _ := svc.SendMessage(ch, "kept", "", "")
	if store.EphemeralExpiry(off.Content) != 0 {
		t.Fatal("a send after the timer was turned off was stamped")
	}
}

// The timer is one setting across an account's devices.
func TestTheChatTimerFollowsTheAccountToItsOtherDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, ch, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	if err := desk.SetChatTimer(ch, 300); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 45*time.Second, func() bool { return phone.store.ChatTimer(ch) == 300 },
		"the phone never learned the chat's timer")
	m, err := phone.SendMessage(ch, "from the phone", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.EphemeralExpiry(m.Content) == 0 {
		t.Fatal("the other device sent into the timed chat without the timer")
	}
}
