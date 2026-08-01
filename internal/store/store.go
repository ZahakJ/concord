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
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
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
CREATE TABLE IF NOT EXISTS guild_gifs (
  guild_id TEXT NOT NULL,
  id       TEXT NOT NULL,
  name     TEXT NOT NULL,
  tags     TEXT NOT NULL DEFAULT '',
  att_keys TEXT NOT NULL,
  subtype  TEXT NOT NULL,
  width    INTEGER NOT NULL DEFAULT 0,
  height   INTEGER NOT NULL DEFAULT 0,
  created  INTEGER NOT NULL,
  PRIMARY KEY (guild_id, id)
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
CREATE TABLE IF NOT EXISTS read_state (
  channel_id TEXT PRIMARY KEY,
  at         INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS blocked (
  fingerprint TEXT PRIMARY KEY,
  created     INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  id         TEXT PRIMARY KEY,
  guild_id   TEXT NOT NULL,
  title      TEXT NOT NULL,
  details    TEXT NOT NULL DEFAULT '',
  start_unix INTEGER NOT NULL,
  end_unix   INTEGER NOT NULL DEFAULT 0,
  location   TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL DEFAULT 0,
  rsvps      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_events_guild ON events(guild_id, start_unix);
CREATE TABLE IF NOT EXISTS pending_members (
  guild_id    TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  created     INTEGER NOT NULL,
  PRIMARY KEY (guild_id, fingerprint)
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
		`ALTER TABLE messages ADD COLUMN expired INTEGER NOT NULL DEFAULT 0`,
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
		`ALTER TABLE profiles ADD COLUMN games TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE profiles ADD COLUMN color2 TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE profiles ADD COLUMN frame TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE profiles ADD COLUMN effect TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE profiles ADD COLUMN style TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE channels ADD COLUMN parent TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE channels ADD COLUMN links TEXT NOT NULL DEFAULT ''`,
		// Forum metadata. forum_tags is the palette on a forum channel (JSON),
		// post_tags the tag IDs on a post (JSON); pinned/solved are the post's
		// board state. All default to empty/0, so a database written before
		// forums had tags reads back as a forum with no tags — which is exactly
		// what it was.
		`ALTER TABLE channels ADD COLUMN forum_tags TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE channels ADD COLUMN post_tags TEXT NOT NULL DEFAULT ''`,
		// A forum's own banner: a data URI or "preset:<id>". Shared, unlike the
		// board's layout/sort, which stay device-local — a banner is part of what
		// the forum IS, so every member should see the same one.
		`ALTER TABLE channels ADD COLUMN banner TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE channels ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE channels ADD COLUMN solved INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := s.db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	// Image-text search is gone (it needed an OCR engine installed outside the
	// binary). Drop the table it filled: the rows are sealed, but they are text
	// read out of people's screenshots, and a feature described as removed should
	// not leave its data sitting in the database forever.
	if _, err := s.db.Exec(`DROP TABLE IF EXISTS attachment_ocr`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	// Index on (channel_id, updated) — added after the column exists. Without it,
	// MAX(updated) per channel is a full-partition scan; LatestTimestamp runs per
	// channel on every sync and presence refresh.
	if _, err := s.db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_messages_channel_updated ON messages(channel_id, updated)`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
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

// PruneContacts deletes unverified contacts whose fingerprint isn't in keep,
// and reports how many went. It exists because contacts used to be recorded for
// every peer the transport dialed: with the public DHT enabled that meant every
// IPFS node in reach, which buried the handful of people you actually know
// under hundreds of strangers. Recording is now gated (see rememberPeer), and
// this clears what the old behaviour left behind.
//
// Verified contacts are never touched, whatever else is true of them —
// verification is a human act and only a human should undo it.
func (s *Store) PruneContacts(keep map[string]bool) (int, error) {
	rows, err := s.db.Query(`SELECT peer_id, fingerprint FROM contacts WHERE verified = 0`)
	if err != nil {
		return 0, fmt.Errorf("store: prune contacts: %w", err)
	}
	var doomed []string
	for rows.Next() {
		var peerID, fpr string
		if err := rows.Scan(&peerID, &fpr); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: prune contacts: %w", err)
		}
		if !keep[fpr] {
			doomed = append(doomed, peerID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: prune contacts: %w", err)
	}
	for _, peerID := range doomed {
		if _, err := s.db.Exec(`DELETE FROM contacts WHERE peer_id = ?`, peerID); err != nil {
			return 0, fmt.Errorf("store: prune contacts: %w", err)
		}
	}
	return len(doomed), nil
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
			`INSERT INTO channels (id, guild_id, name, type, category, position, topic, parent, links,
			   forum_tags, post_tags, pinned, solved, banner)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, type=excluded.type,
			   category=excluded.category, position=excluded.position, topic=excluded.topic,
			   parent=excluded.parent, links=excluded.links, forum_tags=excluded.forum_tags,
			   post_tags=excluded.post_tags, pinned=excluded.pinned, solved=excluded.solved,
			   banner=excluded.banner`,
			c.ID, g.ID, c.Name, c.Type, c.Category, c.Position, c.Topic, c.Parent, encodeLinks(c.Links),
			encodeForumTags(c.ForumTags), encodeLinks(c.Tags), boolToInt(c.Pinned), boolToInt(c.Solved),
			c.Banner,
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
		`SELECT id, guild_id, name, type, category, position, topic, parent, links,
		        forum_tags, post_tags, pinned, solved, banner FROM channels
		 WHERE guild_id = ? ORDER BY position ASC, rowid ASC`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Channel
	for rows.Next() {
		var c domain.Channel
		var links, forumTags, postTags string
		var pinned, solved int
		if err := rows.Scan(&c.ID, &c.GuildID, &c.Name, &c.Type, &c.Category, &c.Position, &c.Topic, &c.Parent,
			&links, &forumTags, &postTags, &pinned, &solved, &c.Banner); err != nil {
			return nil, err
		}
		c.Links = decodeLinks(links)
		c.ForumTags = decodeForumTags(forumTags)
		c.Tags = decodeLinks(postTags)
		c.Pinned, c.Solved = pinned != 0, solved != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// encodeForumTags/decodeForumTags pack a forum's tag palette as JSON ("" = none).
// A row that fails to decode yields no tags rather than an error: the palette is
// decoration on a channel, and refusing to load the guild because one channel's
// tag blob is corrupt would lose the messages too.
func encodeForumTags(tags []domain.ForumTag) string {
	if len(tags) == 0 {
		return ""
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return ""
	}
	return string(b)
}

func decodeForumTags(raw string) []domain.ForumTag {
	if raw == "" {
		return nil
	}
	var out []domain.ForumTag
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// encodeLinks/decodeLinks pack a channel's consumer links as JSON ("" = none).
func encodeLinks(links []string) string {
	if len(links) == 0 {
		return ""
	}
	b, err := json.Marshal(links)
	if err != nil {
		return ""
	}
	return string(b)
}

func decodeLinks(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
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

// UpdateChannelForumMeta writes just one channel's forum state. SaveGuild would
// also do it, but it upserts EVERY channel in the guild — so pinning one post in
// a forum with five hundred of them would rewrite five hundred rows, on an action
// a moderator repeats. Narrow by design.
func (s *Store) UpdateChannelForumMeta(c domain.Channel) error {
	_, err := s.db.Exec(
		`UPDATE channels SET forum_tags=?, post_tags=?, pinned=?, solved=?, banner=? WHERE id=?`,
		encodeForumTags(c.ForumTags), encodeLinks(c.Tags), boolToInt(c.Pinned), boolToInt(c.Solved),
		c.Banner, c.ID)
	if err != nil {
		return fmt.Errorf("store: update channel forum meta: %w", err)
	}
	return nil
}

// UpdateChannelName renames one channel. Its own statement rather than a field
// on UpdateChannelMeta: that method is called by the channel_updated lane with a
// bare four-field Channel, and adding name there would blank every channel's
// name each time somebody moved one — the same hazard the forum banner dodged.
func (s *Store) UpdateChannelName(id, name string) error {
	if _, err := s.db.Exec(`UPDATE channels SET name=? WHERE id=?`, name, id); err != nil {
		return fmt.Errorf("store: rename channel: %w", err)
	}
	return nil
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

// SaveEvent upserts one calendar event. guild_id, created_by and created_at
// are deliberately NOT in the update set: identity and authorship are
// immutable, so even a caller that was lied to cannot move an event between
// guilds or reassign its author by re-saving it. RSVPs ride the row as JSON —
// the map is small (one short word per member) and is always read and written
// together with its event.
func (s *Store) SaveEvent(e domain.Event) error {
	rsvps := ""
	if len(e.RSVPs) > 0 {
		b, err := json.Marshal(e.RSVPs)
		if err != nil {
			return fmt.Errorf("store: save event: %w", err)
		}
		rsvps = string(b)
	}
	_, err := s.db.Exec(
		`INSERT INTO events (id, guild_id, title, details, start_unix, end_unix, location, created_by, created_at, updated_at, rsvps)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   title=excluded.title, details=excluded.details,
		   start_unix=excluded.start_unix, end_unix=excluded.end_unix,
		   location=excluded.location, updated_at=excluded.updated_at,
		   rsvps=excluded.rsvps`,
		e.ID, e.GuildID, e.Title, e.Details, e.StartUnix, e.EndUnix,
		e.Location, e.CreatedBy, e.CreatedAt, e.UpdatedAt, rsvps)
	if err != nil {
		return fmt.Errorf("store: save event: %w", err)
	}
	return nil
}

// DeleteEvent removes one event. Guild-scoped so a caller fed a foreign event
// id cannot delete another guild's entry through this guild's lane.
func (s *Store) DeleteEvent(guildID, id string) error {
	_, err := s.db.Exec(`DELETE FROM events WHERE guild_id=? AND id=?`, guildID, id)
	return err
}

func scanEvent(scan func(dest ...any) error) (domain.Event, error) {
	var e domain.Event
	var rsvps string
	if err := scan(&e.ID, &e.GuildID, &e.Title, &e.Details, &e.StartUnix,
		&e.EndUnix, &e.Location, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &rsvps); err != nil {
		return domain.Event{}, err
	}
	if rsvps != "" {
		_ = json.Unmarshal([]byte(rsvps), &e.RSVPs)
	}
	return e, nil
}

// EventByID fetches one event regardless of guild. Global on purpose: callers
// compare the returned GuildID against the lane the reference arrived on, the
// same cross-guild-hijack check channel_added performs — a lookup scoped to
// the claimed guild would report "unknown" and let an upsert overwrite another
// guild's row through the primary key.
func (s *Store) EventByID(id string) (domain.Event, bool, error) {
	row := s.db.QueryRow(
		`SELECT id, guild_id, title, details, start_unix, end_unix, location, created_by, created_at, updated_at, rsvps
		 FROM events WHERE id=?`, id)
	e, err := scanEvent(row.Scan)
	if err == sql.ErrNoRows {
		return domain.Event{}, false, nil
	}
	if err != nil {
		return domain.Event{}, false, err
	}
	return e, true, nil
}

// Events returns a guild's calendar ordered by start time.
func (s *Store) Events(guildID string) ([]domain.Event, error) {
	rows, err := s.db.Query(
		`SELECT id, guild_id, title, details, start_unix, end_unix, location, created_by, created_at, updated_at, rsvps
		 FROM events WHERE guild_id=? ORDER BY start_unix ASC, created_at ASC, id ASC`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		e, err := scanEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
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

// GuildGifRow is one entry in a guild's GIF pack: the searchable text plus a
// reference to the encrypted attachment blob that holds the image. The bytes
// live in the attachments table like any other image — this row is metadata.
type GuildGifRow struct {
	GuildID, ID, Name, Tags, Keys, Subtype string
	Width, Height                          int
}

// SaveGuildGif upserts one guild GIF (id = the attachment blob id).
func (s *Store) SaveGuildGif(g GuildGifRow) error {
	_, err := s.db.Exec(
		`INSERT INTO guild_gifs (guild_id, id, name, tags, att_keys, subtype, width, height, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(guild_id, id) DO UPDATE SET
		   name=excluded.name, tags=excluded.tags, att_keys=excluded.att_keys,
		   subtype=excluded.subtype, width=excluded.width, height=excluded.height`,
		g.GuildID, g.ID, g.Name, g.Tags, g.Keys, g.Subtype, g.Width, g.Height, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("store: save guild gif: %w", err)
	}
	return nil
}

// DeleteGuildGif removes one guild GIF. The blob stays: messages already sent
// from it still point at that attachment.
func (s *Store) DeleteGuildGif(guildID, id string) error {
	_, err := s.db.Exec(`DELETE FROM guild_gifs WHERE guild_id=? AND id=?`, guildID, id)
	return err
}

// GuildGifs returns a guild's pack, newest first (how a picker wants it).
func (s *Store) GuildGifs(guildID string) ([]GuildGifRow, error) {
	rows, err := s.db.Query(
		`SELECT guild_id, id, name, tags, att_keys, subtype, width, height
		 FROM guild_gifs WHERE guild_id=? ORDER BY created DESC`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GuildGifRow
	for rows.Next() {
		var g GuildGifRow
		if err := rows.Scan(&g.GuildID, &g.ID, &g.Name, &g.Tags, &g.Keys, &g.Subtype, &g.Width, &g.Height); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GuildGif returns one entry of a guild's pack.
func (s *Store) GuildGif(guildID, id string) (GuildGifRow, bool, error) {
	var g GuildGifRow
	err := s.db.QueryRow(
		`SELECT guild_id, id, name, tags, att_keys, subtype, width, height
		 FROM guild_gifs WHERE guild_id=? AND id=?`, guildID, id).
		Scan(&g.GuildID, &g.ID, &g.Name, &g.Tags, &g.Keys, &g.Subtype, &g.Width, &g.Height)
	if err == sql.ErrNoRows {
		return GuildGifRow{}, false, nil
	}
	if err != nil {
		return GuildGifRow{}, false, err
	}
	return g, true, nil
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
	// Two single-column MAXes, each served by a covering index
	// (idx_messages_channel on sent, idx_messages_channel_updated on updated),
	// then the larger. The old nested MAX(MAX(sent),MAX(updated)) couldn't use
	// either index and scanned the whole channel partition.
	var maxSent, maxUpdated sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT MAX(sent) FROM messages WHERE channel_id = ?`, channelID).Scan(&maxSent); err != nil {
		return 0, err
	}
	if err := s.db.QueryRow(
		`SELECT MAX(updated) FROM messages WHERE channel_id = ?`, channelID).Scan(&maxUpdated); err != nil {
		return 0, err
	}
	return maxInt64(maxSent.Int64, maxUpdated.Int64), nil
}

// UnreadCounts returns, per channel, how many normal (non-system) messages are
// strictly newer than that channel's cursor in sinceNano — WITHOUT decrypting a
// single body. The prior approach fetched and secretbox-opened up to 200 rows
// per channel just to count them, on every login and read-state event.
func (s *Store) UnreadCounts(sinceNano map[string]int64) (map[string]int, error) {
	out := make(map[string]int, len(sinceNano))
	for channelID, since := range sinceNano {
		var n int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM messages
			 WHERE channel_id = ? AND sent > ? AND deleted = 0 AND kind IN ('', 'guest')`,
			channelID, since).Scan(&n); err != nil {
			return nil, err
		}
		out[channelID] = n
	}
	return out, nil
}

// Messages returns up to limit most-recent messages for a channel, oldest
// first, decrypting bodies. A limit <= 0 returns all messages.
func (s *Store) Messages(channelID string, limit int) ([]domain.Message, error) {
	q := `SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, expired, content_enc, nonce, sent
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
		var deleted, edited, pinned, expired int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &pinned, &expired, &enc, &nonceB, &sent); err != nil {
			return nil, err
		}
		m.Edited = edited != 0
		m.Pinned = pinned != 0
		m.Expired = expired != 0
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

// MessagesBefore returns up to limit messages in a channel strictly OLDER than
// beforeNano, oldest-first — the page to prepend when the reader scrolls to the
// top of the loaded window. This is what lets history past the initial 200-row
// load actually be seen; the rows have been in the DB all along.
func (s *Store) MessagesBefore(channelID string, beforeNano int64, limit int) ([]domain.Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(
		`SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, expired, content_enc, nonce, sent
		 FROM messages WHERE channel_id = ? AND sent < ? ORDER BY sent DESC LIMIT ?`,
		channelID, beforeNano, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []domain.Message
	for rows.Next() {
		var m domain.Message
		var enc, nonceB []byte
		var sent int64
		var deleted, edited, pinned, expired int
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Name, &m.Kind, &m.ReplyTo, &deleted, &edited, &pinned, &expired, &enc, &nonceB, &sent); err != nil {
			return nil, err
		}
		m.Edited = edited != 0
		m.Pinned = pinned != 0
		m.Expired = expired != 0
		if deleted != 0 {
			m.Deleted = true
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

// EraseContent overwrites a message's stored body with an encrypted empty
// string — a REAL delete, for DMs. Once erased there is nothing left to recover
// in-app on either side (both honest clients run this when they process the
// delete). The row survives as a tombstone (deleted=1) so the "deleted" marker
// can still show; only the content is gone.
func (s *Store) EraseContent(id string) error {
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	sealed := secretbox.Seal(nil, []byte(""), &nonce, &s.key)
	_, err := s.db.Exec(
		`UPDATE messages SET content_enc = ?, nonce = ? WHERE id = ?`,
		sealed, nonce[:], id,
	)
	return err
}

// MessageContent returns one message's decrypted body (empty if the row is
// missing or its content was erased). Used to let a moderator reveal a
// soft-deleted guild message's original text.
func (s *Store) MessageContent(id string) (string, error) {
	var enc, nonceB []byte
	err := s.db.QueryRow(`SELECT content_enc, nonce FROM messages WHERE id = ?`, id).Scan(&enc, &nonceB)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return s.open(enc, nonceB)
}

// MarkExpired flags a tombstoned message as having disappeared via a timer
// (rather than a manual delete), so the UI can label it "disappeared".
func (s *Store) MarkExpired(id string) error {
	_, err := s.db.Exec(`UPDATE messages SET expired = 1 WHERE id = ?`, id)
	return err
}

// PurgeDeletedContent permanently erases the retained body of every soft-deleted
// message (guild deletes keep content for moderator reveal). Returns how many
// rows were scrubbed. This is the "empty trash" action: after it, "Show
// original" has nothing left to show. channelIDs scopes it to a guild's
// channels; empty scopes it to the whole device.
func (s *Store) PurgeDeletedContent(channelIDs []string) (int, error) {
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0, err
	}
	sealed := secretbox.Seal(nil, []byte(""), &nonce, &s.key)

	q := `UPDATE messages SET content_enc = ?, nonce = ? WHERE deleted = 1`
	args := []any{sealed, nonce[:]}
	if len(channelIDs) > 0 {
		ph := make([]string, len(channelIDs))
		for i, id := range channelIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		q += ` AND channel_id IN (` + strings.Join(ph, ",") + `)`
	}
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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
	Games                                           string // JSON array of games ("" = none)
	Color2, Frame, Effect, Style                    string
}

// SaveProfile upserts a peer's learned profile so display names (and their
// mailbox key) survive restarts instead of living only in memory.
func (s *Store) SaveProfile(p ProfileRow) error {
	_, err := s.db.Exec(
		`INSERT INTO profiles (fingerprint, name, status, emoji, color, avatar, banner, presence, bio, mailbox_pub, games, color2, frame, effect, style, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(fingerprint) DO UPDATE SET
		   name=excluded.name, status=excluded.status, emoji=excluded.emoji,
		   color=excluded.color, avatar=excluded.avatar, banner=excluded.banner,
		   presence=excluded.presence, bio=excluded.bio, mailbox_pub=excluded.mailbox_pub,
		   games=excluded.games, color2=excluded.color2,
		   frame=excluded.frame, effect=excluded.effect, style=excluded.style, updated=excluded.updated`,
		p.Fingerprint, p.Name, p.Status, p.Emoji, p.Color, p.Avatar, p.Banner, p.Presence, p.Bio, p.MailboxPub, p.Games, p.Color2, p.Frame, p.Effect, p.Style, time.Now().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: save profile: %w", err)
	}
	return nil
}

// Profiles returns every learned peer profile.
func (s *Store) Profiles() ([]ProfileRow, error) {
	rows, err := s.db.Query(`SELECT fingerprint, name, status, emoji, color, avatar, banner, presence, bio, mailbox_pub, games, color2, frame, effect, style FROM profiles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProfileRow
	for rows.Next() {
		var p ProfileRow
		if err := rows.Scan(&p.Fingerprint, &p.Name, &p.Status, &p.Emoji, &p.Color, &p.Avatar, &p.Banner, &p.Presence, &p.Bio, &p.MailboxPub, &p.Games, &p.Color2, &p.Frame, &p.Effect, &p.Style); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AdvanceReadState records that the user has read a channel through at
// (UnixMilli), keeping the newest value seen. Reports whether the stored
// cursor actually advanced (false = we already knew a newer read time, e.g.
// a stale marker from another device arriving late).
func (s *Store) AdvanceReadState(channelID string, at int64) (bool, error) {
	res, err := s.db.Exec(
		`INSERT INTO read_state (channel_id, at) VALUES (?, ?)
		 ON CONFLICT(channel_id) DO UPDATE SET at=excluded.at WHERE excluded.at > read_state.at`,
		channelID, at)
	if err != nil {
		return false, fmt.Errorf("store: advance read state: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteReadState drops the read cursors for channels that no longer exist
// (guild left, channel removed), so the table doesn't accumulate orphans.
func (s *Store) DeleteReadState(channelIDs []string) error {
	for _, id := range channelIDs {
		if _, err := s.db.Exec(`DELETE FROM read_state WHERE channel_id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// ReadState returns every channel's read-through time (UnixMilli).
func (s *Store) ReadState() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT channel_id, at FROM read_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
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
// content) and per-message reactions. It is the server side of history sync,
// serving state changes (edit/delete/pin/react) to messages older than the
// cursor as well as new ones.
func (s *Store) MessagesChangedSince(channelID string, sinceNano int64, limit int) ([]domain.Message, error) {
	if limit <= 0 {
		limit = 200
	}
	// 'app' is included so app-plane payloads sync like any other message: a
	// member who was offline still receives the machine traffic they missed.
	rows, err := s.db.Query(
		`SELECT id, channel_id, sender, name, kind, reply_to, deleted, edited, pinned, content_enc, nonce, sent, updated
		 FROM messages WHERE channel_id = ? AND (sent > ? OR updated > ?) AND kind IN ('', 'system', 'app')
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
//
// trusted gates the DESTRUCTIVE reconcile branches (tombstoning or overwriting a
// message we already hold). Those mutate an existing, already-authenticated
// message, so a malicious backfill peer could otherwise censor or rewrite any
// message by ID. Inserts of genuinely new (gap-fill) messages are always
// allowed so ordinary-member catch-up keeps working; only the caller-designated
// trusted sources (guild owner / SyncHost) may mutate existing rows.
func (s *Store) UpsertSyncedMessage(m domain.Message, selfFingerprint string, trusted bool) (bool, error) {
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
	inserted := false
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
		inserted = true
	case err != nil:
		return false, err
	case !trusted:
		// Row already exists and the serving peer isn't a trusted sync source:
		// refuse to tombstone or overwrite it. Only reaction reconciliation (below,
		// which never touches our own rows) is allowed from an untrusted backfill.
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

	// Reconcile reactions when the remote copy is at least as fresh as ours — but
	// only from a trusted source, or for a message we just inserted (whose reaction
	// set arrives with it). An untrusted backfill peer must not rewrite the
	// reaction rows of a message we already hold.
	if remoteUpdated >= curUpdated && (trusted || inserted) {
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

// ---- pending guild members (added, not yet joined) ----

func (s *Store) AddPendingMember(guildID, fpr string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO pending_members (guild_id, fingerprint, created) VALUES (?, ?, ?)", guildID, fpr, time.Now().UnixNano())
	return err
}

func (s *Store) RemovePendingMember(guildID, fpr string) error {
	_, err := s.db.Exec("DELETE FROM pending_members WHERE guild_id = ? AND fingerprint = ?", guildID, fpr)
	return err
}

// PendingMembers returns guildID -> [fingerprints] for every recorded pending
// member (loaded into memory at startup).
func (s *Store) PendingMembers() (map[string][]string, error) {
	rows, err := s.db.Query("SELECT guild_id, fingerprint FROM pending_members")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var g, f string
		if err := rows.Scan(&g, &f); err != nil {
			return nil, err
		}
		out[g] = append(out[g], f)
	}
	return out, rows.Err()
}

// ---- blocked users ----

// BlockFingerprint adds an account fingerprint to the block list (idempotent).
func (s *Store) BlockFingerprint(fpr string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO blocked (fingerprint, created) VALUES (?, ?)", fpr, time.Now().UnixNano())
	return err
}

// UnblockFingerprint removes an account fingerprint from the block list.
func (s *Store) UnblockFingerprint(fpr string) error {
	_, err := s.db.Exec("DELETE FROM blocked WHERE fingerprint = ?", fpr)
	return err
}

// BlockedFingerprints lists every blocked account fingerprint.
func (s *Store) BlockedFingerprints() ([]string, error) {
	rows, err := s.db.Query("SELECT fingerprint FROM blocked")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---- storage stats (read-only aggregates for the Stats panel) ----

// DBSizeBytes is the on-disk size of the SQLite database.
func (s *Store) DBSizeBytes() (int64, error) {
	var pageCount, pageSize int64
	if err := s.db.QueryRow("PRAGMA page_count").Scan(&pageCount); err != nil {
		return 0, err
	}
	if err := s.db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, err
	}
	return pageCount * pageSize, nil
}

// GuildStorageStats aggregates the message footprint of a set of channels.
// Bytes is the stored (encrypted) message payload — a close proxy for text size.
type GuildStorageStats struct {
	Messages int64
	Bytes    int64
	Oldest   int64 // unix seconds, 0 if none
	Newest   int64
}

// GuildStorage sums message count/bytes/age across the given channel IDs.
func (s *Store) GuildStorage(channelIDs []string) (GuildStorageStats, error) {
	var st GuildStorageStats
	if len(channelIDs) == 0 {
		return st, nil
	}
	ph := make([]string, len(channelIDs))
	args := make([]any, len(channelIDs))
	for i, id := range channelIDs {
		ph[i] = "?"
		args[i] = id
	}
	// `sent` is stored as UnixNano; divide to unix seconds for the view.
	q := "SELECT COUNT(*), COALESCE(SUM(LENGTH(content_enc)),0), " +
		"COALESCE(MIN(sent),0)/1000000000, COALESCE(MAX(sent),0)/1000000000 " +
		"FROM messages WHERE deleted = 0 AND channel_id IN (" + strings.Join(ph, ",") + ")"
	err := s.db.QueryRow(q, args...).Scan(&st.Messages, &st.Bytes, &st.Oldest, &st.Newest)
	return st, err
}

// PostStats is what a forum board needs to know about one post, DERIVED from
// the post's own messages rather than carried on its channel record. Opening is
// the post's first real message: its author is MLS-authenticated, which makes
// deriving strictly more trustworthy than a self-asserted "author" field on a
// channel announcement — and free, because the row is already here.
//
// Zero values are meaningful: a post whose history has not synced yet has no
// author, no body and no timestamp. The board renders that as a pending card;
// it must not render it as a post by nobody.
type PostStats struct {
	AuthorKey  []byte // opening message's sender (account key), nil if unsynced
	AuthorName string // sender's self-asserted name at post time (decorative)
	Opening    string // opening message body, full text — callers truncate
	Created    int64  // opening message time, UnixNano; 0 if unsynced
	Replies    int    // real messages after the opening one; never negative
	LastAt     int64  // newest sent/updated time in the post, UnixNano
	// Media is the first image attachment token belonging to the POST ITSELF —
	// the opening message, or one of the attachment-only messages the composer
	// posts immediately after it under the same author.
	//
	// Derived here rather than scraped out of Opening by the client, which is
	// what the board did first and why no real post ever showed a picture: the
	// composer sends attachments as their own messages, so the token was never
	// in the opening body at all, and even inline it lost a race with the
	// 240-character excerpt cut. Prose length silently deciding whether your
	// picture appears is not a bug you find by reading the code.
	Media string
}

// postStatsBatch is how many channel ids go into one IN (…) clause. SQLite has a
// hard ceiling on bound variables per statement, so a forum that grows past it
// would start failing outright rather than getting slower — the kind of bug that
// only appears on the busiest board in the guild, years in.
const postStatsBatch = 400

// openingBatchScan bounds how many of a post's earliest messages are examined to
// find where the post ends and the conversation begins. Six covers a composer
// that staged five attachments; beyond that the extra messages are replies by
// any reasonable reading, and the cap is what keeps a busy post costing the same
// as a quiet one.
const openingBatchScan = 6

// attachmentOnly reports whether a message body is nothing but attachment
// tokens — what the composer posts after an opening message when pictures were
// staged with it. Anything with words in it is somebody talking.
func attachmentOnly(body string) bool {
	return strings.TrimSpace(attachTokenRe.ReplaceAllString(body, "")) == ""
}

// firstImageToken returns the first inline IMAGE attachment token in a body, or
// "" if there is none. File tokens are deliberately not media: a card shows a
// picture, not a spreadsheet.
func firstImageToken(body string) string {
	return imageTokenRe.FindString(body)
}

// Mirrors the token grammar in internal/app/attach.go and lib/attachments.js.
// v1 and v2 both, since an image sent with a spoiler or a description is still
// the picture this post is about.
var (
	imageTokenRe  = regexp.MustCompile(`!\[image\]\(concord://attach/v[12]/[^)\s]+\)`)
	attachTokenRe = regexp.MustCompile(`!?\[(?:image|file)\]\(concord://(?:attach|file)/v[12]/[^)\s]+\)`)
)

// PostStatsFor derives PostStats for a set of post channels in three queries per
// batch, not three per post. A forum with two hundred posts refreshes on every
// guild update; the per-post shape of this (a MessageCount here, a
// LatestTimestamp there) is how a board becomes visibly slow on a laptop.
//
// "Real message" means a non-deleted normal or relayed-guest message: system
// notices are the room's history of itself, and counting tombstones would make
// the board advertise replies that are not there.
func (s *Store) PostStatsFor(channelIDs []string) (map[string]PostStats, error) {
	out := make(map[string]PostStats, len(channelIDs))
	for start := 0; start < len(channelIDs); start += postStatsBatch {
		end := min(start+postStatsBatch, len(channelIDs))
		if err := s.postStatsInto(channelIDs[start:end], out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// postStatsInto accumulates one batch's worth of stats into out.
func (s *Store) postStatsInto(channelIDs []string, out map[string]PostStats) error {
	if len(channelIDs) == 0 {
		return nil
	}
	ph := make([]string, len(channelIDs))
	args := make([]any, len(channelIDs))
	for i, id := range channelIDs {
		ph[i] = "?"
		args[i] = id
	}
	in := "(" + strings.Join(ph, ",") + ")"
	const realMsg = ` AND deleted = 0 AND (kind = '' OR kind = 'guest')`

	// Opening message per channel. The bare columns alongside MIN(sent) are
	// SQLite's documented min/max special case: they come from the row that
	// produced the minimum, so this is one grouped scan instead of a correlated
	// subquery per post. (Only guaranteed with a single MIN/MAX aggregate, which
	// is why the reply count below is its own query rather than another column.)
	rows, err := s.db.Query(
		`SELECT channel_id, sender, name, content_enc, nonce, MIN(sent) FROM messages
		 WHERE channel_id IN `+in+realMsg+` GROUP BY channel_id`, args...)
	if err != nil {
		return fmt.Errorf("store: post openings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var chID, name string
		var sender, enc, nonceB []byte
		var sent int64
		if err := rows.Scan(&chID, &sender, &name, &enc, &nonceB, &sent); err != nil {
			return err
		}
		st := out[chID]
		st.AuthorKey, st.AuthorName, st.Created = sender, name, sent
		// A body we cannot open is not a reason to drop the whole board: show the
		// post with an empty excerpt, which is also what a deleted opening gives.
		if body, err := s.open(enc, nonceB); err == nil {
			st.Opening = body
		}
		out[chID] = st
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// The post's OPENING BATCH: the opening message plus the attachment-only
	// messages the composer sends straight after it, by the same author, before
	// anyone else has spoken. Those are part of the post, not replies to it —
	// counting them made every post with a picture claim a reply it never had,
	// and quietly excluded it from "unanswered", which is the board's headline
	// filter.
	//
	// Bounded by openingBatchScan rows per post: a window function does the
	// limiting in SQLite so this stays one query, and the bound means a post
	// with a thousand messages costs the same as one with ten.
	lead, err := s.db.Query(
		`SELECT channel_id, sender, content_enc, nonce FROM (
		   SELECT channel_id, sender, content_enc, nonce, sent,
		          ROW_NUMBER() OVER (PARTITION BY channel_id ORDER BY sent ASC) rn
		   FROM messages WHERE channel_id IN `+in+realMsg+`
		 ) WHERE rn <= ? ORDER BY channel_id, sent ASC`,
		append(append([]any{}, args...), openingBatchScan)...)
	if err != nil {
		return fmt.Errorf("store: post opening batch: %w", err)
	}
	defer lead.Close()
	batch := map[string]int{} // channel -> messages that are part of the post
	for lead.Next() {
		var chID string
		var sender, enc, nonceB []byte
		if err := lead.Scan(&chID, &sender, &enc, &nonceB); err != nil {
			return err
		}
		st := out[chID]
		// Stop at the first message that is not the author still attaching: a
		// word from them, or anyone else's message, ends the post and begins the
		// conversation.
		body, err := s.open(enc, nonceB)
		if err != nil {
			continue
		}
		first := batch[chID] == 0
		sameAuthor := len(st.AuthorKey) > 0 && bytes.Equal(sender, st.AuthorKey)
		if !first && (!sameAuthor || !attachmentOnly(body) || batch[chID] < 0) {
			batch[chID] = -batch[chID] // negative marks "closed", keeping the count
			continue
		}
		if st.Media == "" {
			if tok := firstImageToken(body); tok != "" {
				st.Media = tok
				out[chID] = st
			}
		}
		batch[chID]++
	}
	if err := lead.Err(); err != nil {
		return err
	}

	counts, err := s.groupedCount(`SELECT channel_id, COUNT(*) FROM messages
		 WHERE channel_id IN `+in+realMsg+` GROUP BY channel_id`, args)
	if err != nil {
		return fmt.Errorf("store: post reply counts: %w", err)
	}
	for chID, n := range counts {
		st := out[chID]
		lead := batch[chID]
		if lead < 0 {
			lead = -lead
		}
		if lead < 1 {
			lead = 1 // the opening message always belongs to the post
		}
		if int(n) > lead {
			st.Replies = int(n) - lead
		}
		out[chID] = st
	}

	// Activity ordering counts everything, including edits and system notices:
	// the board's "3h ago" is when the post last moved, not when it last gained
	// a reply. Matches LatestTimestamp, which is what ChannelView.LastActivity
	// already reports for the same channels.
	act, err := s.db.Query(
		`SELECT channel_id, MAX(sent), MAX(updated) FROM messages
		 WHERE channel_id IN `+in+` GROUP BY channel_id`, args...)
	if err != nil {
		return fmt.Errorf("store: post activity: %w", err)
	}
	defer act.Close()
	for act.Next() {
		var chID string
		var maxSent, maxUpdated sql.NullInt64
		if err := act.Scan(&chID, &maxSent, &maxUpdated); err != nil {
			return err
		}
		st := out[chID]
		st.LastAt = maxInt64(maxSent.Int64, maxUpdated.Int64)
		out[chID] = st
	}
	return act.Err()
}

// groupedCount runs a "SELECT key, COUNT(*) … GROUP BY key" and collects it.
func (s *Store) groupedCount(query string, args []any) (map[string]int64, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key string
		var n int64
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}

// AttachmentTotals is the global blob store footprint. Blobs are content-
// addressed + deduped and not guild-tagged, so this is a whole-device total.
func (s *Store) AttachmentTotals() (count int64, bytes int64, err error) {
	err = s.db.QueryRow("SELECT COUNT(*), COALESCE(SUM(LENGTH(ct)),0) FROM attachments").Scan(&count, &bytes)
	return count, bytes, err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
