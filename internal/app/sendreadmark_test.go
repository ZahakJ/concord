package app

import (
	"context"
	"testing"
	"time"
)

// ZW-03. Only the user writing in a chat moves the read mark. A scheduled
// send fires whether or not anyone is looking, and a system notice is not the
// user reading anything.
func TestOnlyAnInteractiveSendMovesTheReadMark(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Later")
	if err != nil {
		t.Fatal(err)
	}
	ch := g.Channels[0].ID
	mark := func() int64 {
		at, _ := svc.store.ReadMark(ch)
		return at
	}
	before := mark()

	at := time.Now().Add(time.Hour).Unix()
	if _, err := svc.ScheduleSend(ch, "posted while nobody was looking", "", at); err != nil {
		t.Fatal(err)
	}
	svc.fireDueScheduledSends(at + 1)
	if !hasMessage(svc, ch, "posted while nobody was looking") {
		t.Fatal("setup: the scheduled message never sent")
	}
	if got := mark(); got != before {
		t.Errorf("a scheduled send moved the read mark from %d to %d", before, got)
	}
	time.Sleep(5 * time.Millisecond)
	svc.sendSystem(ch, "a notice")
	if !hasMessage(svc, ch, "a notice") {
		t.Fatal("setup: the notice never posted")
	}
	if got := mark(); got != before {
		t.Errorf("a system notice moved the read mark from %d to %d", before, got)
	}

	msg, err := svc.SendMessage(ch, "written, so read", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := mark(); got < msg.Sent.UnixMilli() {
		t.Errorf("an interactive send left the mark at %d, before its own message %d", got, msg.Sent.UnixMilli())
	}
}
