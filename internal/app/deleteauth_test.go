package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// A moderator's delete is judged against the roster and roles held when it is
// applied, not when it was written. A frame encrypted while its sender held
// Manage messages, and delivered after the sender was removed, changes nothing.
// The removal here is by a member who may remove members but not edit roles, so
// the removed member's role is still on the books.
func TestDeleteIsJudgedAgainstTheCurrentRoster(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	mod := startService(t, ctx)
	door := startService(t, ctx)
	reader := startService(t, ctx)
	g := guildWith(t, owner, mod, door, reader)
	ch := g.Channels[0].ID

	modRole, err := owner.UpsertRole(g.ID, "", "Moderator", "", PermManageMessages, 1)
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	doorRole, err := owner.UpsertRole(g.ID, "", "Doorkeeper", "", PermManageMembers, 2)
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := owner.AssignRole(g.ID, mod.Fingerprint(), modRole, true); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if err := owner.AssignRole(g.ID, door.Fingerprint(), doorRole, true); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	waitFor(t, 60*time.Second, func() bool {
		return reader.memberHasPerm(g.ID, mod.Fingerprint(), PermManageMessages) &&
			mod.hasPerm(g.ID, PermManageMessages) && door.hasPerm(g.ID, PermManageMembers)
	}, "the roles never reached every member")

	first, err := reader.SendMessage(ch, "first", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	second, err := reader.SendMessage(ch, "second", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		_, ok1, _ := mod.store.MessageByID(first.ID)
		_, ok2, _ := mod.store.MessageByID(second.ID)
		return ok1 && ok2
	}, "the moderator never received the reader's messages")

	// Written now, while the sender is a moderator; it reaches the reader after
	// the removal. MLS keeps secrets from past epochs so a late frame still
	// opens, and what it then hands the live lane is this message attributed to
	// the moderator's leaf, which is what is delivered below.
	held := domain.Message{
		ID: domain.NewID(), ChannelID: ch, Sender: mod.PublicKey(),
		Kind: "delete", ReplyTo: first.ID, Content: "deleted", Sent: time.Now().UTC(),
	}
	held.Sig = mod.signMessage(held)
	modLeaf := mod.myCredential

	// While a moderator, their delete applies.
	if err := mod.DeleteMessage(ch, second.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		row, ok, err := reader.store.MessageByID(second.ID)
		return err == nil && ok && row.Deleted
	}, "a moderator's delete did not apply")

	if err := door.KickMember(g.ID, mod.Fingerprint(), ""); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	waitFor(t, 60*time.Second, func() bool {
		return reader.isRemoved(g.ID, mod.Fingerprint()) && !reader.guildHasMember(g.ID, mod.Fingerprint())
	}, "the reader never saw the removal")

	reader.deliverMessage(g.ID, held, modLeaf)
	if row, ok, err := reader.store.MessageByID(first.ID); err != nil || !ok || row.Deleted {
		t.Fatalf("a delete from a removed member applied: err=%v found=%v deleted=%v", err, ok, row.Deleted)
	}
	if reader.memberHasPerm(g.ID, mod.Fingerprint(), PermManageMessages) {
		t.Error("a removed member still reads as holding Manage messages")
	}
}

// In a direct message only the author deletes a message for everyone. The
// member who opened the DM holds no moderator power over the other's messages.
func TestInADirectMessageOnlyTheAuthorDeletes(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	guildWith(t, a, b)
	dm, err := a.StartDM(b.Fingerprint()) // a opens the DM
	if err != nil {
		t.Fatalf("StartDM: %v", err)
	}
	ch := dm.Channels[0].ID
	waitFor(t, 60*time.Second, func() bool {
		g, ok := b.channelGuild(ch)
		return ok && g == dm.ID && b.guildHasMember(dm.ID, a.Fingerprint())
	}, "b never joined the DM")

	var theirs domain.Message
	waitFor(t, 30*time.Second, func() bool {
		if theirs.ID == "" {
			m, err := b.SendMessage(ch, "from b", "", "")
			if err != nil {
				return false
			}
			theirs = m
		}
		_, ok, _ := a.store.MessageByID(theirs.ID)
		return ok
	}, "a never received b's message")
	mine, err := a.SendMessage(ch, "from a", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		_, ok, _ := b.store.MessageByID(mine.ID)
		return ok
	}, "b never received a's message")

	// a deletes their own: it is gone on both sides.
	if err := a.DeleteMessage(ch, mine.ID); err != nil {
		t.Fatalf("DeleteMessage own: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		row, ok, err := b.store.MessageByID(mine.ID)
		return err == nil && ok && row.Deleted
	}, "the author's own delete did not apply")

	// a may not delete b's, by the API or by a frame sent anyway.
	if err := a.DeleteMessage(ch, theirs.ID); err == nil {
		t.Error("DeleteMessage accepted deleting the other side's message in a DM")
	}
	b.mu.RLock()
	groupID := b.guilds[dm.ID].GroupID
	b.mu.RUnlock()
	frame := domain.Message{
		ID: domain.NewID(), ChannelID: ch, Sender: a.PublicKey(),
		Kind: "delete", ReplyTo: theirs.ID, Content: "deleted", Sent: time.Now().UTC(),
	}
	frame.Sig = a.signMessage(frame)
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ct, err := a.mls.Encrypt(ctx, groupID, raw)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !b.deliverCiphertext(groupID, ct) {
		t.Fatal("b could not decrypt a frame a encrypted at the shared epoch")
	}
	for name, s := range map[string]*Service{"b": b, "a": a} {
		if row, ok, err := s.store.MessageByID(theirs.ID); err != nil || !ok || row.Deleted {
			t.Errorf("on %s, the DM's opener deleted the other side's message: err=%v found=%v deleted=%v", name, err, ok, row.Deleted)
		}
	}
}
