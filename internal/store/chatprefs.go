package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ChatPref is one chat's pin and archive state. These never leave the
// account's own devices. Each of the two facts carries its own stamp
// (UnixMilli) so concurrent changes on two devices converge per fact: the
// later stamp wins. A cleared fact keeps its row and stamp, so an old "pinned"
// arriving late cannot undo a newer unpin.
type ChatPref struct {
	Pinned   bool    `json:"p,omitempty"`
	PinKey   float64 `json:"k,omitempty"` // pin order, ascending: smallest is first
	PinAt    int64   `json:"pa,omitempty"`
	Archived bool    `json:"a,omitempty"`
	ArchAt   int64   `json:"aa,omitempty"`
}

// ChatPrefSkew bounds how far ahead of the local clock a stamp may sit, so one
// device with a fast clock cannot win every later change.
const ChatPrefSkew = 2 * time.Minute

var chatPrefMu sync.Mutex

// newerPin reports whether a beats b. Ties break on content so both devices
// pick the same winner.
func pinWins(a, b ChatPref) bool {
	if a.PinAt != b.PinAt {
		return a.PinAt > b.PinAt
	}
	if a.Pinned != b.Pinned {
		return a.Pinned
	}
	return a.PinKey > b.PinKey
}

func archWins(a, b ChatPref) bool {
	if a.ArchAt != b.ArchAt {
		return a.ArchAt > b.ArchAt
	}
	return a.Archived && !b.Archived
}

// MergeChatPref folds in a record, local or remote, stamps capped at
// now+ChatPrefSkew. It returns the stored result and whether anything changed.
func (s *Store) MergeChatPref(chatID string, in ChatPref, now time.Time) (ChatPref, bool, error) {
	if chatID == "" {
		return ChatPref{}, false, nil
	}
	lim := now.Add(ChatPrefSkew).UnixMilli()
	if in.PinAt > lim {
		in.PinAt = lim
	}
	if in.ArchAt > lim {
		in.ArchAt = lim
	}
	chatPrefMu.Lock()
	defer chatPrefMu.Unlock()
	cur, err := s.chatPref(chatID)
	if err != nil {
		return ChatPref{}, false, err
	}
	next := cur
	if in.PinAt > 0 && (cur.PinAt == 0 || pinWins(in, cur)) {
		next.Pinned, next.PinKey, next.PinAt = in.Pinned, in.PinKey, in.PinAt
	}
	if in.ArchAt > 0 && (cur.ArchAt == 0 || archWins(in, cur)) {
		next.Archived, next.ArchAt = in.Archived, in.ArchAt
	}
	if next == cur {
		return cur, false, nil
	}
	_, err = s.db.Exec(
		`INSERT INTO chat_prefs (chat_id, pinned, pin_key, pin_at, archived, arch_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET pinned=excluded.pinned, pin_key=excluded.pin_key,
		   pin_at=excluded.pin_at, archived=excluded.archived, arch_at=excluded.arch_at`,
		chatID, b2i(next.Pinned), next.PinKey, next.PinAt, b2i(next.Archived), next.ArchAt)
	if err != nil {
		return cur, false, fmt.Errorf("store: merge chat pref: %w", err)
	}
	return next, true, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) chatPref(chatID string) (ChatPref, error) {
	var p ChatPref
	var pn, ar int
	err := s.db.QueryRow(
		`SELECT pinned, pin_key, pin_at, archived, arch_at FROM chat_prefs WHERE chat_id = ?`,
		chatID).Scan(&pn, &p.PinKey, &p.PinAt, &ar, &p.ArchAt)
	if err == sql.ErrNoRows {
		return ChatPref{}, nil
	}
	p.Pinned, p.Archived = pn != 0, ar != 0
	return p, err
}

// ChatPrefs returns every chat's record (cleared ones included: they carry the
// stamps that make a late older change lose).
func (s *Store) ChatPrefs() (map[string]ChatPref, error) {
	rows, err := s.rd().Query(`SELECT chat_id, pinned, pin_key, pin_at, archived, arch_at FROM chat_prefs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ChatPref{}
	for rows.Next() {
		var id string
		var p ChatPref
		var pn, ar int
		if err := rows.Scan(&id, &pn, &p.PinKey, &p.PinAt, &ar, &p.ArchAt); err != nil {
			return nil, err
		}
		p.Pinned, p.Archived = pn != 0, ar != 0
		out[id] = p
	}
	return out, rows.Err()
}

// DeleteChatPrefs drops the records of chats that no longer exist.
func (s *Store) DeleteChatPrefs(chatIDs []string) error {
	chatPrefMu.Lock()
	defer chatPrefMu.Unlock()
	for _, id := range chatIDs {
		if _, err := s.db.Exec(`DELETE FROM chat_prefs WHERE chat_id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// PruneChatPrefs deletes every record whose chat is not in keep, returning the
// ids it removed.
func (s *Store) PruneChatPrefs(keep map[string]bool) ([]string, error) {
	all, err := s.ChatPrefs()
	if err != nil {
		return nil, err
	}
	var gone []string
	for id := range all {
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	return gone, s.DeleteChatPrefs(gone)
}
