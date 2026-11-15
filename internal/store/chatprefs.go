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
	// Timer is the chat's disappearing-messages timer in seconds (0 = off),
	// which the core stamps on every message sent into it (app/chattimer.go).
	Timer   int64 `json:"t,omitempty"`
	TimerAt int64 `json:"ta,omitempty"`
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

func timerWins(a, b ChatPref) bool {
	if a.TimerAt != b.TimerAt {
		return a.TimerAt > b.TimerAt
	}
	return a.Timer > b.Timer
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
	if chatID == "" || len(chatID) > maxChatIDLen {
		return ChatPref{}, false, nil
	}
	lim := now.Add(ChatPrefSkew).UnixMilli()
	if in.PinAt > lim {
		in.PinAt = lim
	}
	if in.ArchAt > lim {
		in.ArchAt = lim
	}
	if in.TimerAt > lim {
		in.TimerAt = lim
	}
	if !ValidChatTimer(in.Timer) {
		in.Timer, in.TimerAt = 0, 0 // not a timer anyone offered: take no part of it
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
	if in.TimerAt > 0 && (cur.TimerAt == 0 || timerWins(in, cur)) {
		next.Timer, next.TimerAt = in.Timer, in.TimerAt
	}
	if next == cur {
		return cur, false, nil
	}
	if cur == (ChatPref{}) {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_prefs`).Scan(&n); err != nil {
			return cur, false, err
		}
		if n >= chatPrefCap {
			return cur, false, nil // full: a new chat's record is not taken
		}
	}
	_, err = s.db.Exec(
		`INSERT INTO chat_prefs (chat_id, pinned, pin_key, pin_at, archived, arch_at, timer, timer_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET pinned=excluded.pinned, pin_key=excluded.pin_key,
		   pin_at=excluded.pin_at, archived=excluded.archived, arch_at=excluded.arch_at,
		   timer=excluded.timer, timer_at=excluded.timer_at`,
		chatID, b2i(next.Pinned), next.PinKey, next.PinAt, b2i(next.Archived), next.ArchAt, next.Timer, next.TimerAt)
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
		`SELECT pinned, pin_key, pin_at, archived, arch_at, timer, timer_at FROM chat_prefs WHERE chat_id = ?`,
		chatID).Scan(&pn, &p.PinKey, &p.PinAt, &ar, &p.ArchAt, &p.Timer, &p.TimerAt)
	if err == sql.ErrNoRows {
		return ChatPref{}, nil
	}
	p.Pinned, p.Archived = pn != 0, ar != 0
	return p, err
}

// ChatPrefs returns every chat's record (cleared ones included: they carry the
// stamps that make a late older change lose).
func (s *Store) ChatPrefs() (map[string]ChatPref, error) {
	rows, err := s.rd().Query(`SELECT chat_id, pinned, pin_key, pin_at, archived, arch_at, timer, timer_at FROM chat_prefs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ChatPref{}
	for rows.Next() {
		var id string
		var p ChatPref
		var pn, ar int
		if err := rows.Scan(&id, &pn, &p.PinKey, &p.PinAt, &ar, &p.ArchAt, &p.Timer, &p.TimerAt); err != nil {
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

// chatTimers are the disappearing timers the app offers, in seconds.
var chatTimers = map[int64]bool{0: true, 300: true, 3600: true, 28800: true, 86400: true, 604800: true}

// ValidChatTimer reports whether secs is one of them (0 = off).
func ValidChatTimer(secs int64) bool { return chatTimers[secs] }

// ChatTimer returns a chat's disappearing timer in seconds, 0 when off.
func (s *Store) ChatTimer(chatID string) int64 {
	p, err := s.chatPref(chatID)
	if err != nil {
		return 0
	}
	return p.Timer
}

// migrateChatTimer adds the timer columns to a chat_prefs table made before
// them.
func (s *Store) migrateChatTimer() error {
	for _, col := range []string{"timer", "timer_at"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('chat_prefs') WHERE name = ?`, col).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := s.db.Exec(`ALTER TABLE chat_prefs ADD COLUMN ` + col + ` INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
	}
	return nil
}

// MaxChatPrefs bounds the table. A record only ever comes from this account's
// own devices, but a device that misbehaved must still not grow it for good.
const MaxChatPrefs = 4096

// chatPrefCap is MaxChatPrefs; tests lower it.
var chatPrefCap = MaxChatPrefs

// maxChatIDLen is far above any channel id; anything longer is not one.
const maxChatIDLen = 128

// ChatPrefTTL is how long a record for a chat this device does not have is
// kept: long enough for a pin synced ahead of the chat itself to meet it.
const ChatPrefTTL = 90 * 24 * time.Hour

// SweepChatPrefs drops the records of chats this device does not have whose
// newest change is older than ChatPrefTTL. It returns how many went.
func (s *Store) SweepChatPrefs(have func(chatID string) bool, now time.Time) (int, error) {
	all, err := s.ChatPrefs()
	if err != nil {
		return 0, err
	}
	cut := now.Add(-ChatPrefTTL).UnixMilli()
	var gone []string
	for id, p := range all {
		if have(id) {
			continue
		}
		if last := max(p.PinAt, p.ArchAt, p.TimerAt); last < cut {
			gone = append(gone, id)
		}
	}
	return len(gone), s.DeleteChatPrefs(gone)
}
