package store

import (
	"encoding/json"
	"fmt"
)

// Receipts: what the recipients of OUR messages have told us about them. One
// row per message per recipient account, at most, holding the second each
// state was reported (0 = not reported). Rows are removed with their message
// (channel delete, guild delete, retention prune).
//
// This is DM-only state (see internal/app/receipts.go); nothing here knows
// about members, it only enforces that a receipt can attach to a message of
// ours in the channel it names.

// Receipt is one recipient account's state for one message (unix seconds).
type Receipt struct {
	Account   string
	Delivered int64
	Read      int64
}

// RecordDelivered notes that account has received and applied the given
// messages. Only ids that are messages of ownKey in channelID count; anything
// else is ignored. Returns the ids whose state changed.
func (s *Store) RecordDelivered(ownKey []byte, channelID, account string, ids []string, at int64) ([]string, error) {
	if len(ids) == 0 || account == "" {
		return nil, nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	return s.recordReceipt(`
		SELECT m.id FROM messages m, json_each(?) j
		 WHERE m.id = j.value AND m.channel_id = ? AND m.sender = ? AND m.kind = '' AND m.deleted = 0
		   AND NOT EXISTS (SELECT 1 FROM msg_receipts r WHERE r.message_id = m.id AND r.account = ? AND r.delivered > 0)`,
		[]any{string(raw), channelID, ownKey, account}, channelID, account, at, false)
}

// RecordRead notes that account read our messages in channelID sent after
// sinceNano and up to throughNano (UnixNano, as stored). Read implies delivered.
// Returns the ids whose state changed.
func (s *Store) RecordRead(ownKey []byte, channelID, account string, sinceNano, throughNano, at int64) ([]string, error) {
	if account == "" || throughNano <= sinceNano {
		return nil, nil
	}
	return s.recordReceipt(`
		SELECT m.id FROM messages m
		 WHERE m.channel_id = ? AND m.sender = ? AND m.kind = '' AND m.deleted = 0
		   AND m.sent > ? AND m.sent <= ?
		   AND NOT EXISTS (SELECT 1 FROM msg_receipts r WHERE r.message_id = m.id AND r.account = ? AND r.read > 0)`,
		[]any{channelID, ownKey, sinceNano, throughNano, account}, channelID, account, at, true)
}

func (s *Store) recordReceipt(selectSQL string, args []any, channelID, account string, at int64, read bool) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	rows, err := tx.Query(selectSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("store: receipts select: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if read {
			_, err = tx.Exec(`INSERT INTO msg_receipts (message_id, channel_id, account, delivered, read) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(message_id, account) DO UPDATE SET read = excluded.read,
				  delivered = CASE WHEN delivered = 0 THEN excluded.delivered ELSE delivered END`,
				id, channelID, account, at, at)
		} else {
			_, err = tx.Exec(`INSERT INTO msg_receipts (message_id, channel_id, account, delivered) VALUES (?, ?, ?, ?)
				ON CONFLICT(message_id, account) DO UPDATE SET delivered = excluded.delivered WHERE delivered = 0`,
				id, channelID, account, at)
		}
		if err != nil {
			return nil, fmt.Errorf("store: receipts upsert: %w", err)
		}
	}
	return ids, tx.Commit()
}

// ReceiptsFor returns the stored receipts for a channel's messages, by message id.
func (s *Store) ReceiptsFor(channelID string) (map[string][]Receipt, error) {
	rows, err := s.rd().Query(`SELECT message_id, account, delivered, read FROM msg_receipts WHERE channel_id = ?`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Receipt{}
	for rows.Next() {
		var id string
		var r Receipt
		if err := rows.Scan(&id, &r.Account, &r.Delivered, &r.Read); err != nil {
			return nil, err
		}
		out[id] = append(out[id], r)
	}
	return out, rows.Err()
}

// ClearReadReceipts forgets every stored read receipt (delivered stays). Used
// when the person turns read receipts off: what they were told while the
// setting was on is not kept once it is off.
func (s *Store) ClearReadReceipts() error {
	_, err := s.db.Exec(`UPDATE msg_receipts SET read = 0 WHERE read > 0`)
	return err
}
