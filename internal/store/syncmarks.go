package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// syncmarks.go is the bookkeeping that lets history sync ask a peer "what has
// changed on YOUR side since we last talked", instead of "what was written
// after the newest thing I hold".
//
// The second question is the one sync used to ask, with a cursor taken from the
// newest send or update stamp in the channel. It cannot be answered correctly.
// The newest stamp moves for reasons that say nothing about what is missing —
// one live message heard after hours offline puts it at now, and the backlog
// below it is never requested; a reaction stamps an old row with a fresh time
// and jumps it past rows an answer had no room for; catching up from one member
// moves it past what a different member wrote while both of us were away.
//
// So every write to a message row (insert, edit, delete, pin, reaction) takes
// the next value of a per-database counter, and the row keeps it in `seq`. A
// responder answers "everything with seq after N" in seq order, which is
// exactly the set of rows it has learned or changed since the requester last
// heard from it, wherever those rows came from and whatever their stamps say;
// and any prefix of that answer is a safe place to resume from, so truncation
// loses nothing. The requester keeps the N it reached per peer and per channel
// (sync_marks). A peer's seq numbers mean nothing outside its own database, so
// each database names its numbering with a random space ID, and a mark is only
// honoured against the space it was issued in.
//
// sync_floor is the fallback for a peer we hold no mark for — a member we have
// never synced with, or one on a release from before marks — which can only be
// asked the old question. It is a send time, and unlike the old cursor it moves
// only on what a sync answer actually served: never on live delivery, never on
// our own sends, never on an update stamp.

// migrateSyncMarks adds the change counter and the mark tables.
func (s *Store) migrateSyncMarks() error {
	_, err := s.db.Exec(`ALTER TABLE messages ADD COLUMN seq INTEGER NOT NULL DEFAULT 0`)
	added := err == nil
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("store: migrate sync marks: %w", err)
	}
	var floorExists int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'sync_floor'`).Scan(&floorExists); err != nil {
		return fmt.Errorf("store: migrate sync marks: %w", err)
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sync_counter (
		   id INTEGER PRIMARY KEY CHECK (id = 0),
		   n  INTEGER NOT NULL
		 )`,
		`CREATE TABLE IF NOT EXISTS sync_marks (
		   peer       TEXT NOT NULL,
		   channel_id TEXT NOT NULL,
		   space      TEXT NOT NULL,
		   seq        INTEGER NOT NULL,
		   local      INTEGER NOT NULL DEFAULT 0,
		   PRIMARY KEY (peer, channel_id)
		 )`,
		`CREATE INDEX IF NOT EXISTS idx_sync_marks_channel ON sync_marks(channel_id)`,
		`CREATE TABLE IF NOT EXISTS sync_floor (
		   channel_id TEXT PRIMARY KEY,
		   sent       INTEGER NOT NULL
		 )`,
	}
	if added {
		// Rows that predate the counter are numbered in the order they were
		// written, so a responder can page through them like any others.
		stmts = append(stmts, `UPDATE messages SET seq = rowid`)
	}
	if floorExists == 0 {
		// An upgrade starts the fallback where the old cursor stood, less the
		// update stamps: the newest send time held. A fresh database has no
		// rows, so every channel starts at zero and asks for everything.
		stmts = append(stmts,
			`INSERT INTO sync_floor (channel_id, sent) SELECT channel_id, MAX(sent) FROM messages GROUP BY channel_id`)
	}
	stmts = append(stmts,
		`INSERT OR IGNORE INTO sync_counter (id, n) SELECT 0, COALESCE(MAX(seq), 0) FROM messages`,
		`CREATE INDEX IF NOT EXISTS idx_messages_channel_seq ON messages(channel_id, seq)`,
		// Every change a sync answer would carry takes the next number. The
		// counter only ever rises — a MAX(seq) would hand a deleted row's number
		// to the next insert, and a peer already past it would never see it.
		// Writing seq does not re-fire the update trigger: it is not one of the
		// columns the trigger watches.
		`CREATE TRIGGER IF NOT EXISTS messages_seq_insert AFTER INSERT ON messages BEGIN
		   UPDATE sync_counter SET n = n + 1 WHERE id = 0;
		   UPDATE messages SET seq = (SELECT n FROM sync_counter WHERE id = 0) WHERE rowid = NEW.rowid;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS messages_seq_update AFTER UPDATE OF deleted, edited, pinned, updated ON messages BEGIN
		   UPDATE sync_counter SET n = n + 1 WHERE id = 0;
		   UPDATE messages SET seq = (SELECT n FROM sync_counter WHERE id = 0) WHERE rowid = NEW.rowid;
		 END`,
		// Reactions live in their own table and travel as the message's state,
		// including the ones a sync applies, which never touch `updated`.
		`CREATE TRIGGER IF NOT EXISTS reactions_seq_insert AFTER INSERT ON reactions BEGIN
		   UPDATE sync_counter SET n = n + 1 WHERE id = 0;
		   UPDATE messages SET seq = (SELECT n FROM sync_counter WHERE id = 0) WHERE id = NEW.message_id;
		 END`,
		`CREATE TRIGGER IF NOT EXISTS reactions_seq_delete AFTER DELETE ON reactions BEGIN
		   UPDATE sync_counter SET n = n + 1 WHERE id = 0;
		   UPDATE messages SET seq = (SELECT n FROM sync_counter WHERE id = 0) WHERE id = OLD.message_id;
		 END`,
	)
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("store: migrate sync marks: %w", err)
		}
	}
	return nil
}

const syncSpaceKey = "sync_space"

// SyncSpace names this database's change numbering. Generated once; a peer's
// marks against us are honoured only while it matches, so a database rebuilt
// under the same identity (its counter back at zero) is not mistaken for the
// old one.
func (s *Store) SyncSpace() (string, error) {
	if v, err := s.GetSetting(syncSpaceKey); err != nil || v != "" {
		return v, err
	}
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	v := hex.EncodeToString(b[:])
	if _, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`, syncSpaceKey, v); err != nil {
		return "", fmt.Errorf("store: sync space: %w", err)
	}
	return s.GetSetting(syncSpaceKey)
}

// SyncCounter is the newest change number this database has handed out.
func (s *Store) SyncCounter() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT n FROM sync_counter WHERE id = 0`).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// ChannelSeqHead is the newest change number on any row of a channel.
func (s *Store) ChannelSeqHead(channelID string) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(seq) FROM messages WHERE channel_id = ?`, channelID).Scan(&n)
	return n.Int64, err
}

// SeqRows is one page of a channel's changes, in change order.
type SeqRows struct {
	Msgs []domain.Message
	Seqs []int64 // Seqs[i] is Msgs[i]'s change number
	// LastRead is the change number of the last row the page read, including
	// one it had to skip (a body that no longer opens), so a reader that took
	// the whole page can resume after it instead of meeting it again.
	LastRead int64
	// Full says the page hit its limit, so there may be more.
	Full bool
}

// MessagesAfterSeq is the serving side of a marked sync: up to limit rows of
// the synced kinds whose change number is after afterSeq, in change order.
// sinceNano, when non-zero, keeps only rows sent or updated after it — the time
// filter a requester with no mark for us still relies on.
func (s *Store) MessagesAfterSeq(channelID string, afterSeq, sinceNano int64, limit int) (SeqRows, error) {
	var out SeqRows
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(
		`SELECT id, channel_id, sender, name, kind, reply_to, dir, deleted, edited, pinned, content_enc, nonce, sent, updated, sig, seq
		 FROM messages WHERE channel_id = ? AND seq > ? AND (? = 0 OR sent > ? OR updated > ?) AND kind IN `+syncedKindsSQL+`
		 ORDER BY seq ASC LIMIT ?`, channelID, afterSeq, sinceNano, sinceNano, sinceNano, limit)
	if err != nil {
		return out, err
	}
	read := 0
	for rows.Next() {
		read++
		var m domain.Message
		var enc, nonceB []byte
		var sent, updated, seq int64
		var deleted, edited, pinned int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &m.Dir, &deleted, &edited, &pinned, &enc, &nonceB, &sent, &updated, &m.Sig, &seq); err != nil {
			rows.Close()
			return SeqRows{}, err
		}
		out.LastRead = seq
		if deleted != 0 {
			m.Deleted = true
		} else {
			content, err := s.open(enc, nonceB)
			if err != nil {
				continue // skip undecryptable rows rather than abort the sync
			}
			m.Content = content
		}
		m.Edited = edited != 0
		m.Pinned = pinned != 0
		m.Sent = time.Unix(0, sent).UTC()
		if updated != 0 {
			m.Updated = time.Unix(0, updated).UTC()
		}
		out.Msgs = append(out.Msgs, m)
		out.Seqs = append(out.Seqs, seq)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return SeqRows{}, err
	}
	rows.Close()
	if err := s.attachReactions(out.Msgs); err != nil {
		return SeqRows{}, err
	}
	out.Full = read >= limit
	return out, nil
}

// attachReactions fills in the reaction sets of the given rows.
func (s *Store) attachReactions(msgs []domain.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	idx := make(map[string]int, len(msgs))
	args := make([]any, 0, len(msgs))
	for i, m := range msgs {
		idx[m.ID] = i
		args = append(args, m.ID)
	}
	rows, err := s.db.Query(
		`SELECT message_id, emoji, fingerprint FROM reactions WHERE message_id IN (`+
			strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, emoji, fpr string
		if err := rows.Scan(&mid, &emoji, &fpr); err != nil {
			return err
		}
		m := &msgs[idx[mid]]
		if m.Reactions == nil {
			m.Reactions = map[string][]string{}
		}
		m.Reactions[emoji] = append(m.Reactions[emoji], fpr)
	}
	return rows.Err()
}

// RowDigest condenses the state of a row as sync serves it: which row, and
// everything about it a sync could change. Two copies with the same digest
// hold the same thing, so a responder may skip a row whose digest the requester
// says it holds (HeldDigests). Sixty-four bits: a collision would withhold one
// row from one peer, and at a few thousand digests per request the odds are
// below one in a quadrillion.
func RowDigest(m domain.Message) string {
	var b strings.Builder
	b.WriteString(m.ID)
	b.WriteByte(0)
	for _, f := range []bool{m.Deleted, m.Edited, m.Pinned} {
		if f {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	b.WriteByte(0)
	if !m.Updated.IsZero() {
		b.WriteString(strconv.FormatInt(m.Updated.UnixNano(), 10))
	}
	emojis := make([]string, 0, len(m.Reactions))
	for e := range m.Reactions {
		emojis = append(emojis, e)
	}
	sort.Strings(emojis)
	for _, e := range emojis {
		fprs := append([]string(nil), m.Reactions[e]...)
		sort.Strings(fprs)
		b.WriteByte(0)
		b.WriteString(e)
		for _, f := range fprs {
			b.WriteByte(1)
			b.WriteString(f)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

// HeldDigests lists the digests of a channel's rows that changed here after
// our change number afterLocal, newest first, at most max of them. It is what a
// requester tells a peer it already holds: the rows that reached us since we
// last synced with that peer are mostly the same rows that reached the peer,
// and there is no reason to have them sent back.
func (s *Store) HeldDigests(channelID string, afterLocal int64, max int) ([]string, error) {
	if max <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT id, deleted, edited, pinned, updated FROM messages
		  WHERE channel_id = ? AND seq > ? AND kind IN `+syncedKindsSQL+`
		  ORDER BY seq DESC LIMIT ?`, channelID, afterLocal, max)
	if err != nil {
		return nil, err
	}
	var msgs []domain.Message
	for rows.Next() {
		var m domain.Message
		var deleted, edited, pinned int
		var updated int64
		if err := rows.Scan(&m.ID, &deleted, &edited, &pinned, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		m.Deleted, m.Edited, m.Pinned = deleted != 0, edited != 0, pinned != 0
		if updated != 0 {
			m.Updated = time.Unix(0, updated).UTC()
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err := s.attachReactions(msgs); err != nil {
		return nil, err
	}
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = RowDigest(m)
	}
	return out, nil
}

// SyncMarks returns what we hold from one peer for the given channels: the
// space its numbers belong to, the number reached per channel, and our own
// change number when that peer last answered (for HeldDigests). Channels with
// no mark are absent.
func (s *Store) SyncMarks(peer string, channelIDs []string) (space string, seq, local map[string]int64, err error) {
	seq, local = map[string]int64{}, map[string]int64{}
	if len(channelIDs) == 0 {
		return "", seq, local, nil
	}
	args := []any{peer}
	for _, id := range channelIDs {
		args = append(args, id)
	}
	rows, err := s.db.Query(
		`SELECT channel_id, space, seq, local FROM sync_marks WHERE peer = ? AND channel_id IN (`+
			strings.TrimSuffix(strings.Repeat("?,", len(channelIDs)), ",")+`)`, args...)
	if err != nil {
		return "", nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ch, sp string
		var n, l int64
		if err := rows.Scan(&ch, &sp, &n, &l); err != nil {
			return "", nil, nil, err
		}
		if space == "" {
			space = sp
		}
		if sp != space {
			continue // marks from an older numbering of this peer; superseded below
		}
		seq[ch], local[ch] = n, l
	}
	return space, seq, local, rows.Err()
}

// SetSyncMark records that a peer's answer for a channel has been taken in
// through its change number n. Within one space a mark only rises; a new space
// replaces it.
func (s *Store) SetSyncMark(peer, channelID, space string, n, local int64) error {
	_, err := s.db.Exec(
		`INSERT INTO sync_marks (peer, channel_id, space, seq, local) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(peer, channel_id) DO UPDATE SET
		   seq   = CASE WHEN space = excluded.space THEN MAX(seq, excluded.seq) ELSE excluded.seq END,
		   local = CASE WHEN space = excluded.space THEN MAX(local, excluded.local) ELSE excluded.local END,
		   space = excluded.space`,
		peer, channelID, space, n, local)
	if err != nil {
		return fmt.Errorf("store: set sync mark: %w", err)
	}
	return nil
}

// SyncFloor is a channel's fallback cursor (UnixNano send time), or 0.
func (s *Store) SyncFloor(channelID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT sent FROM sync_floor WHERE channel_id = ?`, channelID).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// RaiseSyncFloor moves a channel's fallback cursor up to sent, never down.
func (s *Store) RaiseSyncFloor(channelID string, sent int64) error {
	_, err := s.db.Exec(
		`INSERT INTO sync_floor (channel_id, sent) VALUES (?, ?)
		 ON CONFLICT(channel_id) DO UPDATE SET sent = MAX(sent, excluded.sent)`, channelID, sent)
	if err != nil {
		return fmt.Errorf("store: raise sync floor: %w", err)
	}
	return nil
}

// forgetSyncMarksTx drops a channel's marks and floor along with its rows: a
// mark claims we hold what a peer served, and after a delete we do not.
func forgetSyncMarksTx(tx *sql.Tx, channelID string) error {
	if _, err := tx.Exec(`DELETE FROM sync_marks WHERE channel_id = ?`, channelID); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM sync_floor WHERE channel_id = ?`, channelID)
	return err
}
