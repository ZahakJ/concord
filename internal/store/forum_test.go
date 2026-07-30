package store

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/zahak/concord/internal/domain"
)

func TestChannelForumMetadataRoundTrip(t *testing.T) {
	s, _ := openTestStore(t)
	g := domain.NewGuild("board", []byte("gid"), []byte("owner"))
	g.Channels = append(g.Channels,
		domain.Channel{ID: "forum-1", GuildID: g.ID, Name: "help", Type: "forum",
			ForumTags: []domain.ForumTag{
				{ID: "t1", Name: "Bug", Color: "#ff0000", Emoji: "🐛"},
				{ID: "t2", Name: "Idea", Color: "#00ff00"},
			}},
		domain.Channel{ID: "post-1", GuildID: g.ID, Name: "Cannot log in", Type: "thread",
			Parent: "forum-1", Tags: []string{"t1"}, Pinned: true, Solved: true},
	)
	if err := s.SaveGuild(g); err != nil {
		t.Fatalf("SaveGuild: %v", err)
	}
	guilds, err := s.Guilds()
	if err != nil {
		t.Fatalf("Guilds: %v", err)
	}
	byID := map[string]domain.Channel{}
	for _, c := range guilds[0].Channels {
		byID[c.ID] = c
	}
	forum := byID["forum-1"]
	if len(forum.ForumTags) != 2 || forum.ForumTags[0].Name != "Bug" ||
		forum.ForumTags[0].Emoji != "🐛" || forum.ForumTags[1].Color != "#00ff00" {
		t.Fatalf("palette did not round-trip: %+v", forum.ForumTags)
	}
	post := byID["post-1"]
	if len(post.Tags) != 1 || post.Tags[0] != "t1" || !post.Pinned || !post.Solved {
		t.Fatalf("post board state did not round-trip: %+v", post)
	}
	// A plain channel must come back with nothing set, not with empty non-nil
	// slices that would make every equality check subtly wrong.
	general := byID[guilds[0].Channels[0].ID]
	if general.ForumTags != nil || general.Tags != nil || general.Pinned || general.Solved {
		t.Fatalf("an untouched channel gained forum metadata: %+v", general)
	}
}

// TestForumColumnsAddedToAnOlderDatabase is the on-disk half of the
// compatibility story: a database written before forums had tags must open,
// migrate, and read its channels back — with the new fields simply empty.
func TestForumColumnsAddedToAnOlderDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	// Hand-build the channels table as it existed before this change (through the
	// parent/links migration) and put a row in it.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE channels (
		  id TEXT PRIMARY KEY, guild_id TEXT NOT NULL, name TEXT NOT NULL,
		  type TEXT NOT NULL DEFAULT '', category TEXT NOT NULL DEFAULT '',
		  position INTEGER NOT NULL DEFAULT 0, topic TEXT NOT NULL DEFAULT '',
		  parent TEXT NOT NULL DEFAULT '', links TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE guilds (
		  id TEXT PRIMARY KEY, name TEXT NOT NULL, group_id BLOB NOT NULL,
		  owner_id BLOB NOT NULL, created INTEGER NOT NULL
		);
		INSERT INTO guilds (id, name, group_id, owner_id, created)
		  VALUES ('g1', 'old guild', x'00', x'00', 1);
		INSERT INTO channels (id, guild_id, name, type, parent)
		  VALUES ('c1', 'g1', 'help', 'forum', '');
		INSERT INTO channels (id, guild_id, name, type, parent)
		  VALUES ('c2', 'g1', 'an old post', 'thread', 'c1');
	`); err != nil {
		t.Fatalf("build old schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	s, err := Open(path, bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("Open on an older database: %v", err)
	}
	defer s.Close() //nolint:errcheck
	guilds, err := s.Guilds()
	if err != nil {
		t.Fatalf("Guilds after migration: %v", err)
	}
	if len(guilds) != 1 || len(guilds[0].Channels) != 2 {
		t.Fatalf("channels lost across migration: %+v", guilds)
	}
	for _, c := range guilds[0].Channels {
		if c.ForumTags != nil || c.Tags != nil || c.Pinned || c.Solved {
			t.Errorf("migrated channel %s invented forum metadata: %+v", c.ID, c)
		}
	}
	// And the new fields are writable from here on.
	g := guilds[0]
	g.Channels[0].ForumTags = []domain.ForumTag{{ID: "t1", Name: "Bug", Color: "#ff0000"}}
	if err := s.SaveGuild(g); err != nil {
		t.Fatalf("SaveGuild after migration: %v", err)
	}
	again, _ := s.Guilds()
	if len(again[0].Channels[0].ForumTags) != 1 {
		t.Fatal("palette not writable after migrating an older database")
	}
}

func TestPostStatsForDerivesOpeningAndReplies(t *testing.T) {
	s, _ := openTestStore(t)
	alice, bob := []byte("alice-key"), []byte("bob-key")

	// Opening message first, then replies — with explicit, increasing timestamps
	// so "earliest is the opening" is genuinely under test rather than incidental.
	base := time.Now().Add(-time.Hour).UTC()
	save := func(ch string, sender []byte, name, kind, body string, offset time.Duration) domain.Message {
		t.Helper()
		m, err := domain.NewMessage(ch, sender, body)
		if err != nil {
			t.Fatalf("NewMessage: %v", err)
		}
		m.Name, m.Kind, m.Sent = name, kind, base.Add(offset)
		if _, err := s.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
		return m
	}

	save("post-1", alice, "Alice", "", "the opening question", 0)
	save("post-1", bob, "Bob", "", "a real reply", time.Minute)
	save("post-1", bob, "Bob", "", "another real reply", 2*time.Minute)
	save("post-1", bob, "Bob", "system", "created this channel", 3*time.Minute)
	gone := save("post-1", bob, "Bob", "", "a reply since deleted", 4*time.Minute)
	if _, ok, err := s.MarkDeleted(gone.ID, bob, true); err != nil || !ok {
		t.Fatalf("MarkDeleted: %v (ok=%v)", err, ok)
	}
	save("post-2", bob, "Bob", "", "lonely post", time.Minute)

	stats, err := s.PostStatsFor([]string{"post-1", "post-2", "post-never-synced"})
	if err != nil {
		t.Fatalf("PostStatsFor: %v", err)
	}

	p1 := stats["post-1"]
	if !bytes.Equal(p1.AuthorKey, alice) || p1.AuthorName != "Alice" {
		t.Errorf("opening author wrong: key=%q name=%q", p1.AuthorKey, p1.AuthorName)
	}
	if p1.Opening != "the opening question" {
		t.Errorf("opening body = %q", p1.Opening)
	}
	// Two real replies. The system notice and the tombstone must not count — a
	// board that advertises five replies where two are readable is lying.
	if p1.Replies != 2 {
		t.Errorf("replies = %d, want 2", p1.Replies)
	}
	if p1.Created != base.UnixNano() {
		t.Errorf("created = %d, want the earliest real message %d", p1.Created, base.UnixNano())
	}
	// Activity counts everything, tombstones and system notices included: the
	// deletion at +4m (whose `updated` is later still) is the post's last move.
	if p1.LastAt < base.Add(4*time.Minute).UnixNano() {
		t.Errorf("lastAt = %d, want at least the deleted reply's %d",
			p1.LastAt, base.Add(4*time.Minute).UnixNano())
	}

	// A post with only its opening message has zero replies, not -1.
	if p2 := stats["post-2"]; p2.Replies != 0 || p2.Opening != "lonely post" {
		t.Errorf("single-message post wrong: %+v", p2)
	}
	// A post we have never synced is absent/zero, never invented.
	if p3 := stats["post-never-synced"]; len(p3.AuthorKey) != 0 || p3.Created != 0 || p3.Replies != 0 {
		t.Errorf("unsynced post invented metadata: %+v", p3)
	}
	// The empty case must not build "IN ()".
	if got, err := s.PostStatsFor(nil); err != nil || len(got) != 0 {
		t.Errorf("PostStatsFor(nil) = %v, %v", got, err)
	}
}

// TestPostStatsForBatchesLargeForums guards the bound-variable ceiling: a forum
// with more posts than one IN (…) clause can hold must still work, and the
// batching must not lose or duplicate a post.
func TestPostStatsForBatchesLargeForums(t *testing.T) {
	s, _ := openTestStore(t)
	const n = postStatsBatch*2 + 7
	ids := make([]string, n)
	for i := range ids {
		ids[i] = domain.NewID()
		m, err := domain.NewMessage(ids[i], []byte("author"), "body")
		if err != nil {
			t.Fatalf("NewMessage: %v", err)
		}
		if _, err := s.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
	}
	stats, err := s.PostStatsFor(ids)
	if err != nil {
		t.Fatalf("PostStatsFor(%d posts): %v", n, err)
	}
	if len(stats) != n {
		t.Fatalf("got stats for %d of %d posts", len(stats), n)
	}
	for _, id := range ids {
		if stats[id].Opening != "body" {
			t.Fatalf("post %s lost its opening across batching", id)
		}
	}
}
