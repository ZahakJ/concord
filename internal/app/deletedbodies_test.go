package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// The one-time clear of deleted bodies at rest: a device clears them in a
// guild where it cannot reveal them, keeps the moderator's reveal in a guild
// where it can, records the run, and does not run again.
func TestDeletedBodiesAtRestAreClearedOnceWhereNotRevealable(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g := guildWith(t, owner, member)
	ch := g.Channels[0].ID

	// The launch already ran it once, on a store with nothing to clear.
	waitFor(t, 10*time.Second, func() bool {
		v, _ := member.store.GetSetting(deletedBodiesClearedKey)
		return v != ""
	}, "the launch run was not recorded")
	if err := member.store.DeleteSetting(deletedBodiesClearedKey); err != nil {
		t.Fatal(err)
	}

	// A deleted body in the owner's guild, where the member is no moderator.
	m, err := domain.NewMessage(ch, owner.id.PublicKey(), "not the member's to reveal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.store.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := member.store.MarkDeleted(m.ID, nil, true); err != nil || !ok {
		t.Fatalf("MarkDeleted: %v %v", ok, err)
	}
	// The member's own guild, where it holds ManageMessages and deletes a message.
	own, err := member.CreateGuild("own")
	if err != nil {
		t.Fatal(err)
	}
	ownCh := own.Channels[0].ID
	mine, err := member.SendMessage(ownCh, "the moderator's to reveal", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := member.DeleteMessage(ownCh, mine.ID); err != nil {
		t.Fatal(err)
	}

	member.clearDeletedBodiesOnce()

	if body, _ := member.store.MessageContent(m.ID); body != "" {
		t.Errorf("a deleted body this device cannot reveal was kept: %q", body)
	}
	if got, err := member.RevealDeleted(ownCh, mine.ID); err != nil || got != "the moderator's to reveal" {
		t.Errorf("the moderator's reveal: %q %v", got, err)
	}
	if v, _ := member.store.GetSetting(deletedBodiesClearedKey); v == "" {
		t.Fatal("the run was not recorded")
	}

	// Recorded: a later deleted body is not touched by another call.
	again, err := domain.NewMessage(ch, owner.id.PublicKey(), "after the run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.store.SaveMessage(again); err != nil {
		t.Fatal(err)
	}
	if _, _, err := member.store.MarkDeleted(again.ID, nil, true); err != nil {
		t.Fatal(err)
	}
	member.clearDeletedBodiesOnce()
	if body, _ := member.store.MessageContent(again.ID); body != "after the run" {
		t.Errorf("the clear ran a second time: %q", body)
	}
}
