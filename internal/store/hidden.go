package store

import (
	"fmt"
	"time"
)

// "Delete for me": a message the user has hidden on their own devices. The
// message row itself is untouched (peers still sync it from us, and a later
// "delete for everyone" from its author still lands); hiding is purely a
// local, per-account filter. It lives in its own table keyed by message id so
// a hide that arrives from another device BEFORE the message does still takes
// effect the moment the message is stored, with no change to SaveMessage.

// notHidden is the one predicate every user-facing read of messages ANDs in.
// idCol is the message id column as the query spells it ("id" or "m.id").
func notHidden(idCol string) string {
	return idCol + ` NOT IN (SELECT message_id FROM hidden_messages)`
}

// hiddenOrphanTTL is how long a hide whose message never turned up is kept. A
// hide can legitimately precede its message (sync order), but not forever.
const hiddenOrphanTTL = 90 * 24 * time.Hour

func (s *Store) migrateHidden() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS hidden_messages (
  message_id TEXT PRIMARY KEY,
  channel_id TEXT NOT NULL,
  at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_hidden_channel ON hidden_messages(channel_id)`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// HideMessage hides a message for this account. Idempotent; reports whether it
// was newly hidden.
func (s *Store) HideMessage(channelID, messageID string) (bool, error) {
	if channelID == "" || messageID == "" {
		return false, nil
	}
	res, err := s.db.Exec(
		`INSERT INTO hidden_messages (message_id, channel_id, at) VALUES (?, ?, ?)
		 ON CONFLICT(message_id) DO NOTHING`, messageID, channelID, time.Now().Unix())
	if err != nil {
		return false, fmt.Errorf("store: hide message: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UnhideMessage undoes HideMessage; reports whether it was hidden.
func (s *Store) UnhideMessage(messageID string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM hidden_messages WHERE message_id = ?`, messageID)
	if err != nil {
		return false, fmt.Errorf("store: unhide message: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// IsHidden reports whether a message is hidden.
func (s *Store) IsHidden(messageID string) bool {
	var n int
	err := s.rd().QueryRow(`SELECT COUNT(1) FROM hidden_messages WHERE message_id = ?`, messageID).Scan(&n)
	return err == nil && n > 0
}

// HiddenIDs returns the hidden message ids of one channel.
func (s *Store) HiddenIDs(channelID string) ([]string, error) {
	rows, err := s.rd().Query(`SELECT message_id FROM hidden_messages WHERE channel_id = ?`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AllHidden returns every hidden (channel, message) pair, newest hide first,
// bounded by limit, for offering to a device that has just come online.
func (s *Store) AllHidden(limit int) (map[string][]string, error) {
	if limit <= 0 {
		limit = 2000
	}
	rows, err := s.rd().Query(`SELECT channel_id, message_id FROM hidden_messages ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var ch, id string
		if err := rows.Scan(&ch, &id); err != nil {
			return nil, err
		}
		out[ch] = append(out[ch], id)
	}
	return out, rows.Err()
}

// SweepHidden bounds the table: it drops hides whose message is gone (deleted
// for everyone, or pruned) and hides that never met a message within
// hiddenOrphanTTL. Returns the rows removed.
func (s *Store) SweepHidden(now time.Time) (int, error) {
	res, err := s.db.Exec(
		`DELETE FROM hidden_messages
		 WHERE message_id IN (SELECT id FROM messages WHERE deleted = 1)
		    OR (message_id NOT IN (SELECT id FROM messages) AND at < ?)`,
		now.Add(-hiddenOrphanTTL).Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
