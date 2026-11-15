package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// "Delete for me": a message the user has hidden on their own devices. The
// message row itself is untouched (peers still sync it from us, and a later
// "delete for everyone" from its author still lands); hiding is purely a
// local, per-account filter. It lives in its own table keyed by message id so
// a hide that arrives from another device BEFORE the message does still takes
// effect the moment the message is stored, with no change to SaveMessage.
//
// Each row is a timestamped register, not a bare set: hidden=0 is an unhide
// kept as a TOMBSTONE, and the row with the newest `at` (UnixMilli) wins. That
// is what lets a device that was offline catch up by exchanging state instead
// of replaying ops: an undone hide can never be resurrected by a stale peer,
// because the newer unhide beats it. `seq` is a local, strictly increasing
// stamp of when this device last changed the row; peers page through it.

// HideRec is one register as it travels between our own devices.
type HideRec struct {
	Channel string `json:"c"`
	ID      string `json:"i"`
	Hidden  bool   `json:"h"`
	At      int64  `json:"t"` // UnixMilli, last writer wins
}

// hiddenOrphanTTL is how long a row whose message never turned up is kept. A
// hide can legitimately precede its message (sync order), but not forever.
const hiddenOrphanTTL = 90 * 24 * time.Hour

func (s *Store) migrateHidden() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS hidden_messages (
  message_id TEXT PRIMARY KEY,
  channel_id TEXT NOT NULL,
  at         INTEGER NOT NULL,
  hidden     INTEGER NOT NULL DEFAULT 1,
  seq        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_hidden_channel ON hidden_messages(channel_id)`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	for _, col := range []string{
		`ALTER TABLE hidden_messages ADD COLUMN hidden INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE hidden_messages ADD COLUMN seq INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := s.db.Exec(col); err != nil && !isDuplicateColumn(err) {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	if _, err := s.db.Exec(`UPDATE hidden_messages SET seq = rowid WHERE seq = 0`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_hidden_seq ON hidden_messages(seq)`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column")
}

// SetHidden applies one register with last-writer-wins on At (an equal At
// lets hide beat unhide, so two devices agree). Reports whether the message's
// hidden state actually changed. Stale records are no-ops.
func (s *Store) SetHidden(r HideRec) (bool, error) {
	if r.Channel == "" || r.ID == "" || r.At <= 0 {
		return false, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck
	var prevAt int64
	var prevHidden int
	err = tx.QueryRow(`SELECT at, hidden FROM hidden_messages WHERE message_id = ?`, r.ID).Scan(&prevAt, &prevHidden)
	exists := err == nil
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if exists && (r.At < prevAt || (r.At == prevAt && !r.Hidden)) {
		return false, nil
	}
	if exists && r.At == prevAt && r.Hidden == (prevHidden == 1) {
		return false, nil
	}
	if _, err := tx.Exec(
		`INSERT INTO hidden_messages (message_id, channel_id, at, hidden, seq)
		 VALUES (?, ?, ?, ?, MAX(COALESCE((SELECT MAX(seq) FROM hidden_messages), 0) + 1, ?))
		 ON CONFLICT(message_id) DO UPDATE SET channel_id = excluded.channel_id, at = excluded.at,
		   hidden = excluded.hidden, seq = excluded.seq`,
		r.ID, r.Channel, r.At, boolToInt(r.Hidden), time.Now().UnixMicro()); err != nil {
		return false, fmt.Errorf("store: set hidden: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return (exists && prevHidden == 1) != r.Hidden || (!exists && r.Hidden), nil
}

// LocalHide records the user's own hide or unhide now. The stamp is forced to
// be newer than whatever the row holds, so an Undo within the same millisecond
// still wins. Returns the record to tell our other devices.
func (s *Store) LocalHide(channelID, messageID string, hidden bool) (HideRec, bool, error) {
	at := time.Now().UnixMilli()
	var prev int64
	if err := s.rd().QueryRow(`SELECT at FROM hidden_messages WHERE message_id = ?`, messageID).Scan(&prev); err == nil && prev >= at {
		at = prev + 1
	}
	rec := HideRec{Channel: channelID, ID: messageID, Hidden: hidden, At: at}
	changed, err := s.SetHidden(rec)
	return rec, changed, err
}

// HideMessage hides a message for this account; reports whether it was newly hidden.
func (s *Store) HideMessage(channelID, messageID string) (bool, error) {
	if channelID == "" || messageID == "" {
		return false, nil
	}
	_, changed, err := s.LocalHide(channelID, messageID, true)
	return changed, err
}

// UnhideMessage undoes HideMessage, leaving a tombstone; reports whether it was hidden.
func (s *Store) UnhideMessage(messageID string) (bool, error) {
	var ch string
	if err := s.rd().QueryRow(`SELECT channel_id FROM hidden_messages WHERE message_id = ?`, messageID).Scan(&ch); err != nil {
		return false, nil
	}
	_, changed, err := s.LocalHide(ch, messageID, false)
	return changed, err
}

// IsHidden reports whether a message is hidden.
func (s *Store) IsHidden(messageID string) bool {
	var n int
	err := s.rd().QueryRow(`SELECT COUNT(1) FROM hidden_messages WHERE message_id = ? AND hidden = 1`, messageID).Scan(&n)
	return err == nil && n > 0
}

// HiddenIDs returns the hidden message ids of one channel.
func (s *Store) HiddenIDs(channelID string) ([]string, error) {
	rows, err := s.rd().Query(`SELECT message_id FROM hidden_messages WHERE channel_id = ? AND hidden = 1`, channelID)
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

// HideRecordsAfter pages through the registers (tombstones included) changed
// here after local stamp seq, oldest change first. It returns the stamp of the
// last row returned, to resume from, and whether more remain.
func (s *Store) HideRecordsAfter(seq int64, limit int) (recs []HideRec, last int64, more bool, err error) {
	if limit <= 0 {
		limit = 150
	}
	rows, err := s.rd().Query(
		`SELECT channel_id, message_id, hidden, at, seq FROM hidden_messages WHERE seq > ? ORDER BY seq LIMIT ?`,
		seq, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var r HideRec
		var h int
		var sq int64
		if err := rows.Scan(&r.Channel, &r.ID, &h, &r.At, &sq); err != nil {
			return nil, 0, false, err
		}
		if len(recs) == limit {
			return recs, last, true, nil
		}
		r.Hidden = h == 1
		recs = append(recs, r)
		last = sq
	}
	return recs, last, false, rows.Err()
}

// SweepHidden bounds the table: it drops registers (hides and tombstones)
// whose message is gone (deleted for everyone, or pruned) and ones that never
// met a message within hiddenOrphanTTL. Returns the rows removed.
func (s *Store) SweepHidden(now time.Time) (int, error) {
	res, err := s.db.Exec(
		`DELETE FROM hidden_messages
		 WHERE message_id IN (SELECT id FROM messages WHERE deleted = 1)
		    OR (message_id NOT IN (SELECT id FROM messages) AND at < ?)`,
		now.Add(-hiddenOrphanTTL).UnixMilli())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
