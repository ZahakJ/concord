package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// "These send from this device — even if this window closes, as long as Concord
// is running here." That sentence is the whole reason the queue was moved out
// of the browser and into the store, and nothing had ever exercised it. The
// boundary is driven directly (fireDueScheduledSends takes its `now` so tests
// can), because the real loop beats every thirty seconds.
func TestAScheduledMessageSendsWhenItsTimeComes(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Later")
	if err != nil {
		t.Fatalf("create guild: %v", err)
	}
	ch := g.Channels[0].ID

	at := time.Now().Add(time.Hour).Unix()
	id, err := svc.ScheduleSend(ch, "posted while nobody was looking", "", at)
	if err != nil {
		t.Fatalf("ScheduleSend: %v", err)
	}
	q, _ := svc.ScheduledSends()
	if len(q) != 1 || q[0].ID != id {
		t.Fatalf("the queue does not hold the message: %+v", q)
	}

	// Its time has not come.
	svc.fireDueScheduledSends(time.Now().Unix())
	if msgs, _ := svc.store.Messages(ch, 0); anyContent(msgs, "posted while nobody was looking") {
		t.Fatal("a scheduled message sent an hour early")
	}
	if q, _ := svc.ScheduledSends(); len(q) != 1 {
		t.Fatal("the queue lost a message that had not fired")
	}

	// Its time comes.
	svc.fireDueScheduledSends(at + 1)
	msgs, err := svc.store.Messages(ch, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !anyContent(msgs, "posted while nobody was looking") {
		t.Fatal("the scheduled message never sent")
	}
	if q, _ := svc.ScheduledSends(); len(q) != 0 {
		t.Fatalf("a message that sent is still queued: %+v", q)
	}

	// And it does not send twice on the next beat.
	svc.fireDueScheduledSends(at + 3600)
	if n := countContent(msgs2(t, svc, ch), "posted while nobody was looking"); n != 1 {
		t.Fatalf("the message was sent %d times", n)
	}
}

// Cancelling takes it out of the queue, and a later sweep must not resurrect it.
func TestACancelledScheduledMessageNeverSends(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, _ := svc.CreateGuild("Later")
	ch := g.Channels[0].ID
	at := time.Now().Add(time.Hour).Unix()
	id, err := svc.ScheduleSend(ch, "thought better of it", "", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CancelScheduledSend(id); err != nil {
		t.Fatal(err)
	}
	svc.fireDueScheduledSends(at + 1)
	if anyContent(msgs2(t, svc, ch), "thought better of it") {
		t.Fatal("a cancelled scheduled message sent anyway")
	}
}

// A queue row whose channel has gone can never send. It is dropped rather than
// retried every thirty seconds until the process ends.
func TestAScheduledSendForAVanishedChannelIsDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, _ := svc.CreateGuild("Later")
	ch := g.Channels[0].ID
	at := time.Now().Add(time.Hour).Unix()
	if _, err := svc.ScheduleSend(ch, "into the void", "", at); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteChannel(g.ID, ch); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	svc.fireDueScheduledSends(at + 1)
	if q, _ := svc.ScheduledSends(); len(q) != 0 {
		t.Fatalf("a send for a channel that no longer exists is still queued: %+v", q)
	}
}

func msgs2(t *testing.T, svc *Service, ch string) []domain.Message {
	t.Helper()
	rows, err := svc.store.Messages(ch, 0)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func anyContent(rows []domain.Message, want string) bool {
	return countContent(rows, want) > 0
}

func countContent(rows []domain.Message, want string) int {
	n := 0
	for _, m := range rows {
		if m.Content == want {
			n++
		}
	}
	return n
}

// The send-later loop must not inherit the background beat when something is
// actually due. Measured before this: a message queued for 70 seconds' time,
// with the browser then closed, arrived 131 seconds late — one three-minute
// background beat, because closing the last window is what puts the node to
// sleep, and "even if this window closes" is precisely what the feature
// promises.
func TestTheSendLaterLoopWakesForItsOwnDeadline(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	beat := 3 * time.Minute

	// Due inside the beat: wait for the deadline, not the beat.
	if got := scheduledWait(now, beat, now.Add(70*time.Second).Unix()); got != 71*time.Second {
		t.Errorf("a send due in 70s waits %v, want 71s", got)
	}
	// Due well beyond it: the beat is soon enough, and it will be re-evaluated.
	if got := scheduledWait(now, beat, now.Add(time.Hour).Unix()); got != beat {
		t.Errorf("a send due in an hour waits %v, want the %v beat", got, beat)
	}
	// Already overdue (and presumably failing to send): keep the paced retry
	// cadence rather than spinning on it.
	if got := scheduledWait(now, beat, now.Add(-time.Minute).Unix()); got != beat {
		t.Errorf("an overdue send waits %v, want the %v beat", got, beat)
	}
	// Exactly now counts as overdue, not as a zero-length sleep.
	if got := scheduledWait(now, beat, now.Unix()); got != beat {
		t.Errorf("a send due this instant waits %v, want the %v beat", got, beat)
	}
	// The foreground beat is short already; nothing gets longer.
	fg := 30 * time.Second
	if got := scheduledWait(now, fg, now.Add(5*time.Second).Unix()); got != 6*time.Second {
		t.Errorf("a send due in 5s on the foreground beat waits %v, want 6s", got)
	}
}

// And the queue-reading half: an empty queue costs nothing and sleeps the beat.
func TestAnEmptySendLaterQueueSleepsTheWholeBeat(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	if got := svc.nextScheduledWait(time.Now()); got != svc.bgPace(scheduledSendTick) {
		t.Fatalf("an empty queue waits %v, want the paced beat %v", got, svc.bgPace(scheduledSendTick))
	}
	g, _ := svc.CreateGuild("Later")
	at := time.Now().Add(10 * time.Second).Unix()
	if _, err := svc.ScheduleSend(g.Channels[0].ID, "soon", "", at); err != nil {
		t.Fatal(err)
	}
	if got := svc.nextScheduledWait(time.Now()); got > 12*time.Second {
		t.Fatalf("a send due in ten seconds left the loop asleep for %v", got)
	}
}
