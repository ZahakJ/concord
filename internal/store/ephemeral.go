package store

import (
	"regexp"
	"strconv"
	"time"
)

// ephemeral.go keeps the core's own record of when a disappearing message dies.
//
// The expiry rides inside the message body, sealed at rest like the rest of it,
// so the store could not see it, and the only thing that ever acted on it was the
// view: a device erased a message when it happened to have that channel on
// screen past the deadline. Every other device — a phone in a drawer, a linked
// desktop parked in another channel, the sender's own — kept the plaintext and
// served it to search indefinitely. So the deadline is read out of the body when
// the row is written, the way the quiet flag is, and kept beside it in the clear;
// it says when a row dies, not what it says. A sweeper in the core then erases
// whatever is due whether or not anybody is looking.

// ephemeralTokenRe is the token the composer stamps: an absolute unix second.
var ephemeralTokenRe = regexp.MustCompile(`\[eph\]\(concord://eph/v1/(\d{9,12})\)`)

// EphemeralExpiry returns a body's disappearing-message deadline in unix
// seconds, or 0 when it has none.
func EphemeralExpiry(content string) int64 {
	m := ephemeralTokenRe.FindStringSubmatch(content)
	if m == nil {
		return 0
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// migrateExpires adds the expiry column and, the one time it is added, marks
// every live row already on disk for a backfill of its deadline: those are
// precisely the messages written before the store could see one.
//
// The backfill itself does not run here. It has to open every sealed body,
// which on a long history (88k messages) put half a second on the first
// unlock after the upgrade, in front of the lock screen. The mark is a cursor
// in settings, so the work survives a restart half way through, and
// backfillExpires does it after Open has returned.
func (s *Store) migrateExpires() error {
	if _, err := s.db.Exec(`ALTER TABLE messages ADD COLUMN expires INTEGER NOT NULL DEFAULT 0`); err != nil {
		return nil // already there
	}
	return s.SetSetting(expiresBackfillKey, "0")
}

// expiresBackfillKey holds the rowid the deadline backfill has reached; absent
// once it is done (or for a store that never needed it).
const expiresBackfillKey = "store.expires_backfill_from"

// expiresBackfillChunk bounds one step: a step reads through the read pool and
// writes only the few rows that carry a deadline, so the writer is held for
// moments at a time, never for the whole history.
const expiresBackfillChunk = 2000

// expiresBackfillDelay is how long after Open the backfill starts. A var so a
// test need not wait it out.
var expiresBackfillDelay = 5 * time.Second

// backfillExpires reads the deadline out of every live row the migration
// marked, a chunk at a time, until it reaches the end or the store closes.
// Until it has passed a row, a disappearing message written before the
// upgrade is erased by the sweeper a little late; nothing else depends on it.
func (s *Store) backfillExpires(delay time.Duration) {
	defer s.bg.Done()
	// Not during the unlock: the app is busy opening every guild and the UI is
	// asking for its first screens. Nothing waits on this.
	select {
	case <-s.closing:
		return
	case <-time.After(delay):
	}
	for {
		select {
		case <-s.closing:
			return
		default:
		}
		v, err := s.GetSetting(expiresBackfillKey)
		if err != nil || v == "" {
			return
		}
		from, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			_ = s.DeleteSetting(expiresBackfillKey)
			return
		}
		next, done, err := s.backfillExpiresChunk(from, expiresBackfillChunk)
		if err != nil {
			return // the next Open picks it up from the same row
		}
		if done {
			_ = s.DeleteSetting(expiresBackfillKey)
			return
		}
		if err := s.SetSetting(expiresBackfillKey, strconv.FormatInt(next, 10)); err != nil {
			return
		}
		select {
		case <-s.closing:
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// backfillExpiresChunk handles the live rows with rowid > from, at most limit
// of them, and returns the last rowid it saw and whether there were no more.
func (s *Store) backfillExpiresChunk(from int64, limit int) (int64, bool, error) {
	rows, err := s.rd().Query(
		`SELECT rowid, id, content_enc, nonce FROM messages WHERE rowid > ? AND deleted = 0 ORDER BY rowid LIMIT ?`,
		from, limit)
	if err != nil {
		return from, false, err
	}
	due := map[string]int64{}
	last, n := from, 0
	for rows.Next() {
		var rowid int64
		var id string
		var enc, nonce []byte
		if rows.Scan(&rowid, &id, &enc, &nonce) != nil {
			continue
		}
		last = rowid
		n++
		body, err := s.open(enc, nonce)
		if err != nil {
			continue
		}
		if at := EphemeralExpiry(body); at > 0 {
			due[id] = at
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return from, false, err
	}
	for id, at := range due {
		// Only where nothing has set it since: a row stored after the upgrade
		// already carries its own deadline.
		_, _ = s.db.Exec(`UPDATE messages SET expires = ? WHERE id = ? AND expires = 0`, at, id)
	}
	return last, n < limit, nil
}

// DueMessage is a live row whose disappearing timer has run out.
type DueMessage struct {
	ID        string
	ChannelID string
}

// DueEphemeral lists live rows whose deadline is at or before now (unix seconds).
func (s *Store) DueEphemeral(now int64) ([]DueMessage, error) {
	rows, err := s.rd().Query(
		`SELECT id, channel_id FROM messages WHERE expires > 0 AND expires <= ? AND deleted = 0`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueMessage
	for rows.Next() {
		var d DueMessage
		if rows.Scan(&d.ID, &d.ChannelID) == nil {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// NextEphemeral is the earliest deadline still pending, or 0 when none is.
func (s *Store) NextEphemeral() int64 {
	var at int64
	_ = s.rd().QueryRow(`SELECT COALESCE(MIN(expires), 0) FROM messages WHERE expires > 0 AND deleted = 0`).Scan(&at)
	return at
}
