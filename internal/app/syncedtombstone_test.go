package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// serveSyncRows hands `to` one catch-up payload for a channel, encrypted by
// `from` in the guild's group, as the sync lane would deliver it.
func serveSyncRows(t *testing.T, ctx context.Context, from, to *Service, guildID, ch string, rows ...domain.Message) {
	t.Helper()
	to.mu.RLock()
	g := to.guilds[guildID]
	to.mu.RUnlock()
	raw, err := json.Marshal(syncPayload{Guild: g.Clone(), Messages: map[string][]domain.Message{ch: rows}})
	if err != nil {
		t.Fatal(err)
	}
	ct, err := from.mls.Encrypt(ctx, g.GroupID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := to.applySyncPayloadFrom(guildID, g.GroupID, ct, from.Fingerprint()); !ok {
		t.Fatal("sync payload not read")
	}
}

// A member serves a tombstone that carries a body, for a message this device
// does not hold. The device keeps the tombstone and no body, so a moderator's
// "Show original" has nothing to show.
func TestATombstoneServedWithABodyIsKeptWithoutIt(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g := guildWith(t, owner, member)
	ch := g.Channels[0].ID

	m, err := domain.NewMessage(ch, member.id.PublicKey(), "a body in a tombstone")
	if err != nil {
		t.Fatal(err)
	}
	m.Deleted = true
	m.Updated = time.Now().UTC()
	serveSyncRows(t, ctx, member, owner, g.ID, ch, m)

	row, held, err := owner.store.MessageByID(m.ID)
	if err != nil || !held || !row.Deleted {
		t.Fatalf("tombstone held=%v deleted=%v err=%v", held, row.Deleted, err)
	}
	if body, err := owner.store.MessageContent(m.ID); err != nil || body != "" {
		t.Errorf("the tombstone was stored with the body %q (err %v)", body, err)
	}
	if got, _ := owner.RevealDeleted(ch, m.ID); got != "" {
		t.Errorf("reveal answered %q", got)
	}
}

// The owner serves a tombstone for a message the member holds live. The
// delete applies, and the body held at rest goes with it.
func TestATombstoneServedForAHeldMessageTakesItsBody(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g := guildWith(t, owner, member)
	ch := g.Channels[0].ID

	m, err := domain.NewMessage(ch, owner.id.PublicKey(), "words held live")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.store.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	tomb := m
	tomb.Deleted = true
	tomb.Updated = time.Now().UTC()
	serveSyncRows(t, ctx, owner, member, g.ID, ch, tomb)

	row, held, err := member.store.MessageByID(m.ID)
	if err != nil || !held || !row.Deleted {
		t.Fatalf("delete not applied: held=%v deleted=%v err=%v", held, row.Deleted, err)
	}
	if body, err := member.store.MessageContent(m.ID); err != nil || body != "" {
		t.Errorf("the held body survived the delete: %q (err %v)", body, err)
	}
}
