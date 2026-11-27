package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Actions on a message (a reaction, a pin, an edit, a delete) are not rows of
// their own, so "a message id is stored once" did not cover them. Two of them
// are toggles: applied twice, a reaction or a pin is undone. A reaction sent
// again after a failure (the composer's retry, or the core's own after a disk
// error), or one that reaches a peer by two paths, used to switch itself off.
// applied_actions records each action's id as it is applied, in the same
// transaction as the change, so an action is applied once per id on every
// device, and a change that fails records nothing and is applied when tried
// again.

// appliedActionKeep is how long an applied action's id is remembered. A copy
// can turn up for as long as a mailbox holds one or a held action waits for its
// target; past that, nothing delivers it again.
const appliedActionKeep = 60 * 24 * time.Hour

// The steps a test can fail with InjectFault.
const (
	OpSaveMessage    = "save-message"
	OpSaveAttachment = "save-attachment"
	OpSetSetting     = "set-setting"
	OpReadMessage    = "read-message"
	OpEdit           = "edit"
	OpPin            = "pin"
	OpReact          = "react"
	OpAction         = "action" // recording an action as applied
)

// InjectFault makes the store step named by op fail with what f returns, while
// f is set and returns an error for it. Tests only: a disk that fails (or
// fills) at the worst moment. Nothing in the product sets it.
func (s *Store) InjectFault(f func(op string) error) {
	if f == nil {
		s.fault.Store(nil)
		return
	}
	s.fault.Store(&f)
}

func (s *Store) failAt(op string) error {
	if f := s.fault.Load(); f != nil {
		return (*f)(op)
	}
	return nil
}

// ActionApplied reports whether the action with this id has been applied here.
func (s *Store) ActionApplied(id string) (bool, error) {
	var one int
	err := s.rd().QueryRow(`SELECT 1 FROM applied_actions WHERE id = ?`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// noteAction records id as applied inside tx; fresh is false when it already
// was, in which case the caller changes nothing.
func (s *Store) noteAction(tx *sql.Tx, id string) (fresh bool, err error) {
	if err := s.failAt(OpAction); err != nil {
		return false, fmt.Errorf("store: note action: %w", err)
	}
	res, err := tx.Exec(`INSERT INTO applied_actions (id, at) VALUES (?, ?) ON CONFLICT(id) DO NOTHING`, id, time.Now().UnixNano())
	if err != nil {
		return false, fmt.Errorf("store: note action: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// NoteActionApplied records an idempotent action (an edit, a delete) as
// applied, after the change itself. fresh is false when it already was.
func (s *Store) NoteActionApplied(id string) (fresh bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if fresh, err = s.noteAction(tx, id); err != nil {
		return false, err
	}
	return fresh, tx.Commit()
}

// ToggleReactionOnce is ToggleReaction for the action with id actionID:
// applied reports whether it changed anything, which it does once per id.
// added is the reaction's state after it, when applied.
func (s *Store) ToggleReactionOnce(actionID, messageID, fingerprint, emoji string) (added, applied bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	fresh, err := s.noteAction(tx, actionID)
	if err != nil || !fresh {
		return false, false, err
	}
	// A deleted message takes no reactions: its delete removed them.
	var deleted int
	if err := tx.QueryRow(`SELECT deleted FROM messages WHERE id = ?`, messageID).Scan(&deleted); err == nil && deleted != 0 {
		return false, false, tx.Commit()
	}
	if err := s.failAt(OpReact); err != nil {
		return false, false, fmt.Errorf("store: react: %w", err)
	}
	var one int
	err = tx.QueryRow(`SELECT 1 FROM reactions WHERE message_id=? AND fingerprint=? AND emoji=?`, messageID, fingerprint, emoji).Scan(&one)
	switch {
	case err == nil:
		if _, err := tx.Exec(`DELETE FROM reactions WHERE message_id=? AND fingerprint=? AND emoji=?`, messageID, fingerprint, emoji); err != nil {
			return false, false, err
		}
	case err == sql.ErrNoRows:
		if _, err := tx.Exec(`INSERT INTO reactions (message_id, fingerprint, emoji) VALUES (?, ?, ?)`, messageID, fingerprint, emoji); err != nil {
			return false, false, err
		}
		added = true
	default:
		return false, false, err
	}
	if _, err := tx.Exec(`UPDATE messages SET updated = ? WHERE id = ?`, time.Now().UnixNano(), messageID); err != nil {
		return false, false, err
	}
	if err := tx.Commit(); err != nil {
		return false, false, err
	}
	return added, true, nil
}

// TogglePinnedOnce is TogglePinned for the action with id actionID, applied
// once per id. A deleted message is not pinned again.
func (s *Store) TogglePinnedOnce(actionID, messageID string) (pinned, applied bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	fresh, err := s.noteAction(tx, actionID)
	if err != nil || !fresh {
		return false, false, err
	}
	var was, deleted int
	err = tx.QueryRow(`SELECT pinned, deleted FROM messages WHERE id = ?`, messageID).Scan(&was, &deleted)
	if err == sql.ErrNoRows {
		return false, false, nil // no row to pin: the action is not spent on it
	}
	if err == nil && deleted != 0 {
		return false, false, tx.Commit()
	}
	if err != nil {
		return false, false, err
	}
	if err := s.failAt(OpPin); err != nil {
		return false, false, fmt.Errorf("store: pin: %w", err)
	}
	now := 1 - was
	if _, err := tx.Exec(`UPDATE messages SET pinned = ?, updated = ? WHERE id = ?`, now, time.Now().UnixNano(), messageID); err != nil {
		return false, false, err
	}
	if err := tx.Commit(); err != nil {
		return false, false, err
	}
	return now == 1, true, nil
}

// PruneAppliedActions forgets actions applied longer ago than appliedActionKeep.
func (s *Store) PruneAppliedActions(now time.Time) error {
	_, err := s.db.Exec(`DELETE FROM applied_actions WHERE at < ?`, now.Add(-appliedActionKeep).UnixNano())
	return err
}

// DeleteMessagesOfKind removes every row of one local-only kind, returning how
// many went. It is for notices this device wrote for itself and has stopped
// writing (the "added a new device" line), never for anything a peer sent.
func (s *Store) DeleteMessagesOfKind(kind string) (int64, error) {
	if kind == "" || kind == "system" {
		return 0, nil
	}
	res, err := s.db.Exec(`DELETE FROM messages WHERE kind = ?`, kind)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
