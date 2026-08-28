package app

import (
	"context"
	"testing"
	"time"
)

// Retention is one of the few things in the app that DELETES, on a timer
// nobody watches: the loop runs once at start and then hourly, and the shortest
// policy a person can choose is a day. Nothing had ever run it. This drives the
// boundary directly — sweepRetention takes its `now` for exactly that reason —
// and pins the three promises the panel makes beside the switch: old messages
// go, pinned and saved ones stay, and a policy of zero means never.
func TestRetentionSweepRemovesOldMessagesAndKeepsTheKeptOnes(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Housekeeping")
	if err != nil {
		t.Fatalf("create guild: %v", err)
	}
	ch := g.Channels[0].ID

	var ids []string
	for _, body := range []string{"ordinary", "pinned one", "saved one"} {
		m, err := svc.SendMessage(ch, body, "", "")
		if err != nil {
			t.Fatalf("send %q: %v", body, err)
		}
		ids = append(ids, m.ID)
	}
	if err := svc.store.SetPinned(ids[1], true); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SaveBookmark(ids[2], ch, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}

	// A day, the shortest the panel offers.
	if err := svc.SetRetention(g.ID, "", 86400); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if got := svc.retentionFor(svc.govState[g.ID], ch); got != 86400 {
		t.Fatalf("policy did not take: %d", got)
	}

	// Now, on this side of the cutoff: nothing has aged out yet.
	svc.sweepRetention(time.Now())
	if msgs, _ := svc.store.Messages(ch, 0); len(msgs) < 3 {
		t.Fatalf("a sweep at the present time removed something: %d messages left", len(msgs))
	}

	// Two days on: everything that was not deliberately kept goes.
	svc.sweepRetention(time.Now().Add(48 * time.Hour))
	msgs, err := svc.store.Messages(ch, 0)
	if err != nil {
		t.Fatal(err)
	}
	left := map[string]bool{}
	for _, m := range msgs {
		left[m.Content] = true
	}
	if left["ordinary"] {
		t.Error("a message older than the policy survived the sweep")
	}
	if !left["pinned one"] {
		t.Error("a pinned message was removed; the panel promises pinned messages stay")
	}
	if !left["saved one"] {
		t.Error("a saved message was removed; the panel promises saved messages stay")
	}
}

// "Keep everything" has to be the real absence of a policy, not a very long
// one: the op deletes the entry, and a sweep a decade later must still find
// every message where it was left.
func TestRetentionZeroMeansNeverSweeps(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, _ := svc.CreateGuild("Forever")
	ch := g.Channels[0].ID
	if _, err := svc.SendMessage(ch, "still here", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRetention(g.ID, "", 86400); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRetention(g.ID, "", 0); err != nil {
		t.Fatal(err)
	}
	svc.sweepRetention(time.Now().Add(10 * 365 * 24 * time.Hour))
	if msgs, _ := svc.store.Messages(ch, 0); len(msgs) == 0 {
		t.Fatal("a guild set back to 'keep everything' was swept anyway")
	}
}

// The floor is enforced in REPLAY, so a policy of seconds — however it was
// issued — folds to an hour on every client rather than eating a conversation
// as fast as it is typed.
func TestRetentionFoldsASecondsPolicyToTheHourFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, _ := svc.CreateGuild("Impatient")
	ch := g.Channels[0].ID
	if _, err := svc.SendMessage(ch, "written a moment ago", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRetention(g.ID, "", 5); err != nil {
		t.Fatal(err)
	}
	if got := svc.retentionFor(svc.govState[g.ID], ch); got != 3600 {
		t.Fatalf("a 5-second policy folded to %d, want the 3600 floor", got)
	}
	svc.sweepRetention(time.Now().Add(time.Minute))
	if msgs, _ := svc.store.Messages(ch, 0); len(msgs) == 0 {
		t.Fatal("a minute-old message was swept by a policy that is supposed to floor at an hour")
	}
}
