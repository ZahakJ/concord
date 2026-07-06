// Package store is layer 5 of Concord: local-first, encrypted persistence.
//
// Concord is peer-to-peer with no central database, so each peer keeps its own
// copy of the guilds it belongs to and the message history it has seen. Message
// bodies are the sensitive part, so they are sealed at rest with NaCl secretbox
// under a data key derived from the (passphrase-protected) identity — a stolen
// database file yields no readable messages. Structural metadata (guild names,
// timestamps) is stored in the clear so the app can query and order it.
//
// The backend is modernc.org/sqlite, a pure-Go SQLite with no CGO, keeping the
// whole project buildable without a C toolchain.
package store

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/zahak/concord/internal/domain"

	_ "modernc.org/sqlite"
)

const nonceSize = 24

// Store is a peer's local encrypted database.
type Store struct {
	db  *sql.DB
	key [32]byte // secretbox key for message bodies
}

// Open opens (creating if needed) the SQLite database at path and prepares the
// schema. dataKey must be 32 bytes; it seals message bodies at rest.
func Open(path string, dataKey []byte) (*Store, error) {
	if len(dataKey) != 32 {
		return nil, fmt.Errorf("store: dataKey must be 32 bytes, got %d", len(dataKey))
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}
	// SQLite handles one writer at a time; keep a single connection to avoid
	// "database is locked" churn, and enable WAL for concurrent readers.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: set pragmas: %w", err)
	}

	s := &Store{db: db}
	copy(s.key[:], dataKey)
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS guilds (
  id       TEXT PRIMARY KEY,
  name     TEXT NOT NULL,
  group_id BLOB NOT NULL,
  owner_id BLOB NOT NULL,
  created  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS channels (
  id       TEXT PRIMARY KEY,
  guild_id TEXT NOT NULL,
  name     TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
  id          TEXT PRIMARY KEY,
  channel_id  TEXT NOT NULL,
  sender      BLOB NOT NULL,
  name        TEXT NOT NULL DEFAULT '',
  kind        TEXT NOT NULL DEFAULT '',
  reply_to    TEXT NOT NULL DEFAULT '',
  deleted     INTEGER NOT NULL DEFAULT 0,
  edited      INTEGER NOT NULL DEFAULT 0,
  content_enc BLOB NOT NULL,
  nonce       BLOB NOT NULL,
  sent        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(channel_id, sent);
CREATE TABLE IF NOT EXISTS contacts (
  peer_id     TEXT PRIMARY KEY,
  fingerprint TEXT NOT NULL,
  verified    INTEGER NOT NULL DEFAULT 0,
  first_seen  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS reactions (
  message_id  TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  emoji       TEXT NOT NULL,
  PRIMARY KEY (message_id, fingerprint, emoji)
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	// Tolerant column add for databases created before the name column existed.
	// SQLite errors "duplicate column name" if it's already present; ignore that.
	for _, col := range []string{
		`ALTER TABLE messages ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN reply_to TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN edited INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := s.db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	return nil
}

// RecordContact registers a peer on first sight (trust-on-first-use). Repeated
// sightings preserve the original first_seen and verified flag.
func (s *Store) RecordContact(peerID, fingerprint string) error {
	_, err := s.db.Exec(
		`INSERT INTO contacts (peer_id, fingerprint, verified, first_seen)
		 VALUES (?, ?, 0, ?)
		 ON CONFLICT(peer_id) DO NOTHING`,
		peerID, fingerprint, time.Now().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: record contact: %w", err)
	}
	return nil
}

// SetVerified marks a contact as human-verified.
func (s *Store) SetVerified(peerID string) error {
	res, err := s.db.Exec(`UPDATE contacts SET verified = 1 WHERE peer_id = ?`, peerID)
	if err != nil {
		return fmt.Errorf("store: set verified: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: unknown contact %s", peerID)
	}
	return nil
}

// Contacts returns all known contacts, first-seen order.
func (s *Store) Contacts() ([]domain.Contact, error) {
	rows, err := s.db.Query(`SELECT peer_id, fingerprint, verified, first_seen FROM contacts ORDER BY first_seen`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Contact
	for rows.Next() {
		var c domain.Contact
		var verified int
		var seen int64
		if err := rows.Scan(&c.PeerID, &c.Fingerprint, &verified, &seen); err != nil {
			return nil, err
		}
		c.Verified = verified != 0
		c.FirstSeen = time.Unix(0, seen).UTC()
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveGuild upserts a guild and its channels.
func (s *Store) SaveGuild(g domain.Guild) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	_, err = tx.Exec(
		`INSERT INTO guilds (id, name, group_id, owner_id, created)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name`,
		g.ID, g.Name, g.GroupID, g.OwnerID, g.Created.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: save guild: %w", err)
	}
	for _, c := range g.Channels {
		if _, err := tx.Exec(
			`INSERT INTO channels (id, guild_id, name) VALUES (?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name`,
			c.ID, g.ID, c.Name,
		); err != nil {
			return fmt.Errorf("store: save channel: %w", err)
		}
	}
	return tx.Commit()
}

// Guilds loads all guilds with their channels.
func (s *Store) Guilds() ([]domain.Guild, error) {
	rows, err := s.db.Query(`SELECT id, name, group_id, owner_id, created FROM guilds ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var guilds []domain.Guild
	for rows.Next() {
		var g domain.Guild
		var created int64
		if err := rows.Scan(&g.ID, &g.Name, &g.GroupID, &g.OwnerID, &created); err != nil {
			return nil, err
		}
		g.Created = time.Unix(0, created).UTC()
		guilds = append(guilds, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range guilds {
		chans, err := s.channelsFor(guilds[i].ID)
		if err != nil {
			return nil, err
		}
		guilds[i].Channels = chans
	}
	return guilds, nil
}

func (s *Store) channelsFor(guildID string) ([]domain.Channel, error) {
	rows, err := s.db.Query(`SELECT id, guild_id, name FROM channels WHERE guild_id = ?`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Channel
	for rows.Next() {
		var c domain.Channel
		if err := rows.Scan(&c.ID, &c.GuildID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveMessage stores a message, sealing its content at rest. Saving the same
// message ID twice is a no-op, which makes gossip re-delivery idempotent.
func (s *Store) SaveMessage(m domain.Message) error {
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	sealed := secretbox.Seal(nil, []byte(m.Content), &nonce, &s.key)

	_, err := s.db.Exec(
		`INSERT INTO messages (id, channel_id, sender, name, kind, reply_to, content_enc, nonce, sent)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		m.ID, m.ChannelID, m.Sender, m.Name, m.Kind, m.ReplyTo, sealed, nonce[:], m.Sent.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: save message: %w", err)
	}
	return nil
}

// Messages returns up to limit most-recent messages for a channel, oldest
// first, decrypting bodies. A limit <= 0 returns all messages.
func (s *Store) Messages(channelID string, limit int) ([]domain.Message, error) {
	q := `SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, content_enc, nonce, sent
	      FROM messages WHERE channel_id = ? ORDER BY sent DESC`
	args := []any{channelID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []domain.Message
	for rows.Next() {
		var m domain.Message
		var enc, nonceB []byte
		var sent int64
		var deleted, edited int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &enc, &nonceB, &sent); err != nil {
			return nil, err
		}
		m.Edited = edited != 0
		if deleted != 0 {
			m.Deleted = true // leave content blank
		} else {
			content, err := s.open(enc, nonceB)
			if err != nil {
				return nil, err
			}
			m.Content = content
		}
		m.Sent = time.Unix(0, sent).UTC()
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse into chronological order (we queried DESC to honour LIMIT).
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	reacts, err := s.reactionsForChannel(channelID)
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		msgs[i].Reactions = reacts[msgs[i].ID]
	}
	return msgs, nil
}

func (s *Store) open(enc, nonceB []byte) (string, error) {
	if len(nonceB) != nonceSize {
		return "", fmt.Errorf("store: bad nonce length %d", len(nonceB))
	}
	var nonce [nonceSize]byte
	copy(nonce[:], nonceB)
	plain, ok := secretbox.Open(nil, enc, &nonce, &s.key)
	if !ok {
		return "", fmt.Errorf("store: message decryption failed (wrong key or corrupt db)")
	}
	return string(plain), nil
}

// ToggleReaction adds a reaction if absent, or removes it if present (so a
// second tap of the same emoji un-reacts). Returns whether it is now added.
func (s *Store) ToggleReaction(messageID, fingerprint, emoji string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM reactions WHERE message_id=? AND fingerprint=? AND emoji=?`,
		messageID, fingerprint, emoji,
	).Scan(&one)
	if err == nil {
		_, derr := s.db.Exec(
			`DELETE FROM reactions WHERE message_id=? AND fingerprint=? AND emoji=?`,
			messageID, fingerprint, emoji)
		return false, derr
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	_, ierr := s.db.Exec(
		`INSERT INTO reactions (message_id, fingerprint, emoji) VALUES (?, ?, ?)`,
		messageID, fingerprint, emoji)
	return true, ierr
}

// reactionsForChannel loads all reactions for a channel's messages, grouped as
// messageID -> emoji -> [fingerprints].
func (s *Store) reactionsForChannel(channelID string) (map[string]map[string][]string, error) {
	rows, err := s.db.Query(
		`SELECT r.message_id, r.emoji, r.fingerprint
		 FROM reactions r JOIN messages m ON m.id = r.message_id
		 WHERE m.channel_id = ?`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string][]string{}
	for rows.Next() {
		var mid, emoji, fpr string
		if err := rows.Scan(&mid, &emoji, &fpr); err != nil {
			return nil, err
		}
		if out[mid] == nil {
			out[mid] = map[string][]string{}
		}
		out[mid][emoji] = append(out[mid][emoji], fpr)
	}
	return out, rows.Err()
}

// MessageByID loads a single message (with reactions) for post-reaction refresh.
func (s *Store) MessageByID(id string) (domain.Message, bool, error) {
	var m domain.Message
	var enc, nonceB []byte
	var sent int64
	var deleted int
	var edited int
	err := s.db.QueryRow(
		`SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, content_enc, nonce, sent
		 FROM messages WHERE id = ?`, id,
	).Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &enc, &nonceB, &sent)
	if err == sql.ErrNoRows {
		return domain.Message{}, false, nil
	}
	if err != nil {
		return domain.Message{}, false, err
	}
	m.Edited = edited != 0
	if deleted != 0 {
		m.Deleted = true
	} else if content, oerr := s.open(enc, nonceB); oerr == nil {
		m.Content = content
	}
	m.Sent = time.Unix(0, sent).UTC()
	reacts, err := s.reactionsForChannel(m.ChannelID)
	if err != nil {
		return domain.Message{}, false, err
	}
	m.Reactions = reacts[m.ID]
	return m, true, nil
}

// UpdateContent replaces a message's (encrypted) content, but only if bySender
// authored it. Marks the message edited. Returns whether a row changed.
func (s *Store) UpdateContent(id string, bySender []byte, newContent string) (bool, error) {
	var sender []byte
	err := s.db.QueryRow(`SELECT sender FROM messages WHERE id = ?`, id).Scan(&sender)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(bySender) == 0 || string(sender) != string(bySender) {
		return false, nil
	}
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false, err
	}
	sealed := secretbox.Seal(nil, []byte(newContent), &nonce, &s.key)
	if _, err := s.db.Exec(
		`UPDATE messages SET content_enc = ?, nonce = ?, edited = 1 WHERE id = ?`,
		sealed, nonce[:], id,
	); err != nil {
		return false, err
	}
	return true, nil
}

// MarkDeleted tombstones a message, but only if bySender authored it (so a peer
// can't delete someone else's message). Returns the deleted message and whether
// a row changed.
func (s *Store) MarkDeleted(id string, bySender []byte) (domain.Message, bool, error) {
	var m domain.Message
	var chID string
	var sent int64
	err := s.db.QueryRow(
		`SELECT channel_id, sender, sent FROM messages WHERE id = ?`, id,
	).Scan(&chID, &m.Sender, &sent)
	if err == sql.ErrNoRows {
		return domain.Message{}, false, nil
	}
	if err != nil {
		return domain.Message{}, false, err
	}
	if len(bySender) == 0 || string(m.Sender) != string(bySender) {
		return domain.Message{}, false, nil // not the author
	}
	if _, err := s.db.Exec(`UPDATE messages SET deleted = 1 WHERE id = ?`, id); err != nil {
		return domain.Message{}, false, err
	}
	m.ID = id
	m.ChannelID = chID
	m.Deleted = true
	m.Sent = time.Unix(0, sent).UTC()
	return m, true, nil
}

// SetSetting stores a key/value app setting (e.g. the display name).
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("store: set setting: %w", err)
	}
	return nil
}

// GetSetting returns a stored setting, or "" if unset.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get setting: %w", err)
	}
	return v, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
