package store

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/mediatok"
)

// media.go is the gallery's index: one row per attachment of a live message,
// so "every photo in this conversation" is a page of SQL instead of opening
// every body the channel holds.
//
// What is in the clear: which messages carry an attachment, of which kind
// (photo, video, file), in which channel and when. The channel and the time
// are clear on the message row already; the kind is new, and DESIGN.md §11.1
// lists it. No blob id, no key, no name: an item's details stay in the sealed
// body, and a page opens the bodies of its own rows and nothing else.
//
// Keeping it in step: the messages table carries triggers, so any change to a
// body, a tombstone, a channel move and any delete, whichever function does
// it, drops the message's index rows and (for a live row) puts the message on
// media_pending. A new row is queued by the two functions that insert one
// (SaveMessage, upsertSyncedMessage, through queueMedia), and only when its
// body names an attachment: an insert trigger cost every message a second
// b-tree write, a quarter of the sync-batch time. A drain (Go, since it has
// to open the body) turns pending messages into index rows. The page query then
// repeats the feed's own filters (deleted, hidden, kind, an expiry
// not yet swept) on every read, so a row the drain has not caught up with can
// be missing for a moment but never shown wrongly.

// mediaIndexKey records that the one-time enqueue of the history already on
// disk has been done.
const mediaIndexKey = "store.media_index_v1"

func (s *Store) migrateMedia() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS media_index (
  message_id TEXT    NOT NULL,
  ord        INTEGER NOT NULL,
  channel_id TEXT    NOT NULL,
  tab        INTEGER NOT NULL,
  kind       INTEGER NOT NULL,
  sent       INTEGER NOT NULL,
  expires    INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (message_id, ord)
);
CREATE INDEX IF NOT EXISTS idx_media_page ON media_index(channel_id, tab, sent DESC, message_id DESC, ord DESC);
CREATE INDEX IF NOT EXISTS idx_media_recent ON media_index(sent DESC, message_id DESC, ord DESC);
CREATE TABLE IF NOT EXISTS media_pending (
  channel_id TEXT NOT NULL,
  message_id TEXT NOT NULL,
  PRIMARY KEY (channel_id, message_id)
) WITHOUT ROWID;
CREATE TRIGGER IF NOT EXISTS media_msg_update AFTER UPDATE OF content_enc, nonce, deleted, channel_id, sent, kind ON messages
BEGIN
  DELETE FROM media_index WHERE message_id = OLD.id;
  DELETE FROM media_pending WHERE channel_id = OLD.channel_id AND message_id = OLD.id;
  INSERT OR IGNORE INTO media_pending (message_id, channel_id)
    SELECT NEW.id, NEW.channel_id WHERE NEW.deleted = 0;
END;
CREATE TRIGGER IF NOT EXISTS media_msg_delete AFTER DELETE ON messages
BEGIN
  DELETE FROM media_index WHERE message_id = OLD.id;
  DELETE FROM media_pending WHERE channel_id = OLD.channel_id AND message_id = OLD.id;
END;`); err != nil {
		return fmt.Errorf("store: migrate media: %w", err)
	}
	// The history written before the index existed: queued once, in one
	// statement (ids only, nothing opened), and drained after Open returns.
	if v, _ := s.GetSetting(mediaIndexKey); v == "" {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("store: migrate media: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := tx.Exec(`INSERT OR IGNORE INTO media_pending (message_id, channel_id)
			SELECT id, channel_id FROM messages WHERE deleted = 0`); err != nil {
			return fmt.Errorf("store: migrate media: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, '1')
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, mediaIndexKey); err != nil {
			return fmt.Errorf("store: migrate media: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: migrate media: %w", err)
		}
	}
	return nil
}

// queueMedia puts a newly inserted live row on the queue when its body may
// hold an attachment. Both functions that insert into messages call it; a
// third would have to as well (TestEveryMessageInsertQueuesMedia counts).
func queueMedia(q querier, m domain.Message) error {
	if m.Deleted || !strings.Contains(m.Content, "concord://") {
		return nil
	}
	_, err := q.Exec(`INSERT OR IGNORE INTO media_pending (message_id, channel_id) VALUES (?, ?)`, m.ID, m.ChannelID)
	return err
}

// mediaDrainChunk bounds one drain step: the step runs in one write
// transaction (read the pending bodies, open them, write their rows), so it
// is also how long the writer is held. 250 short bodies is a few
// milliseconds.
const mediaDrainChunk = 250

// drainMedia indexes pending messages, those of one channel when channelID is
// set, at most limit of them (all when limit <= 0), a chunk per transaction.
// It reports how many it took off the queue.
func (s *Store) drainMedia(channelID string, limit int) (int, error) {
	done := 0
	for limit <= 0 || done < limit {
		n := mediaDrainChunk
		if limit > 0 && limit-done < n {
			n = limit - done
		}
		got, err := s.drainMediaChunk(channelID, n)
		done += got
		if err != nil || got < n {
			return done, err
		}
	}
	return done, nil
}

func (s *Store) drainMediaChunk(channelID string, n int) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	q := `SELECT p.channel_id, p.message_id, m.channel_id, m.sent, m.kind, m.deleted, m.content_enc, m.nonce
	      FROM media_pending p JOIN messages m ON m.id = p.message_id`
	args := []any{}
	if channelID != "" {
		q += ` WHERE p.channel_id = ?`
		args = append(args, channelID)
	}
	q += ` LIMIT ?`
	args = append(args, n)
	rows, err := tx.Query(q, args...)
	if err != nil {
		return 0, err
	}
	type row struct {
		id, channel string
		sent        int64
		tab         mediatok.Tab
		kind        mediatok.Kind
		ord         int
		expires     int64
	}
	var done []any // (channel, message) pairs taken off the queue
	var out []row
	now := time.Now().Unix()
	for rows.Next() {
		var pch, id, channel, kind string
		var sent int64
		var deleted int
		var enc, nonce []byte
		if err := rows.Scan(&pch, &id, &channel, &sent, &kind, &deleted, &enc, &nonce); err != nil {
			rows.Close()
			return 0, err
		}
		done = append(done, pch, id)
		// Only what the feed shows as somebody's message is a gallery item.
		// (The quiet flag is not consulted: a quiet body is a lone game token
		// and holds no attachment, and an edit does not recompute the flag.)
		if deleted != 0 || (kind != "" && kind != "guest") {
			continue
		}
		body, err := s.open(enc, nonce)
		if err != nil {
			continue
		}
		items := mediatok.Parse(body)
		if len(items) == 0 {
			continue
		}
		// The body's own deadline, read here as well as from the row: a
		// disappearing message written before the store kept deadlines has
		// expires = 0 on its row until that backfill reaches it.
		exp := EphemeralExpiry(body)
		if exp > 0 && exp <= now {
			continue
		}
		for _, it := range items {
			out = append(out, row{id, channel, sent, mediatok.TabOf(it.Kind), it.Kind, it.Ord, exp})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	// Pending ids whose message row is gone are dropped by the delete
	// trigger, so every id read here is a real row.
	for _, r := range out {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO media_index (message_id, ord, channel_id, tab, kind, sent, expires)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, r.id, r.ord, r.channel, int(r.tab), int(r.kind), r.sent, r.expires); err != nil {
			return 0, err
		}
	}
	for start := 0; start < len(done); start += 400 {
		end := min(start+400, len(done))
		if _, err := tx.Exec(`DELETE FROM media_pending WHERE (channel_id, message_id) IN (VALUES `+pairs((end-start)/2)+`)`, done[start:end]...); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(done) / 2, nil
}

// pairs is n "(?, ?)" row values for a VALUES list.
func pairs(n int) string {
	return strings.TrimSuffix(strings.Repeat("(?, ?),", n), ",")
}

// MediaPending reports whether messages are still waiting to be indexed, in
// one channel or (channelID "") anywhere.
func (s *Store) MediaPending(channelID string) bool {
	var one int
	var err error
	if channelID == "" {
		err = s.rd().QueryRow(`SELECT 1 FROM media_pending LIMIT 1`).Scan(&one)
	} else {
		err = s.rd().QueryRow(`SELECT 1 FROM media_pending WHERE channel_id = ? LIMIT 1`, channelID).Scan(&one)
	}
	return err == nil
}

// mediaDrainEvery is how often the background drain looks for work. A page
// drains its own channel first, so this only keeps the queue short.
var mediaDrainEvery = 30 * time.Second

// mediaDrainAfterBackfill staggers the first drain after the deadline
// backfill's start, so the two never queue on the writer together at unlock.
const mediaDrainAfterBackfill = 3 * time.Second

// mediaDrainLoop indexes whatever is pending, a chunk at a time with a pause
// between chunks, until the store closes. It starts after the deadline
// backfill's delay: the unlock is busy enough.
func (s *Store) mediaDrainLoop(delay time.Duration) {
	defer s.bg.Done()
	wait := delay
	for {
		select {
		case <-s.closing:
			return
		case <-time.After(wait):
		}
		wait = mediaDrainEvery
		for {
			n, err := s.drainMediaChunk("", mediaDrainChunk)
			if err != nil || n < mediaDrainChunk {
				break
			}
			select {
			case <-s.closing:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
}

// MediaItem is one gallery entry: an index row joined to its message, with
// the item read out of the message's body.
type MediaItem struct {
	MessageID  string
	ChannelID  string
	Sender     []byte
	SenderName string
	Sent       int64 // UnixNano
	Expires    int64 // unix seconds, 0 = none
	// Unverified and Guest are the message row's own: the feed marks an
	// unsigned relayed copy from them, and the gallery shows the same mark.
	Unverified bool
	Guest      bool
	Item       mediatok.Item
}

// MediaCursor is a keyset position: the page after it starts strictly older.
// The zero cursor is the newest end.
type MediaCursor struct {
	Sent      int64
	MessageID string
	Ord       int
}

// String is the cursor as the UI carries it.
func (c MediaCursor) String() string {
	if c.MessageID == "" {
		return ""
	}
	return strconv.FormatInt(c.Sent, 10) + ":" + strconv.Itoa(c.Ord) + ":" + c.MessageID
}

// ParseMediaCursor reads String back; anything else is the newest end.
func ParseMediaCursor(s string) MediaCursor {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || parts[2] == "" {
		return MediaCursor{}
	}
	sent, err1 := strconv.ParseInt(parts[0], 10, 64)
	ord, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return MediaCursor{}
	}
	return MediaCursor{Sent: sent, MessageID: parts[2], Ord: ord}
}

// MediaPageMax caps one page.
const MediaPageMax = 2000

// mediaPageQuery is the feed's own rule for which rows are somebody's
// message to show, repeated here rather than trusted to the index: live, a
// person's or a relayed guest's message, not hidden on
// this account, and not past a disappearing deadline the sweeper has yet to
// act on (the row's or the one read from the body).
const mediaPageQuery = `
SELECT mi.message_id, mi.ord, mi.kind, mi.sent, mi.expires, m.expires, m.sender, m.name, m.unverified, m.kind, m.content_enc, m.nonce
FROM media_index mi JOIN messages m ON m.id = mi.message_id
WHERE mi.channel_id = ? AND mi.tab = ?
  AND m.deleted = 0 AND m.kind IN ('', 'guest')
  AND (m.expires = 0 OR m.expires > ?) AND (mi.expires = 0 OR mi.expires > ?)
  AND NOT EXISTS (SELECT 1 FROM hidden_messages h WHERE h.message_id = mi.message_id AND h.hidden = 1)
  AND (mi.sent, mi.message_id, mi.ord) < (?, ?, ?)
ORDER BY mi.sent DESC, mi.message_id DESC, mi.ord DESC
LIMIT ?`

// MediaPage returns up to limit items of one tab of a channel, newest first,
// starting after cursor, and the cursor for the next page ("" at the end).
// It indexes the channel's pending messages first, so what it returns is
// complete up to the moment it was asked. Only the bodies of the rows on the
// page are opened.
func (s *Store) MediaPage(channelID string, tab mediatok.Tab, cursor MediaCursor, limit int) ([]MediaItem, MediaCursor, error) {
	if limit <= 0 || limit > MediaPageMax {
		limit = MediaPageMax
	}
	if _, err := s.drainMedia(channelID, 0); err != nil {
		return nil, MediaCursor{}, err
	}
	if cursor.MessageID == "" {
		cursor = MediaCursor{Sent: int64(^uint64(0) >> 1), MessageID: "\U0010FFFF", Ord: 1 << 30}
	}
	now := time.Now().Unix()
	rows, err := s.rd().Query(mediaPageQuery, channelID, int(tab), now, now, cursor.Sent, cursor.MessageID, cursor.Ord, limit)
	if err != nil {
		return nil, MediaCursor{}, err
	}
	defer rows.Close()
	var out []MediaItem
	var last MediaCursor
	n := 0
	for rows.Next() {
		var id string
		var ord, kind int
		var sent, miExp, mExp int64
		var sender []byte
		var name, mkind string
		var unverified int
		var enc, nonce []byte
		if err := rows.Scan(&id, &ord, &kind, &sent, &miExp, &mExp, &sender, &name, &unverified, &mkind, &enc, &nonce); err != nil {
			return nil, MediaCursor{}, err
		}
		n++
		last = MediaCursor{Sent: sent, MessageID: id, Ord: ord}
		body, err := s.open(enc, nonce)
		if err != nil {
			continue
		}
		it, ok := mediatok.At(body, ord, mediatok.Kind(kind))
		if !ok {
			continue
		}
		exp := max(miExp, mExp)
		out = append(out, MediaItem{MessageID: id, ChannelID: channelID, Sender: sender, SenderName: name, Sent: sent, Expires: exp,
			Unverified: unverified != 0, Guest: mkind == "guest", Item: it})
	}
	if err := rows.Err(); err != nil {
		return nil, MediaCursor{}, err
	}
	if n < limit {
		last = MediaCursor{}
	}
	return out, last, nil
}

// recentDrainMax bounds the indexing a "Recent" query does before it reads:
// the strip asks each time the attach menu opens, so it must not be the one
// that indexes a whole imported history (the background drain does that).
const recentDrainMax = 500

// mediaRecentQuery is mediaPageQuery across every channel, stricter in one
// way: only rows signed by their sender (no unverified relayed copy, no guest
// row), because a re-send puts this account's own signature on the bytes.
const mediaRecentQuery = `
SELECT mi.message_id, mi.channel_id, mi.ord, mi.kind, mi.sent, mi.expires, m.expires, m.sender, m.name, m.content_enc, m.nonce
FROM media_index mi JOIN messages m ON m.id = mi.message_id
WHERE m.deleted = 0 AND m.kind = '' AND m.unverified = 0
  AND (m.expires = 0 OR m.expires > ?) AND (mi.expires = 0 OR mi.expires > ?)
  AND NOT EXISTS (SELECT 1 FROM hidden_messages h WHERE h.message_id = mi.message_id AND h.hidden = 1)
  AND (mi.sent, mi.message_id, mi.ord) < (?, ?, ?)
ORDER BY mi.sent DESC, mi.message_id DESC, mi.ord DESC
LIMIT ?`

// RecentMedia returns up to limit gallery items from every channel this
// device holds, newest first, starting after cursor, and the cursor for the
// next page ("" at the end). It is the attach menu's "Recent" strip; the
// caller drops the channels it no longer belongs to. The first page indexes
// at most recentDrainMax pending messages first.
func (s *Store) RecentMedia(cursor MediaCursor, limit int) ([]MediaItem, MediaCursor, error) {
	if limit <= 0 || limit > MediaPageMax {
		limit = MediaPageMax
	}
	if cursor.MessageID == "" {
		if _, err := s.drainMedia("", recentDrainMax); err != nil {
			return nil, MediaCursor{}, err
		}
		cursor = MediaCursor{Sent: int64(^uint64(0) >> 1), MessageID: "\U0010FFFF", Ord: 1 << 30}
	}
	now := time.Now().Unix()
	rows, err := s.rd().Query(mediaRecentQuery, now, now, cursor.Sent, cursor.MessageID, cursor.Ord, limit)
	if err != nil {
		return nil, MediaCursor{}, err
	}
	defer rows.Close()
	var out []MediaItem
	var last MediaCursor
	n := 0
	for rows.Next() {
		var id, channel, name string
		var ord, kind int
		var sent, miExp, mExp int64
		var sender, enc, nonce []byte
		if err := rows.Scan(&id, &channel, &ord, &kind, &sent, &miExp, &mExp, &sender, &name, &enc, &nonce); err != nil {
			return nil, MediaCursor{}, err
		}
		n++
		last = MediaCursor{Sent: sent, MessageID: id, Ord: ord}
		body, err := s.open(enc, nonce)
		if err != nil {
			continue
		}
		it, ok := mediatok.At(body, ord, mediatok.Kind(kind))
		if !ok {
			continue
		}
		out = append(out, MediaItem{MessageID: id, ChannelID: channel, Sender: sender, SenderName: name, Sent: sent,
			Expires: max(miExp, mExp), Item: it})
	}
	if err := rows.Err(); err != nil {
		return nil, MediaCursor{}, err
	}
	if n < limit {
		last = MediaCursor{}
	}
	return out, last, nil
}

// HasAttachment reports whether a blob's ciphertext is on this device,
// without reading it or touching its recency: a gallery tile asks this for
// every blob that scrolls into view.
func (s *Store) HasAttachment(blobID string) bool {
	var one int
	return s.rd().QueryRow(`SELECT 1 FROM attachments WHERE blob_id = ?`, blobID).Scan(&one) == nil
}
