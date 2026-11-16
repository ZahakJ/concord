package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// tombstoneOf is the row as a source that applied its delete serves it.
func tombstoneOf(m domain.Message) domain.Message {
	m.Content = ""
	m.Sig = nil
	m.Deleted = true
	m.Updated = time.Now().UTC()
	return m
}

// heldBody is s's stored body of id, read the way a reveal reads it.
func heldBody(t *testing.T, s *Service, id string) string {
	t.Helper()
	body, err := s.store.MessageContent(id)
	if err != nil {
		t.Fatalf("MessageContent: %v", err)
	}
	return body
}

// In a guild, a delete for everyone erases the body on every device that may
// not reveal it, and a moderator's device keeps it for reveal. A tombstone
// history sync brings keeps no body anywhere, a moderator's device included.
// A device that gains ManageMessages after the delete has nothing to reveal
// and says so.
func TestAGuildDeleteKeepsTheBodyOnlyWhereItCanBeRevealed(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	mod := startService(t, ctx)
	reader := startService(t, ctx)
	g := guildWith(t, owner, mod, reader)
	ch := g.Channels[0].ID
	modRole, err := owner.UpsertRole(g.ID, "", "Moderator", "", PermManageMessages, 1)
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := owner.AssignRole(g.ID, mod.Fingerprint(), modRole, true); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	waitFor(t, 60*time.Second, func() bool {
		return mod.hasPerm(g.ID, PermManageMessages) && reader.memberHasPerm(g.ID, mod.Fingerprint(), PermManageMessages)
	}, "the moderator role never reached every member")

	everyone := map[string]*Service{"owner": owner, "mod": mod, "reader": reader}
	send := func(text string) domain.Message {
		m, err := reader.SendMessage(ch, text, "", "")
		if err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		waitFor(t, 30*time.Second, func() bool {
			for _, s := range everyone {
				if _, ok, _ := s.store.MessageByID(m.ID); !ok {
					return false
				}
			}
			return true
		}, "a message never reached every member")
		return m
	}

	live := send("deleted live")
	if err := owner.DeleteMessage(ch, live.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		for _, s := range everyone {
			if row, ok, _ := s.store.MessageByID(live.ID); !ok || !row.Deleted {
				return false
			}
		}
		return true
	}, "the delete never reached every member")
	for name, s := range map[string]*Service{"owner": owner, "mod": mod} {
		if body := heldBody(t, s, live.ID); body != "deleted live" {
			t.Errorf("the %s's device did not keep the body for reveal: %q", name, body)
		}
		if got, err := s.RevealDeleted(ch, live.ID); err != nil || got != "deleted live" {
			t.Errorf("the %s could not reveal: %q %v", name, got, err)
		}
	}
	if body := heldBody(t, reader, live.ID); body != "" {
		t.Errorf("a device that may not reveal kept a deleted guild message's body: %q", body)
	}

	synced := send("deleted by sync")
	for name, s := range map[string]*Service{"mod": mod, "reader": reader} {
		serveSyncRows(t, ctx, owner, s, g.ID, ch, tombstoneOf(synced))
		if row, ok, _ := s.store.MessageByID(synced.ID); !ok || !row.Deleted {
			t.Fatalf("the %s's device did not take the owner's synced tombstone", name)
		}
	}
	for name, s := range map[string]*Service{"mod": mod, "reader": reader} {
		if body := heldBody(t, s, synced.ID); body != "" {
			t.Errorf("the %s's device kept a synced tombstone's body: %q", name, body)
		}
	}
	if got, err := mod.RevealDeleted(ch, synced.ID); err == nil {
		t.Errorf("the moderator revealed a body a synced tombstone erased: %q", got)
	}

	// The reader becomes a moderator: what was erased stays erased.
	if err := owner.AssignRole(g.ID, reader.Fingerprint(), modRole, true); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	waitFor(t, 60*time.Second, func() bool { return reader.hasPerm(g.ID, PermManageMessages) },
		"the reader never became a moderator")
	if got, err := reader.RevealDeleted(ch, live.ID); err == nil {
		t.Errorf("a device that erased the body revealed %q", got)
	}
}

// An archive's tombstone is restored as a tombstone, never as a live empty
// message, and with no pin, reaction or bookmark.
func TestAnArchivedTombstoneRestoresAsATombstone(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Night Owls")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID
	kept, err := svc.SendMessage(ch, "kept", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	gone, err := svc.SendMessage(ch, "gone", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if err := svc.DeleteMessage(ch, gone.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	sealed, _, err := svc.ExportArchive("archive-pass", false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := svc.store.PruneMessagesBefore([]string{ch}, 1<<62); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if _, err := svc.ImportArchive(sealed, "archive-pass"); err != nil {
		t.Fatalf("import: %v", err)
	}
	if row, ok, _ := svc.store.MessageByID(kept.ID); !ok || row.Deleted || row.Content != "kept" {
		t.Fatalf("a live message did not come back live: %v %+v", ok, row)
	}
	row, ok, err := svc.store.MessageByID(gone.ID)
	if err != nil || !ok {
		t.Fatalf("the tombstone did not come back: %v %v", err, ok)
	}
	if !row.Deleted || row.Content != "" || row.Pinned || len(row.Reactions) != 0 {
		t.Errorf("an archived tombstone came back as %+v", row)
	}
	if body := heldBody(t, svc, gone.ID); body != "" {
		t.Errorf("an archived tombstone came back with a body: %q", body)
	}
}

// A guild channel's retention cutoff is a per-row skip for history sync: a
// row sent before it is not taken back, the rest of the payload is, and the
// channel's cursor is not held.
func TestHistorySyncSkipsRowsPastRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newTwoGuilds(t, ctx) // a owns ga

	if err := w.a.SetRetention(w.ga.ID, "", 3600); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool { return w.b.RetentionSeconds(w.ga.ID, w.chA) == 3600 },
		"the retention policy never reached b")

	old := w.signedByA(w.chA, "", "", "older than the policy")
	old.Sent = time.Now().Add(-2 * time.Hour).UTC()
	old.Sig = w.a.signMessage(old)
	recent := w.signedByA(w.chA, "", "", "inside the policy")

	raw, err := json.Marshal(syncPayload{Guild: w.ga.Clone(), Messages: map[string][]domain.Message{w.chA: {old, recent}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ct, err := w.a.mls.Encrypt(ctx, w.groupA, raw)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ok, _, failed := w.b.applySyncPayloadFrom(w.ga.ID, w.groupA, ct, w.a.Fingerprint())
	if !ok {
		t.Fatal("b could not read the payload")
	}
	if failed[w.chA] {
		t.Error("a row past retention held the channel's cursor")
	}
	if _, held, _ := w.b.store.MessageByID(old.ID); held {
		t.Error("history sync took back a row past the channel's retention")
	}
	if _, held, _ := w.b.store.MessageByID(recent.ID); !held {
		t.Error("a row inside the policy was not taken")
	}
}

// Archive import does not bring back a guild row past its channel's
// retention.
func TestArchiveImportSkipsRowsPastRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := startServiceInDir(t, ctx, t.TempDir())
	g, err := svc.CreateGuild("Night Owls")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID
	old, _ := domain.NewMessage(ch, svc.PublicKey(), "older than the policy")
	old.Sent = time.Now().Add(-2 * time.Hour).UTC()
	if _, err := svc.store.SaveMessage(old); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	recent, err := svc.SendMessage(ch, "inside the policy", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	sealed, _, err := svc.ExportArchive("archive-pass", false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if err := svc.SetRetention(g.ID, "", 3600); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if _, err := svc.store.PruneMessagesBefore([]string{ch}, 1<<62); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if _, err := svc.ImportArchive(sealed, "archive-pass"); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, held, _ := svc.store.MessageByID(old.ID); held {
		t.Error("archive import brought back a row past the channel's retention")
	}
	if _, held, _ := svc.store.MessageByID(recent.ID); !held {
		t.Error("archive import skipped a row inside the policy")
	}
}

// A reveal answers in no direct message, one to one or with several people,
// not even for its creator and not even for a deleted body still held at rest.
func TestARevealAnswersInNoDirectMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	others := []string{"fpr-reveal-dm-bob-000000", "fpr-reveal-dm-cara-00000"}
	for _, f := range others {
		if err := s.VerifyFingerprint(f); err != nil {
			t.Fatal(err)
		}
	}
	gdm, err := s.CreateGroupDM(others)
	if err != nil {
		t.Fatal(err)
	}
	ch := gdm.Channels[0].ID
	if !s.hasPerm(gdm.ID, PermManageMessages) {
		t.Fatal("the group's creator should hold every permission there")
	}
	for _, kind := range []string{"dm", "group"} {
		s.mu.Lock()
		s.guilds[gdm.ID].Kind = kind
		s.mu.Unlock()
		// A body still held at rest by a deleted row, as a build that kept
		// deleted bodies left it.
		m, err := domain.NewMessage(ch, []byte("bob-key-"+kind), "words of "+kind)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.store.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := s.store.MarkDeleted(m.ID, m.Sender, true); err != nil || !ok {
			t.Fatalf("MarkDeleted: %v %v", ok, err)
		}
		if body := heldBody(t, s, m.ID); body != "words of "+kind {
			t.Fatalf("setup: the deleted body is not at rest: %q", body)
		}
		if got, err := s.RevealDeleted(ch, m.ID); err == nil {
			t.Errorf("kind %q: the creator revealed a deleted direct message: %q", kind, got)
		}
	}
}
