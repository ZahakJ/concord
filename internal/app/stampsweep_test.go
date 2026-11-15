package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

func ephStamp(at time.Time) string {
	return fmt.Sprintf("[eph](concord://eph/v1/%d)", at.Unix())
}

func hasDeadline(t *testing.T, s *Service, id string) bool {
	t.Helper()
	due, err := s.store.DueEphemeral(time.Now().Add(400 * 24 * time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range due {
		if d.ID == id {
			return true
		}
	}
	return false
}

// A picture with a caption is ONE message, and the disappearing timer the
// caption carries is the picture's deadline too. Replacing the picture (the
// meme editor's re-edit) keeps both the caption and the timer.
func TestAPictureAndItsCaptionAreOneMessageThatExpiresTogether(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Captions")
	if err != nil {
		t.Fatal(err)
	}
	ch := g.Channels[0].ID
	stamp := ephStamp(time.Now().Add(time.Hour))

	msg, err := svc.SendAttachment(ch, tinyPNG(), 1, 1, "", false, "", "", stamp+"look at this", "")
	if err != nil {
		t.Fatalf("SendAttachment: %v", err)
	}
	if !strings.HasPrefix(msg.Content, "![image](concord://attach/") || !strings.HasSuffix(msg.Content, "\n"+stamp+"look at this") {
		t.Fatalf("picture and caption are not one message: %q", msg.Content)
	}
	if !hasDeadline(t, svc, msg.ID) {
		t.Fatal("a captioned picture sent with the timer on has no deadline")
	}

	bare, err := svc.SendAttachment(ch, tinyPNG(), 1, 1, "", false, "", "", stamp, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasDeadline(t, svc, bare.ID) {
		t.Fatal("a picture without a caption, sent with the timer on, has no deadline")
	}
	file, err := svc.SendFile(ch, "data:text/plain;base64,aGVsbG8=", "a.txt", "", stamp, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasDeadline(t, svc, file.ID) {
		t.Fatal("a file sent with the timer on has no deadline")
	}

	if _, err := svc.EditAttachment(ch, msg.ID, tinyPNG(), 2, 2); err != nil {
		t.Fatalf("EditAttachment: %v", err)
	}
	edited, _, _ := svc.store.MessageByID(msg.ID)
	if !strings.Contains(edited.Content, "/2x2") || !strings.HasSuffix(edited.Content, "\n"+stamp+"look at this") {
		t.Fatalf("replacing the picture lost the caption or the timer: %q", edited.Content)
	}
}

// The sweep infers a deadline only from the sender's own stamped messages
// around an unstamped attachment, counts before it changes anything, and never
// touches the case where the timer may have been switched off.
func TestSweepGivesOldUnstampedAttachmentsTheirTimer(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := startServiceInDir(t, ctx, t.TempDir())
	me, other := svc.PublicKey(), []byte("someone-else-entirely-32-bytes!!")
	const pic = "![image](concord://attach/v1/" + "0000000000000000000000000000000000000000000000000000000000000000/k/png/1x1)"
	hour := time.Hour
	base := time.Now().Add(-3 * hour).Truncate(time.Second)
	n := 0
	put := func(ch string, who []byte, at time.Time, content string) string {
		n++
		id := fmt.Sprintf("m%02d", n)
		if _, err := svc.store.SaveMessage(domain.Message{ID: id, ChannelID: ch, Sender: who, Sent: at, Content: content}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	stamped := func(at time.Time, ttl time.Duration, text string) string { return ephStamp(at.Add(ttl)) + text }

	// Chat 1: a one-hour timer.
	paired := put("c1", me, base, pic) // its caption follows at once: due now
	put("c1", me, base.Add(time.Second), stamped(base.Add(time.Second), hour, "caption"))
	bracketed := put("c1", me, base.Add(2*hour+50*time.Minute), pic) // stamped text either side: due in ~1h
	put("c1", me, base.Add(2*hour+55*time.Minute), stamped(base.Add(2*hour+55*time.Minute), hour, "later"))
	theirs := put("c1", other, base.Add(10*time.Minute), pic) // no stamped text of THEIRS around it
	// Chat 2: stamped, then a picture, then nothing — the timer may be off.
	put("c2", me, base, stamped(base, hour, "before"))
	trailing := put("c2", me, base.Add(time.Minute), pic)
	// Chat 3: stamped, picture, then UNSTAMPED text — switched off; leave it.
	put("c3", me, base, stamped(base, hour, "before"))
	offAfter := put("c3", me, base.Add(time.Minute), pic)
	put("c3", me, base.Add(2*time.Minute), "timer off now")
	// Chat 4: no disappearing messages at all.
	plain := put("c4", me, base, pic)

	dry, err := svc.SweepUnstampedAttachments(false)
	if err != nil {
		t.Fatal(err)
	}
	if want := (StampSweep{Paired: 1, Bracketed: 1, Trailing: 1, DueNow: 1, DryRun: true}); dry != want {
		t.Fatalf("dry run %+v, want %+v", dry, want)
	}
	for _, id := range []string{paired, bracketed, theirs, trailing, offAfter, plain} {
		if hasDeadline(t, svc, id) {
			t.Fatalf("the dry run changed %s", id)
		}
	}

	got, err := svc.SweepUnstampedAttachments(true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Applied != 2 {
		t.Fatalf("applied %d, want 2 (%+v)", got.Applied, got)
	}
	if m, _, _ := svc.store.MessageByID(paired); !m.Deleted {
		t.Fatal("an attachment whose timer has run out was not erased")
	}
	if m, _, _ := svc.store.MessageByID(bracketed); m.Deleted || !hasDeadline(t, svc, bracketed) {
		t.Fatal("an attachment with time left must keep it, with a deadline")
	}
	for _, id := range []string{theirs, trailing, offAfter, plain} {
		if m, _, _ := svc.store.MessageByID(id); m.Deleted || hasDeadline(t, svc, id) {
			t.Fatalf("%s had no evidence of a timer and was touched", id)
		}
	}
	// Running it again finds nothing more to do.
	if again, _ := svc.SweepUnstampedAttachments(true); again.Applied != 0 {
		t.Fatalf("a second run applied %d", again.Applied)
	}
}

func TestInferDeadlinesIgnoresAbsurdTimers(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	rows := []store.StampSweepRow{
		{ID: "p", Sender: []byte("a"), Sent: at.UnixNano(), Content: "![image](concord://attach/v1/x/)"},
		// A stamp a year out is no timer the composer offers.
		{ID: "t", Sender: []byte("a"), Sent: at.Add(time.Second).UnixNano(), Expires: at.Add(365 * 24 * time.Hour).Unix(), Content: "text"},
	}
	if got := inferDeadlines(rows); len(got) != 0 {
		t.Fatalf("inferred %+v from a year-long stamp", got)
	}
}
