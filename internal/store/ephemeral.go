package store

import (
	"regexp"
	"strconv"
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

// migrateExpires adds the expiry column and, the one time it is added, reads
// the deadline out of every live row already on disk: those are precisely the
// messages this bug left behind.
func (s *Store) migrateExpires() error {
	if _, err := s.db.Exec(`ALTER TABLE messages ADD COLUMN expires INTEGER NOT NULL DEFAULT 0`); err != nil {
		return nil // already there
	}
	rows, err := s.rd().Query(`SELECT id, content_enc, nonce FROM messages WHERE deleted = 0`)
	if err != nil {
		return err
	}
	due := map[string]int64{}
	for rows.Next() {
		var id string
		var enc, nonce []byte
		if rows.Scan(&id, &enc, &nonce) != nil {
			continue
		}
		body, err := s.open(enc, nonce)
		if err != nil {
			continue
		}
		if at := EphemeralExpiry(body); at > 0 {
			due[id] = at
		}
	}
	rows.Close()
	for id, at := range due {
		_, _ = s.db.Exec(`UPDATE messages SET expires = ? WHERE id = ?`, at, id)
	}
	return nil
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
