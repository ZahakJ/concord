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
  name     TEXT NOT NULL,
  type     TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  topic    TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS categories (
  id       TEXT PRIMARY KEY,
  guild_id TEXT NOT NULL,
  name     TEXT NOT NULL,
  position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS custom_emoji (
  guild_id TEXT NOT NULL,
  name     TEXT NOT NULL,
  image    TEXT NOT NULL,
  created  INTEGER NOT NULL,
  PRIMARY KEY (guild_id, name)
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
  pinned      INTEGER NOT NULL DEFAULT 0,
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
CREATE TABLE IF NOT EXISTS profiles (
  fingerprint TEXT PRIMARY KEY,
  name        TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT '',
  emoji       TEXT NOT NULL DEFAULT '',
  color       TEXT NOT NULL DEFAULT '',
  avatar      TEXT NOT NULL DEFAULT '',
  updated     INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS nicknames (
  guild_id    TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  nick        TEXT NOT NULL,
  updated     INTEGER NOT NULL,
  PRIMARY KEY (guild_id, fingerprint)
);
CREATE TABLE IF NOT EXISTS guild_ops (
  guild_id TEXT NOT NULL,
  op_hash  TEXT NOT NULL,
  op_json  BLOB NOT NULL,
  created  INTEGER NOT NULL,
  PRIMARY KEY (guild_id, op_hash)
);
CREATE TABLE IF NOT EXISTS mls_commits (
  group_id BLOB NOT NULL,
  epoch    INTEGER NOT NULL,
  commit_b BLOB NOT NULL,
  created  INTEGER NOT NULL,
  PRIMARY KEY (group_id, epoch)
);
CREATE TABLE IF NOT EXISTS attachments (
  blob_id   TEXT PRIMARY KEY,
  ct        BLOB NOT NULL,
  size      INTEGER NOT NULL,
  created   INTEGER NOT NULL,
  last_used INTEGER NOT NULL
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
		`ALTER TABLE messages ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN updated INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE channels ADD COLUMN type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE channels ADD COLUMN category TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE channels ADD COLUMN position INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE channels ADD COLUMN topic TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE guilds ADD COLUMN kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE guilds ADD COLUMN icon TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE guilds ADD COLUMN banner TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE guilds ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE profiles ADD COLUMN mailbox_pub BLOB`,
		`ALTER TABLE profiles ADD COLUMN presence TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE profiles ADD COLUMN bio TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE profiles ADD COLUMN banner TEXT NOT NULL DEFAULT ''`,
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

// SetVerifiedByFingerprint marks every contact carrying this fingerprint as
// human-verified (a peer may appear under multiple transient peer IDs, but the
// fingerprint is the stable identity).
func (s *Store) SetVerifiedByFingerprint(fingerprint string) error {
	res, err := s.db.Exec(`UPDATE contacts SET verified = 1 WHERE fingerprint = ?`, fingerprint)
	if err != nil {
		return fmt.Errorf("store: verify by fingerprint: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: unknown fingerprint")
	}
	return nil
}

// ImportVerifiedFingerprint marks a fingerprint verified before any peer
// carrying it has been sighted — device linking transfers the account's
// verifications, and the new device usually hasn't met those peers yet. The
// placeholder row is keyed by the fingerprint itself ("fpr:…" can't collide
// with a real libp2p peer ID); a later real sighting just adds another row,
// and every verified-lookup already aggregates by fingerprint.
func (s *Store) ImportVerifiedFingerprint(fingerprint string) error {
	_, err := s.db.Exec(
		`INSERT INTO contacts (peer_id, fingerprint, verified, first_seen)
		 VALUES (?, ?, 1, ?)
		 ON CONFLICT(peer_id) DO UPDATE SET verified = 1`,
		"fpr:"+fingerprint, fingerprint, time.Now().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: import verified: %w", err)
	}
	return nil
}

// VerifiedFingerprints returns the set of fingerprints the user has verified.
func (s *Store) VerifiedFingerprints() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT DISTINCT fingerprint FROM contacts WHERE verified = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out[f] = true
	}
	return out, rows.Err()
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
		`INSERT INTO guilds (id, name, group_id, owner_id, created, kind, icon, banner, description)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, icon=excluded.icon,
		   banner=excluded.banner, description=excluded.description`,
		g.ID, g.Name, g.GroupID, g.OwnerID, g.Created.UnixNano(), g.Kind, g.Icon, g.Banner, g.Description,
	)
	if err != nil {
		return fmt.Errorf("store: save guild: %w", err)
	}
	for _, c := range g.Channels {
		if _, err := tx.Exec(
			`INSERT INTO channels (id, guild_id, name, type, category, position, topic) VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, type=excluded.type,
			   category=excluded.category, position=excluded.position, topic=excluded.topic`,
			c.ID, g.ID, c.Name, c.Type, c.Category, c.Position, c.Topic,
		); err != nil {
			return fmt.Errorf("store: save channel: %w", err)
		}
	}
	return tx.Commit()
}

// Guilds loads all guilds with their channels.
func (s *Store) Guilds() ([]domain.Guild, error) {
	rows, err := s.db.Query(`SELECT id, name, group_id, owner_id, created, kind, icon, banner, description FROM guilds ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var guilds []domain.Guild
	for rows.Next() {
		var g domain.Guild
		var created int64
		if err := rows.Scan(&g.ID, &g.Name, &g.GroupID, &g.OwnerID, &created, &g.Kind, &g.Icon, &g.Banner, &g.Description); err != nil {
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
	rows, err := s.db.Query(
		`SELECT id, guild_id, name, type, category, position, topic FROM channels
		 WHERE guild_id = ? ORDER BY position ASC, rowid ASC`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Channel
	for rows.Next() {
		var c domain.Channel
		if err := rows.Scan(&c.ID, &c.GuildID, &c.Name, &c.Type, &c.Category, &c.Position, &c.Topic); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveCategory upserts a guild category (layout metadata).
func (s *Store) SaveCategory(c domain.Category) error {
	_, err := s.db.Exec(
		`INSERT INTO categories (id, guild_id, name, position) VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, position=excluded.position`,
		c.ID, c.GuildID, c.Name, c.Position)
	if err != nil {
		return fmt.Errorf("store: save category: %w", err)
	}
	return nil
}

// Categories returns a guild's categories, ordered by position.
func (s *Store) Categories(guildID string) ([]domain.Category, error) {
	rows, err := s.db.Query(
		`SELECT id, guild_id, name, position FROM categories WHERE guild_id = ? ORDER BY position ASC, rowid ASC`,
		guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Category
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.GuildID, &c.Name, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateChannelMeta sets a channel's type/category/position/topic (layout +
// advisory metadata).
func (s *Store) UpdateChannelMeta(channelID, ctype, category string, position int, topic string) error {
	_, err := s.db.Exec(
		`UPDATE channels SET type=?, category=?, position=?, topic=? WHERE id=?`,
		ctype, category, position, topic, channelID)
	return err
}

// DeleteChannel removes a channel and its messages/reactions.
func (s *Store) DeleteChannel(channelID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM reactions WHERE message_id IN (SELECT id FROM messages WHERE channel_id=?)`, channelID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE channel_id=?`, channelID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM channels WHERE id=?`, channelID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteCategory removes a category and un-categorizes its channels (they stay).
func (s *Store) DeleteCategory(guildID, categoryID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`UPDATE channels SET category='' WHERE guild_id=? AND category=?`, guildID, categoryID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM categories WHERE id=?`, categoryID); err != nil {
		return err
	}
	return tx.Commit()
}

// CustomEmojiRow is one guild custom emoji.
type CustomEmojiRow struct {
	GuildID, Name, Image string
}

// SaveCustomEmoji upserts a guild custom emoji.
func (s *Store) SaveCustomEmoji(e CustomEmojiRow) error {
	_, err := s.db.Exec(
		`INSERT INTO custom_emoji (guild_id, name, image, created) VALUES (?, ?, ?, ?)
		 ON CONFLICT(guild_id, name) DO UPDATE SET image=excluded.image`,
		e.GuildID, e.Name, e.Image, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("store: save custom emoji: %w", err)
	}
	return nil
}

// DeleteCustomEmoji removes a guild custom emoji.
func (s *Store) DeleteCustomEmoji(guildID, name string) error {
	_, err := s.db.Exec(`DELETE FROM custom_emoji WHERE guild_id=? AND name=?`, guildID, name)
	return err
}

// CustomEmoji returns a guild's custom emoji, ordered by name.
func (s *Store) CustomEmoji(guildID string) ([]CustomEmojiRow, error) {
	rows, err := s.db.Query(`SELECT guild_id, name, image FROM custom_emoji WHERE guild_id=? ORDER BY name`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomEmojiRow
	for rows.Next() {
		var e CustomEmojiRow
		if err := rows.Scan(&e.GuildID, &e.Name, &e.Image); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveNickname upserts a per-guild nickname for a member (an empty nick clears
// it, reverting to the member's global profile name).
func (s *Store) SaveNickname(guildID, fingerprint, nick string) error {
	if nick == "" {
		_, err := s.db.Exec(`DELETE FROM nicknames WHERE guild_id=? AND fingerprint=?`, guildID, fingerprint)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO nicknames (guild_id, fingerprint, nick, updated) VALUES (?, ?, ?, ?)
		 ON CONFLICT(guild_id, fingerprint) DO UPDATE SET nick=excluded.nick, updated=excluded.updated`,
		guildID, fingerprint, nick, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("store: save nickname: %w", err)
	}
	return nil
}

// SaveGuildOp stores one governance op (keyed by its content hash, so a
// re-received op is idempotent). op_json is the caller's canonical encoding.
func (s *Store) SaveGuildOp(guildID, opHash string, opJSON []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO guild_ops (guild_id, op_hash, op_json, created) VALUES (?, ?, ?, ?)
		 ON CONFLICT(guild_id, op_hash) DO NOTHING`,
		guildID, opHash, opJSON, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("store: save guild op: %w", err)
	}
	return nil
}

// GuildOps returns the raw op encodings for a guild (unordered; the caller
// replays them in canonical order).
func (s *Store) GuildOps(guildID string) ([][]byte, error) {
	rows, err := s.db.Query(`SELECT op_json FROM guild_ops WHERE guild_id=?`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AllGuildOps returns every stored op across all guilds as guildID → [opJSON],
// for warming the in-memory governance state at startup.
func (s *Store) AllGuildOps() (map[string][][]byte, error) {
	rows, err := s.db.Query(`SELECT guild_id, op_json FROM guild_ops`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][][]byte{}
	for rows.Next() {
		var g string
		var b []byte
		if err := rows.Scan(&g, &b); err != nil {
			return nil, err
		}
		out[g] = append(out[g], b)
	}
	return out, rows.Err()
}

// Nicknames returns every stored per-guild nickname as guildID → fingerprint →
// nick, for warming the in-memory cache at startup.
func (s *Store) Nicknames() (map[string]map[string]string, error) {
	rows, err := s.db.Query(`SELECT guild_id, fingerprint, nick FROM nicknames`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		var g, fpr, nick string
		if err := rows.Scan(&g, &fpr, &nick); err != nil {
			return nil, err
		}
		if out[g] == nil {
			out[g] = map[string]string{}
		}
		out[g][fpr] = nick
	}
	return out, rows.Err()
}

// DeleteGuild removes a guild and all of its local data (channels, messages,
// reactions). Used when leaving/deleting a server.
func (s *Store) DeleteGuild(guildID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	rows, err := tx.Query(`SELECT id FROM channels WHERE guild_id = ?`, guildID)
	if err != nil {
		return err
	}
	var chIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		chIDs = append(chIDs, id)
	}
	rows.Close()

	for _, ch := range chIDs {
		if _, err := tx.Exec(`DELETE FROM reactions WHERE message_id IN (SELECT id FROM messages WHERE channel_id = ?)`, ch); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM messages WHERE channel_id = ?`, ch); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM channels WHERE guild_id = ?`, guildID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM nicknames WHERE guild_id = ?`, guildID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM guild_ops WHERE guild_id = ?`, guildID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM guilds WHERE id = ?`, guildID); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveMessage stores a message, sealing its content at rest. Saving the same
// message ID twice is a no-op, which makes gossip re-delivery and history sync
// idempotent. The bool reports whether a new row was inserted.
func (s *Store) SaveMessage(m domain.Message) (bool, error) {
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false, err
	}
	sealed := secretbox.Seal(nil, []byte(m.Content), &nonce, &s.key)

	res, err := s.db.Exec(
		`INSERT INTO messages (id, channel_id, sender, name, kind, reply_to, content_enc, nonce, sent)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		m.ID, m.ChannelID, m.Sender, m.Name, m.Kind, m.ReplyTo, sealed, nonce[:], m.Sent.UnixNano(),
	)
	if err != nil {
		return false, fmt.Errorf("store: save message: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// LatestTimestamp returns the newest change time (UnixNano) in a channel — the
// max over message send times and later state updates (edit/delete/pin/react) —
// or 0. It is the cursor for history sync.
func (s *Store) LatestTimestamp(channelID string) (int64, error) {
	var t sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(COALESCE(MAX(sent),0), COALESCE(MAX(updated),0))
		 FROM messages WHERE channel_id = ?`, channelID).Scan(&t)
	if err != nil {
		return 0, err
	}
	return t.Int64, nil
}

// MessagesSince returns up to limit messages in a channel strictly newer than
// sinceNano, oldest first, with decrypted bodies (deleted ones excluded).
func (s *Store) MessagesSince(channelID string, sinceNano int64, limit int) ([]domain.Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(
		`SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, content_enc, nonce, sent
		 FROM messages WHERE channel_id = ? AND sent > ? AND deleted = 0 AND kind = ''
		 ORDER BY sent ASC LIMIT ?`, channelID, sinceNano, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var enc, nonceB []byte
		var sent int64
		var deleted, edited, pinned int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &pinned, &enc, &nonceB, &sent); err != nil {
			return nil, err
		}
		content, err := s.open(enc, nonceB)
		if err != nil {
			continue
		}
		m.Content = content
		m.Edited = edited != 0
		m.Pinned = pinned != 0
		m.Sent = time.Unix(0, sent).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// Messages returns up to limit most-recent messages for a channel, oldest
// first, decrypting bodies. A limit <= 0 returns all messages.
func (s *Store) Messages(channelID string, limit int) ([]domain.Message, error) {
	q := `SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, content_enc, nonce, sent
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
		var deleted, edited, pinned int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &pinned, &enc, &nonceB, &sent); err != nil {
			return nil, err
		}
		m.Edited = edited != 0
		m.Pinned = pinned != 0
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
		s.touchMessage(messageID)
		return false, derr
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	_, ierr := s.db.Exec(
		`INSERT INTO reactions (message_id, fingerprint, emoji) VALUES (?, ?, ?)`,
		messageID, fingerprint, emoji)
	s.touchMessage(messageID)
	return true, ierr
}

// touchMessage bumps a message's updated time so history sync serves its new
// state (reactions live in their own table but ride along with the message).
func (s *Store) touchMessage(id string) {
	_, _ = s.db.Exec(`UPDATE messages SET updated = ? WHERE id = ?`, time.Now().UnixNano(), id)
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
	var edited, pinned int
	err := s.db.QueryRow(
		`SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, content_enc, nonce, sent
		 FROM messages WHERE id = ?`, id,
	).Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &pinned, &enc, &nonceB, &sent)
	if err == sql.ErrNoRows {
		return domain.Message{}, false, nil
	}
	if err != nil {
		return domain.Message{}, false, err
	}
	m.Edited = edited != 0
	m.Pinned = pinned != 0
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

// TogglePinned flips a message's pinned flag. Any guild member may pin (the
// action itself arrives over the authenticated encrypted channel).
func (s *Store) TogglePinned(id string) (bool, error) {
	var pinned int
	err := s.db.QueryRow(`SELECT pinned FROM messages WHERE id = ?`, id).Scan(&pinned)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	newVal := 1 - pinned
	if _, err := s.db.Exec(`UPDATE messages SET pinned = ?, updated = ? WHERE id = ?`, newVal, time.Now().UnixNano(), id); err != nil {
		return false, err
	}
	return newVal == 1, nil
}

// SearchMessages scans all stored messages for a case-insensitive substring
// match, newest first, up to limit. Search runs entirely locally over the
// user's own (at-rest-encrypted) history — no server ever sees the query.
func (s *Store) SearchMessages(query string, limit int) ([]domain.Message, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, content_enc, nonce, sent
		 FROM messages WHERE deleted = 0 AND kind = '' ORDER BY sent DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	needle := strings.ToLower(query)
	var out []domain.Message
	for rows.Next() && len(out) < limit {
		var m domain.Message
		var enc, nonceB []byte
		var sent int64
		var deleted, edited, pinned int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &pinned, &enc, &nonceB, &sent); err != nil {
			return nil, err
		}
		content, err := s.open(enc, nonceB)
		if err != nil {
			continue // skip undecryptable rows rather than abort the search
		}
		if !strings.Contains(strings.ToLower(content), needle) {
			continue
		}
		m.Content = content
		m.Edited = edited != 0
		m.Pinned = pinned != 0
		m.Sent = time.Unix(0, sent).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
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
		`UPDATE messages SET content_enc = ?, nonce = ?, edited = 1, updated = ? WHERE id = ?`,
		sealed, nonce[:], time.Now().UnixNano(), id,
	); err != nil {
		return false, err
	}
	return true, nil
}

// MarkDeleted tombstones a message. The author may always delete their own; a
// moderator (force=true, decided by the caller from ManageMessages) may delete
// anyone's. Returns the deleted message and whether a row changed.
func (s *Store) MarkDeleted(id string, bySender []byte, force bool) (domain.Message, bool, error) {
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
	if !force && (len(bySender) == 0 || string(m.Sender) != string(bySender)) {
		return domain.Message{}, false, nil // not the author and not a moderator
	}
	if _, err := s.db.Exec(`UPDATE messages SET deleted = 1, updated = ? WHERE id = ?`, time.Now().UnixNano(), id); err != nil {
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

// ProfileRow is a peer's learned profile as persisted (see app.Profile).
type ProfileRow struct {
	Fingerprint, Name, Status, Emoji, Color, Avatar string
	Banner                                          string
	Presence, Bio                                   string
	MailboxPub                                      []byte
}

// SaveProfile upserts a peer's learned profile so display names (and their
// mailbox key) survive restarts instead of living only in memory.
func (s *Store) SaveProfile(p ProfileRow) error {
	_, err := s.db.Exec(
		`INSERT INTO profiles (fingerprint, name, status, emoji, color, avatar, banner, presence, bio, mailbox_pub, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(fingerprint) DO UPDATE SET
		   name=excluded.name, status=excluded.status, emoji=excluded.emoji,
		   color=excluded.color, avatar=excluded.avatar, banner=excluded.banner,
		   presence=excluded.presence, bio=excluded.bio, mailbox_pub=excluded.mailbox_pub, updated=excluded.updated`,
		p.Fingerprint, p.Name, p.Status, p.Emoji, p.Color, p.Avatar, p.Banner, p.Presence, p.Bio, p.MailboxPub, time.Now().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: save profile: %w", err)
	}
	return nil
}

// Profiles returns every learned peer profile.
func (s *Store) Profiles() ([]ProfileRow, error) {
	rows, err := s.db.Query(`SELECT fingerprint, name, status, emoji, color, avatar, banner, presence, bio, mailbox_pub FROM profiles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProfileRow
	for rows.Next() {
		var p ProfileRow
		if err := rows.Scan(&p.Fingerprint, &p.Name, &p.Status, &p.Emoji, &p.Color, &p.Avatar, &p.Banner, &p.Presence, &p.Bio, &p.MailboxPub); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CommitRow is one MLS commit in a group's ordered commit log.
type CommitRow struct {
	Epoch  uint64
	Commit []byte
}

// SaveCommit records the MLS commit that produced epoch for a group. The log
// lets reconnecting members replay membership changes they missed (commits must
// apply gaplessly, so without this a missed commit strands them at an old
// epoch). Saving the same epoch twice is a no-op.
func (s *Store) SaveCommit(groupID []byte, epoch uint64, commit []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO mls_commits (group_id, epoch, commit_b, created) VALUES (?, ?, ?, ?)
		 ON CONFLICT(group_id, epoch) DO NOTHING`,
		groupID, int64(epoch), commit, time.Now().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: save commit: %w", err)
	}
	return nil
}

// CommitsAfter returns a group's logged commits with epoch > afterEpoch, in
// ascending epoch order.
func (s *Store) CommitsAfter(groupID []byte, afterEpoch uint64) ([]CommitRow, error) {
	rows, err := s.db.Query(
		`SELECT epoch, commit_b FROM mls_commits WHERE group_id = ? AND epoch > ? ORDER BY epoch ASC`,
		groupID, int64(afterEpoch),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommitRow
	for rows.Next() {
		var r CommitRow
		var epoch int64
		if err := rows.Scan(&epoch, &r.Commit); err != nil {
			return nil, err
		}
		r.Epoch = uint64(epoch)
		out = append(out, r)
	}
	return out, rows.Err()
}

// MessagesChangedSince returns up to limit channel messages sent OR updated
// strictly after sinceNano, oldest first, including deleted tombstones (blank
// content) and per-message reactions. It is the server side of history sync:
// unlike MessagesSince it also serves state changes (edit/delete/pin/react) to
// messages older than the cursor.
func (s *Store) MessagesChangedSince(channelID string, sinceNano int64, limit int) ([]domain.Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(
		`SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, content_enc, nonce, sent, updated
		 FROM messages WHERE channel_id = ? AND (sent > ? OR updated > ?) AND kind IN ('', 'system')
		 ORDER BY sent ASC LIMIT ?`, channelID, sinceNano, sinceNano, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var enc, nonceB []byte
		var sent, updated int64
		var deleted, edited, pinned int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &pinned, &enc, &nonceB, &sent, &updated); err != nil {
			return nil, err
		}
		if deleted != 0 {
			m.Deleted = true // tombstone: content stays blank
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
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	reacts, err := s.reactionsForChannel(channelID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Reactions = reacts[out[i].ID]
	}
	return out, nil
}

// UpsertSyncedMessage applies a message received via history sync: it inserts
// unknown messages and reconciles state (edit/delete/pin/reactions) on known
// ones. Deletion is one-way (a tombstone always wins); edit/pin state is
// adopted only when the remote copy changed more recently than ours; reaction
// rows for fingerprints other than selfFingerprint are replaced by the remote
// snapshot (own reactions are never touched, so un-synced local toggles
// survive). Reports whether anything changed.
func (s *Store) UpsertSyncedMessage(m domain.Message, selfFingerprint string) (bool, error) {
	var curDeleted, curEdited, curPinned int
	var curUpdated int64
	err := s.db.QueryRow(
		`SELECT deleted, edited, pinned, updated FROM messages WHERE id = ?`, m.ID,
	).Scan(&curDeleted, &curEdited, &curPinned, &curUpdated)

	remoteUpdated := int64(0)
	if !m.Updated.IsZero() {
		remoteUpdated = m.Updated.UnixNano()
	}

	changed := false
	switch {
	case err == sql.ErrNoRows:
		var nonce [nonceSize]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return false, err
		}
		sealed := secretbox.Seal(nil, []byte(m.Content), &nonce, &s.key)
		if _, err := s.db.Exec(
			`INSERT INTO messages (id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, content_enc, nonce, sent, updated)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.ChannelID, m.Sender, m.Name, m.Kind, m.ReplyTo,
			boolToInt(m.Deleted), boolToInt(m.Edited), boolToInt(m.Pinned),
			sealed, nonce[:], m.Sent.UnixNano(), remoteUpdated,
		); err != nil {
			return false, fmt.Errorf("store: upsert synced message: %w", err)
		}
		changed = true
	case err != nil:
		return false, err
	default:
		if m.Deleted && curDeleted == 0 {
			if _, err := s.db.Exec(
				`UPDATE messages SET deleted = 1, updated = ? WHERE id = ?`, maxInt64(remoteUpdated, curUpdated), m.ID,
			); err != nil {
				return false, err
			}
			changed = true
		} else if remoteUpdated > curUpdated && curDeleted == 0 {
			var nonce [nonceSize]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				return false, err
			}
			sealed := secretbox.Seal(nil, []byte(m.Content), &nonce, &s.key)
			if _, err := s.db.Exec(
				`UPDATE messages SET content_enc = ?, nonce = ?, edited = ?, pinned = ?, updated = ? WHERE id = ?`,
				sealed, nonce[:], boolToInt(m.Edited), boolToInt(m.Pinned), remoteUpdated, m.ID,
			); err != nil {
				return false, err
			}
			changed = true
		}
	}

	// Reconcile reactions when the remote copy is at least as fresh as ours.
	if remoteUpdated >= curUpdated {
		if rc, err := s.replaceReactionsExceptSelf(m.ID, m.Reactions, selfFingerprint); err == nil && rc {
			changed = true
		}
	}
	return changed, nil
}

// replaceReactionsExceptSelf makes a message's reaction rows for other peers
// match the given snapshot, leaving selfFingerprint's rows untouched. Reports
// whether the stored set actually changed.
func (s *Store) replaceReactionsExceptSelf(messageID string, snapshot map[string][]string, selfFingerprint string) (bool, error) {
	want := map[string]bool{} // "fpr\x00emoji" for everyone but self
	for emoji, fprs := range snapshot {
		for _, fpr := range fprs {
			if fpr != selfFingerprint {
				want[fpr+"\x00"+emoji] = true
			}
		}
	}

	rows, err := s.db.Query(
		`SELECT fingerprint, emoji FROM reactions WHERE message_id = ? AND fingerprint != ?`,
		messageID, selfFingerprint)
	if err != nil {
		return false, err
	}
	have := map[string]bool{}
	for rows.Next() {
		var fpr, emoji string
		if err := rows.Scan(&fpr, &emoji); err != nil {
			rows.Close()
			return false, err
		}
		have[fpr+"\x00"+emoji] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}

	changed := false
	for k := range have {
		if !want[k] {
			fpr, emoji, _ := strings.Cut(k, "\x00")
			if _, err := s.db.Exec(
				`DELETE FROM reactions WHERE message_id = ? AND fingerprint = ? AND emoji = ?`,
				messageID, fpr, emoji); err != nil {
				return false, err
			}
			changed = true
		}
	}
	for k := range want {
		if !have[k] {
			fpr, emoji, _ := strings.Cut(k, "\x00")
			if _, err := s.db.Exec(
				`INSERT INTO reactions (message_id, fingerprint, emoji) VALUES (?, ?, ?)
				 ON CONFLICT(message_id, fingerprint, emoji) DO NOTHING`,
				messageID, fpr, emoji); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// maxAttachmentStore caps the local attachment cache; least-recently-used
// blobs are evicted past it. Evicting the last replica of a blob is accepted
// for now — availability spreads to every member who has viewed the image.
const maxAttachmentStore = 1 << 30 // 1 GiB

// SaveAttachment stores an attachment blob (already-encrypted ciphertext; the
// key travels inside the referencing message, never here). Content-addressed
// and idempotent: saving the same blob twice is a no-op.
func (s *Store) SaveAttachment(blobID string, ct []byte) error {
	now := time.Now().UnixNano()
	_, err := s.db.Exec(
		`INSERT INTO attachments (blob_id, ct, size, created, last_used)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(blob_id) DO NOTHING`,
		blobID, ct, len(ct), now, now,
	)
	if err != nil {
		return fmt.Errorf("store: save attachment: %w", err)
	}
	s.evictAttachments()
	return nil
}

// GetAttachment loads a blob's ciphertext, bumping its recency.
func (s *Store) GetAttachment(blobID string) ([]byte, bool, error) {
	var ct []byte
	err := s.db.QueryRow(`SELECT ct FROM attachments WHERE blob_id = ?`, blobID).Scan(&ct)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	_, _ = s.db.Exec(`UPDATE attachments SET last_used = ? WHERE blob_id = ?`, time.Now().UnixNano(), blobID)
	return ct, true, nil
}

// evictAttachments drops least-recently-used blobs while the cache exceeds
// its size cap. Best-effort.
func (s *Store) evictAttachments() {
	for i := 0; i < 64; i++ { // hard bound on the loop, just in case
		var total sql.NullInt64
		if err := s.db.QueryRow(`SELECT SUM(size) FROM attachments`).Scan(&total); err != nil || total.Int64 <= maxAttachmentStore {
			return
		}
		if _, err := s.db.Exec(
			`DELETE FROM attachments WHERE blob_id IN
			   (SELECT blob_id FROM attachments ORDER BY last_used ASC LIMIT 1)`,
		); err != nil {
			return
		}
	}
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
