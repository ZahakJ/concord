package app

import (
	"regexp"
	"time"

	"github.com/ZahakJ/concord/internal/store"
)

// stampsweep.go repairs pictures and files that were sent into a disappearing
// chat without a deadline. Until SendAttachment and SendFile took the
// composer's stamps, only the text of a send carried the [eph] token, so an
// image sent with the timer on stayed on every device for good.
//
// The timer itself is a per-device UI choice the core never sees, and the
// receiving side never knew it at all, so a deadline is inferred from the
// sender's own stamped messages around the attachment, which are
// MLS-authenticated and say exactly what timer that sender had on. A proven
// stamped text whose own timer has already run out on this device still
// counts: its tombstone keeps the sender, sent time and deadline. A tombstone
// that arrived by sync, or of a row that was never proven, does not.
//
//   - paired: the sender's next message, within a few seconds, is stamped
//     text. That is one send split in two, the caption half of a captioned
//     picture.
//   - bracketed: the sender's text before AND after it are both stamped.
//   - trailing: stamped text before, none after. The timer may have been
//     switched off in between, so these are counted and never touched.
//
// The deadline is the attachment's sent time plus the inferred timer; one that
// has already passed is expired by the ordinary ephemeral sweeper.

// attachOrFileRe spots a message that carries a picture or a file.
var attachOrFileRe = regexp.MustCompile(`concord://(attach|file)/v[12]/`)

// pairedGap is how far apart the two halves of one send can land.
const pairedGap = 5 * time.Second

// maxInferredTTL is the longest timer the composer offers (one week), with
// slack; a longer gap between stamp and send is not a timer.
const maxInferredTTL = 8 * 24 * 3600

// StampSweep is what the sweep found, and on a real run, what it changed.
type StampSweep struct {
	Paired    int  `json:"paired"`
	Bracketed int  `json:"bracketed"`
	Trailing  int  `json:"trailing"` // counted only; never given a deadline
	DueNow    int  `json:"dueNow"`   // of paired+bracketed, deadline already past
	Applied   int  `json:"applied"`  // rows given a deadline (0 on a dry run)
	DryRun    bool `json:"dryRun"`
}

// SweepUnstampedAttachments finds attachments in disappearing chats that carry
// no deadline. With apply false it only counts; with apply true it gives the
// paired and bracketed ones their inferred deadline, and the ephemeral sweeper
// erases any that are already due within one tick.
func (s *Service) SweepUnstampedAttachments(apply bool) (StampSweep, error) {
	out := StampSweep{DryRun: !apply}
	chats, err := s.store.DisappearingChats()
	if err != nil {
		return out, err
	}
	now := time.Now().Unix()
	for _, rows := range chats {
		for _, f := range inferDeadlines(rows) {
			switch f.how {
			case "paired":
				out.Paired++
			case "bracketed":
				out.Bracketed++
			default:
				out.Trailing++
				continue
			}
			if f.at <= now {
				out.DueNow++
			}
			if apply {
				if ok, err := s.store.SetExpiresIfUnset(f.id, f.at); err == nil && ok {
					out.Applied++
				}
			}
		}
	}
	if apply && out.Applied > 0 {
		s.sweepEphemeral(time.Now())
	}
	return out, nil
}

type inferred struct {
	id  string
	how string // paired, bracketed, trailing
	at  int64  // unix seconds
}

// inferDeadlines applies the rules above to one chat's rows, oldest first.
func inferDeadlines(rows []store.StampSweepRow) []inferred {
	var out []inferred
	for i, r := range rows {
		if r.Expires != 0 || !attachOrFileRe.MatchString(r.Content) {
			continue
		}
		sender := string(r.Sender)
		prev, next := -1, -1
		for j := i - 1; j >= 0; j-- {
			if string(rows[j].Sender) == sender && !attachOrFileRe.MatchString(rows[j].Content) {
				prev = j
				break
			}
		}
		for j := i + 1; j < len(rows); j++ {
			if string(rows[j].Sender) == sender && !attachOrFileRe.MatchString(rows[j].Content) {
				next = j
				break
			}
		}
		ttl := func(j int) int64 {
			if j < 0 || rows[j].Expires == 0 {
				return 0
			}
			t := rows[j].Expires - rows[j].Sent/int64(time.Second)
			if t <= 0 || t > maxInferredTTL {
				return 0
			}
			return t
		}
		sent := r.Sent / int64(time.Second)
		switch {
		case next >= 0 && ttl(next) > 0 && rows[next].Sent-r.Sent <= int64(pairedGap):
			out = append(out, inferred{r.ID, "paired", sent + ttl(next)})
		case ttl(prev) > 0 && ttl(next) > 0:
			t := ttl(prev)
			if ttl(next) < t {
				t = ttl(next)
			}
			out = append(out, inferred{r.ID, "bracketed", sent + t})
		case ttl(prev) > 0 && next < 0:
			out = append(out, inferred{r.ID, "trailing", sent + ttl(prev)})
		}
	}
	return out
}
