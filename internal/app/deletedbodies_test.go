package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// The one-time clear of deleted bodies at rest: a device clears every one it
// holds, in a guild where it is no moderator and in one where it is, records
// the run, and does not run again.
func TestDeletedBodiesAtRestAreClearedOnce(t *testing.T) {
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
	mine, err := member.SendMessage(ownCh, "deleted by a moderator", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := member.DeleteMessage(ownCh, mine.ID); err != nil {
		t.Fatal(err)
	}

	member.clearDeletedBodiesOnce()

	if body, _ := member.store.MessageContent(m.ID); body != "" {
		t.Errorf("a deleted body in a guild without moderator rights was kept: %q", body)
	}
	if body, _ := member.store.MessageContent(mine.ID); body != "" {
		t.Errorf("a deleted body in the moderator's own guild was kept: %q", body)
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

// fakeClearer stands in for the store in the one-time clear: each call can be
// made to fail, and it records what was cleared and set.
type fakeClearer struct {
	getErr, clearErr error
	settings         map[string]string
	clears           int
}

func (f *fakeClearer) GetSetting(k string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.settings[k], nil
}

func (f *fakeClearer) SetSetting(k, v string) error {
	f.settings[k] = v
	return nil
}

func (f *fakeClearer) ClearDeletedBodies([]string) (int, error) {
	f.clears++
	return 0, f.clearErr
}

// An error on the way does not record the run, so it runs again at the next
// launch; a run that finishes is recorded and not repeated.
func TestDeletedBodiesClearIsRecordedOnlyWhenItFinishes(t *testing.T) {
	failing := errors.New("disk I/O error")

	f := &fakeClearer{getErr: failing, settings: map[string]string{}}
	clearDeletedBodiesOnceIn(f)
	if f.clears != 0 || f.settings[deletedBodiesClearedKey] != "" {
		t.Fatalf("an unreadable record: cleared %d, recorded %q", f.clears, f.settings[deletedBodiesClearedKey])
	}

	f = &fakeClearer{clearErr: failing, settings: map[string]string{}}
	clearDeletedBodiesOnceIn(f)
	if f.settings[deletedBodiesClearedKey] != "" {
		t.Fatal("a clear that failed was recorded as done")
	}
	f.clearErr = nil
	clearDeletedBodiesOnceIn(f)
	if f.clears != 2 || f.settings[deletedBodiesClearedKey] == "" {
		t.Fatalf("the next launch: cleared %d times, recorded %q", f.clears, f.settings[deletedBodiesClearedKey])
	}
	clearDeletedBodiesOnceIn(f)
	if f.clears != 2 {
		t.Fatalf("a recorded run ran again: %d clears", f.clears)
	}
}
