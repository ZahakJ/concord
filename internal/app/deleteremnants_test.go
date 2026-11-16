package app

import (
	"context"
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
