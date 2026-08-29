package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// A guild whose group state is not there.
//
// Two halves of a guild live in two places: the row, its channels and every
// message in it are in concord.db, and the MLS group state is a file under
// mls/. Nothing has ever guaranteed the two arrive or survive together — a
// crash between SaveGuild and the first persisted epoch, a half-finished leave,
// a restore that copied the database and not the directory beside it.
//
// The cost of that was out of all proportion to the cause. GuildMembers handed
// the MLS layer's own words straight up to an RPC; the browser fetched the
// member panel while opening; and the failure took the whole unlock down with
// it, leaving the login screen refusing a correct passphrase in silence. The
// account was intact the entire time — every message still readable, every
// channel still there — and none of it could be reached.
//
// So the contract these tests hold is: this condition is REPORTABLE and
// SURVIVABLE. Nothing here asserts a repair, because there is nothing to repair
// locally; the group state comes back from any member on the next sync.

// serviceWithPhantomGuild builds a Service holding one guild whose group id was
// never created in its MLS store — the shape described above, with no network
// and no peers involved.
func serviceWithPhantomGuild(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "concord.db"), bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	engine, err := mls.NewPersistent(pub, priv, filepath.Join(dir, "mls"))
	if err != nil {
		t.Fatalf("mls.NewPersistent: %v", err)
	}

	g := domain.NewGuild("Al-Khizana", []byte{0xde, 0xad, 0xbe, 0xef}, pub)
	return &Service{
		ctx:            context.Background(),
		store:          st,
		mls:            engine,
		guilds:         map[string]*domain.Guild{g.ID: &g},
		channelToGuild: map[string]string{},
	}
}

// TestGuildMembersReportsAMissingRosterAsItsOwnCondition pins the classifier.
// "This guild's membership cannot be read" and "you asked about a guild that
// does not exist" want opposite treatment — one is repairable and harmless, the
// other is a caller bug — and before this they were the same failed RPC.
func TestGuildMembersReportsAMissingRosterAsItsOwnCondition(t *testing.T) {
	s := serviceWithPhantomGuild(t)
	var guildID string
	for id := range s.guilds {
		guildID = id
	}

	_, err := s.GuildMembers(guildID)
	if err == nil {
		t.Fatal("a guild with no group state must not report a roster")
	}
	if !errors.Is(err, ErrRosterUnavailable) {
		t.Fatalf("want ErrRosterUnavailable, got %v", err)
	}
	// The guild's own name has to be in it: this reaches a person, and "a
	// guild" is not something anybody can act on.
	if !bytes.Contains([]byte(err.Error()), []byte("Al-Khizana")) {
		t.Errorf("the message must name the guild, got %q", err)
	}

	// An unknown guild stays the other thing. If these two ever collapse into
	// one error the caller loses the only signal that tells them apart.
	if _, err := s.GuildMembers("no-such-guild"); errors.Is(err, ErrRosterUnavailable) {
		t.Error("an unknown guild must not be reported as a missing roster")
	}
}

// TestRosterUnavailableClassifiesOnTheSentinel guards the classifier against
// being quietly rewired to match on message text — which is upstream's, and
// would stop matching without a single test failing.
func TestRosterUnavailableClassifiesOnTheSentinel(t *testing.T) {
	if !rosterUnavailable(mls.ErrGroupNotFound) {
		t.Error("the sentinel itself must classify as a missing roster")
	}
	if !rosterUnavailable(fmt.Errorf("mls: list members: %w", mls.ErrGroupNotFound)) {
		t.Error("a wrapped sentinel must classify — that is how it arrives in practice")
	}
	// A look-alike that only says the same words must NOT match. Asserting the
	// negative is what keeps the check on errors.Is: a substring test would
	// pass this and would then break silently the day upstream reworded it.
	if rosterUnavailable(errors.New("mls: group not found")) {
		t.Error("classification must be by sentinel, not by matching the words")
	}
	if rosterUnavailable(errors.New("disk on fire")) {
		t.Error("an unrelated error must not read as a missing roster")
	}
	if rosterUnavailable(nil) {
		t.Error("no error is not a missing roster")
	}
}

// TestUnlockSurvivesAGuildWithNoGroupState is the incident itself: a workspace
// whose guild rows have outlived their MLS state must still open, with its
// history intact.
//
// It restarts over the SAME data directory, because that is what an upgrade is
// — the one thing every rig this project has had was missing, and the reason
// this bug reached a user before it reached a test.
func TestUnlockSurvivesAGuildWithNoGroupState(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	cfg := Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true}

	first, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	g, err := first.CreateGuild("Al-Khizana")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	for _, body := range []string{"one", "two", "three"} {
		if _, err := first.SendMessage(channel, body, "", ""); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Take the group state away, leaving the database exactly as it was.
	mlsDir := filepath.Join(dir, "mls")
	entries, err := os.ReadDir(mlsDir)
	if err != nil {
		t.Fatalf("read mls dir: %v", err)
	}
	removed := 0
	for _, e := range entries {
		if err := os.Remove(filepath.Join(mlsDir, e.Name())); err != nil {
			t.Fatalf("remove %s: %v", e.Name(), err)
		}
		removed++
	}
	if removed == 0 {
		t.Fatal("no group state to remove — the fixture proves nothing")
	}

	// The unlock a user would perform after upgrading.
	second, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("unlock refused a workspace whose group state had gone: %v", err)
	}
	defer second.Close()

	// Everything the app needs to draw its first screen still answers.
	guilds := second.Guilds()
	if len(guilds) == 0 {
		t.Fatal("the guild vanished from a workspace whose database still holds it")
	}
	var found *domain.Guild
	for i := range guilds {
		if guilds[i].ID == g.ID {
			found = &guilds[i]
		}
	}
	if found == nil {
		t.Fatalf("guild %s is no longer listed", g.ID)
	}
	msgs, err := second.Messages(channel, 0)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("want the 3 messages back, got %d — this is the data-loss check", len(msgs))
	}

	// …and the one thing that genuinely cannot be answered says so in its own
	// words, rather than taking the unlock down with it.
	if _, err := second.GuildMembers(g.ID); !errors.Is(err, ErrRosterUnavailable) {
		t.Fatalf("want ErrRosterUnavailable from the stranded guild, got %v", err)
	}
}
