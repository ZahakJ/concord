package store

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/ZahakJ/concord/internal/domain"
)

// Opening a database written by a much older Concord.
//
// Every rig this project has ever had — the multi-peer script, the browser
// drivers, all four critic passes, both coverage sweeps — starts by seeding a
// FRESH workspace with the code under test. So the single most common thing a
// real user does, upgrading, was the one path nobody had ever driven, and the
// first report of it was a stranger whose app would not open.
//
// The fixture below is the oldest schema this store has ever written, spelled
// out as raw SQL rather than checked in as a binary .db: a blob is unreadable
// in review, undiffable when it changes, and says nothing about WHICH shape it
// is meant to represent. Written out, the columns that do not exist yet are
// visible as absences, which is exactly what the test is about.
//
// It is deliberately NOT kept in sync with the current schema. It is a
// historical record; the whole point is that it stops changing.
const oldestSchema = `
CREATE TABLE guilds (
  id       TEXT PRIMARY KEY,
  name     TEXT NOT NULL,
  group_id BLOB NOT NULL,
  owner_id BLOB NOT NULL,
  created  INTEGER NOT NULL
);
CREATE TABLE channels (
  id       TEXT PRIMARY KEY,
  guild_id TEXT NOT NULL,
  name     TEXT NOT NULL
);
CREATE TABLE categories (
  id       TEXT PRIMARY KEY,
  guild_id TEXT NOT NULL,
  name     TEXT NOT NULL,
  position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE custom_emoji (
  guild_id TEXT NOT NULL,
  name     TEXT NOT NULL,
  image    TEXT NOT NULL,
  created  INTEGER NOT NULL,
  PRIMARY KEY (guild_id, name)
);
CREATE TABLE messages (
  id          TEXT PRIMARY KEY,
  channel_id  TEXT NOT NULL,
  sender      BLOB NOT NULL,
  content_enc BLOB NOT NULL,
  nonce       BLOB NOT NULL,
  sent        INTEGER NOT NULL
);
CREATE INDEX idx_messages_channel ON messages(channel_id, sent);
CREATE TABLE contacts (
  peer_id     TEXT PRIMARY KEY,
  fingerprint TEXT NOT NULL,
  verified    INTEGER NOT NULL DEFAULT 0,
  first_seen  INTEGER NOT NULL
);
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE reactions (
  message_id  TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  emoji       TEXT NOT NULL,
  PRIMARY KEY (message_id, fingerprint, emoji)
);
CREATE TABLE profiles (
  fingerprint TEXT PRIMARY KEY,
  name        TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT '',
  emoji       TEXT NOT NULL DEFAULT '',
  color       TEXT NOT NULL DEFAULT '',
  avatar      TEXT NOT NULL DEFAULT '',
  updated     INTEGER NOT NULL
);
CREATE TABLE nicknames (
  guild_id    TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  nick        TEXT NOT NULL,
  updated     INTEGER NOT NULL,
  PRIMARY KEY (guild_id, fingerprint)
);
CREATE TABLE guild_ops (
  guild_id TEXT NOT NULL,
  op_hash  TEXT NOT NULL,
  op_json  BLOB NOT NULL,
  created  INTEGER NOT NULL,
  PRIMARY KEY (guild_id, op_hash)
);
CREATE TABLE mls_commits (
  group_id BLOB NOT NULL,
  epoch    INTEGER NOT NULL,
  commit_b BLOB NOT NULL,
  created  INTEGER NOT NULL,
  PRIMARY KEY (group_id, epoch)
);
CREATE TABLE attachments (
  blob_id   TEXT PRIMARY KEY,
  ct        BLOB NOT NULL,
  size      INTEGER NOT NULL,
  created   INTEGER NOT NULL,
  last_used INTEGER NOT NULL
);
`

// writeOldWorkspace lays down a database in the oldest schema with a guild, a
// channel, three messages, a profile and a nickname in it, and returns its path.
// The message bodies are sealed the way that build sealed them, which is the
// same way this one does — the at-rest format is the thing an upgrade may NOT
// change, and a test that wrote plaintext would not be testing an upgrade.
func writeOldWorkspace(t *testing.T, key [32]byte) string {
	t.Helper()
	path := t.TempDir() + "/concord.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(oldestSchema); err != nil {
		t.Fatalf("write oldest schema: %v", err)
	}

	seal := func(body string) ([]byte, []byte) {
		var nonce [nonceSize]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			t.Fatalf("nonce: %v", err)
		}
		return secretbox.Seal(nil, []byte(body), &nonce, &key), nonce[:]
	}

	owner := make([]byte, 32)
	for i := range owner {
		owner[i] = byte(i)
	}
	group := []byte{0xde, 0xad, 0xbe, 0xef}
	if _, err := db.Exec(
		`INSERT INTO guilds (id, name, group_id, owner_id, created) VALUES (?, ?, ?, ?, ?)`,
		"g-old", "Al-Khizana", group, owner, time.Now().UnixNano()); err != nil {
		t.Fatalf("insert guild: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO channels (id, guild_id, name) VALUES (?, ?, ?)`,
		"c-old", "g-old", "general"); err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	base := time.Now().Add(-time.Hour).UnixNano()
	for i, body := range []string{"first", "second", "third"} {
		enc, nonce := seal(body)
		if _, err := db.Exec(
			`INSERT INTO messages (id, channel_id, sender, content_enc, nonce, sent) VALUES (?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("m%d", i), "c-old", owner, enc, nonce, base+int64(i)); err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO profiles (fingerprint, name, status, emoji, color, avatar, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"FPR-OLD", "Zaynab", "building things", "🌙", "#7c5cff", "", time.Now().UnixNano()); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO nicknames (guild_id, fingerprint, nick, updated) VALUES (?, ?, ?, ?)`,
		"g-old", "FPR-OLD", "Zayn", time.Now().UnixNano()); err != nil {
		t.Fatalf("insert nickname: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)`, "display_name", "Zaynab"); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO custom_emoji (guild_id, name, image, created) VALUES (?, ?, ?, ?)`,
		"g-old", "barakah", "data:image/gif;base64,AAAA", time.Now().UnixNano()); err != nil {
		t.Fatalf("insert emoji: %v", err)
	}
	return path
}

// TestOpenOldestWorkspace is the whole upgrade path for the store, end to end:
// the oldest database this project has ever written must open under today's
// code with everything in it still readable.
func TestOpenOldestWorkspace(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = byte(200 - i)
	}
	path := writeOldWorkspace(t, key)

	s, err := Open(path, key[:])
	if err != nil {
		t.Fatalf("current code refused to open an old workspace: %v", err)
	}
	defer s.Close()

	// ---- nothing was lost -------------------------------------------------
	guilds, err := s.Guilds()
	if err != nil {
		t.Fatalf("Guilds: %v", err)
	}
	if len(guilds) != 1 || guilds[0].Name != "Al-Khizana" {
		t.Fatalf("guild did not survive the migration: %+v", guilds)
	}
	if len(guilds[0].Channels) != 1 || guilds[0].Channels[0].Name != "general" {
		t.Fatalf("channel did not survive the migration: %+v", guilds[0].Channels)
	}
	msgs, err := s.Messages("c-old", 100)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages after migration, got %d", len(msgs))
	}
	// Bodies sealed by the old build must still decrypt: the at-rest format is
	// the one thing an upgrade is never allowed to change quietly.
	got := []string{msgs[0].Content, msgs[1].Content, msgs[2].Content}
	sort.Strings(got)
	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("message bodies did not survive: got %v want %v", got, want)
		}
	}
	profiles, err := s.Profiles()
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Name != "Zaynab" || profiles[0].Emoji != "🌙" {
		t.Fatalf("profile did not survive the migration: %+v", profiles)
	}
	nicks, err := s.Nicknames()
	if err != nil {
		t.Fatalf("Nicknames: %v", err)
	}
	if nicks["g-old"]["FPR-OLD"] != "Zayn" {
		t.Fatalf("nickname did not survive the migration: %+v", nicks)
	}
	if v, _ := s.GetSetting("display_name"); v != "Zaynab" {
		t.Fatalf("setting did not survive the migration: %q", v)
	}
	emoji, err := s.CustomEmoji("g-old")
	if err != nil {
		t.Fatalf("CustomEmoji: %v", err)
	}
	if len(emoji) != 1 || emoji[0].Name != "barakah" {
		t.Fatalf("custom emoji did not survive the migration: %+v", emoji)
	}

	// ---- the columns added since are readable, at their defaults ----------
	// A row written before a column existed must read back as the value that
	// column's absence MEANT, not as an error. Every one of these is a query
	// the current code runs at unlock or on the first screen.
	if msgs[0].Unverified {
		t.Error("history we were present for must not come back marked unverified")
	}
	if len(msgs[0].Sig) != 0 {
		t.Error("a message written before signatures existed must have none, not garbage")
	}
	if _, err := s.SearchMessages("first", 10); err != nil {
		t.Errorf("search over migrated history: %v", err)
	}
	if _, err := s.ReadState(); err != nil {
		t.Errorf("ReadState on a migrated workspace: %v", err)
	}
	if _, err := s.BlockedFingerprints(); err != nil {
		t.Errorf("BlockedList on a migrated workspace: %v", err)
	}
	if _, err := s.GuildGifs("g-old"); err != nil {
		t.Errorf("GuildGifs on a migrated workspace: %v", err)
	}
	if _, err := s.Events("g-old"); err != nil {
		t.Errorf("Events on a migrated workspace: %v", err)
	}
	if _, err := s.SavedMessageIDs(); err != nil {
		t.Errorf("SavedMessages on a migrated workspace: %v", err)
	}
	if _, err := s.ChronicleManifests("g-old"); err != nil {
		t.Errorf("ChronicleManifests on a migrated workspace: %v", err)
	}
	if s.GuildIsLeft("g-old") {
		t.Error("a guild in an old database must not read as left")
	}

	// ---- and it is still WRITABLE ----------------------------------------
	// The tolerant ALTERs only help if the new INSERTs, which name columns the
	// old schema never had, actually run against the patched table.
	if _, err := s.SaveMessage(domain.Message{
		ID: "m-new", ChannelID: "c-old", Sender: []byte("sender"),
		Content: "after the upgrade", Sent: time.Now(),
	}); err != nil {
		t.Fatalf("cannot write to a migrated workspace: %v", err)
	}
	after, err := s.Messages("c-old", 100)
	if err != nil {
		t.Fatalf("Messages after write: %v", err)
	}
	if len(after) != 4 {
		t.Fatalf("want 4 messages after writing one, got %d", len(after))
	}
}

// TestMigratedSchemaMatchesFresh is the general form of the same worry: not
// "does this one query work" but "is a migrated database the same shape as a
// new one". Anything the fresh schema has and the migrated one does not is a
// query waiting to fail on somebody's install and nowhere else — which is the
// exact class of bug that is invisible to every test that starts from scratch.
func TestMigratedSchemaMatchesFresh(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = byte(i + 3)
	}

	migrated, err := Open(writeOldWorkspace(t, key), key[:])
	if err != nil {
		t.Fatalf("open old workspace: %v", err)
	}
	defer migrated.Close()

	fresh, err := Open(t.TempDir()+"/fresh.db", key[:])
	if err != nil {
		t.Fatalf("open fresh workspace: %v", err)
	}
	defer fresh.Close()

	shapeOf := func(s *Store) map[string]bool {
		out := map[string]bool{}
		rows, err := s.db.Query(
			`SELECT m.name, p.name FROM sqlite_master m
			 JOIN pragma_table_info(m.name) p
			 WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`)
		if err != nil {
			t.Fatalf("read schema: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var table, col string
			if err := rows.Scan(&table, &col); err != nil {
				t.Fatalf("scan schema: %v", err)
			}
			out[table+"."+col] = true
		}
		return out
	}

	want, got := shapeOf(fresh), shapeOf(migrated)
	var missing []string
	for col := range want {
		if !got[col] {
			missing = append(missing, col)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("a migrated database is missing %d column(s) a fresh one has: %v", len(missing), missing)
	}
}
