package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func (f *fakeClearer) ReleaseErasedBlobs() ([]string, error) { return nil, nil }

// An error on the way does not record the run, so it runs again at the next
// launch; a run that finishes is recorded and not repeated.
func TestDeletedBodiesClearIsRecordedOnlyWhenItFinishes(t *testing.T) {
	failing := errors.New("disk I/O error")

	f := &fakeClearer{getErr: failing, settings: map[string]string{}}
	clearDeletedBodiesOnceIn(f, func() []string { return nil })
	if f.clears != 0 || f.settings[deletedBodiesClearedKey] != "" {
		t.Fatalf("an unreadable record: cleared %d, recorded %q", f.clears, f.settings[deletedBodiesClearedKey])
	}

	f = &fakeClearer{clearErr: failing, settings: map[string]string{}}
	clearDeletedBodiesOnceIn(f, func() []string { return nil })
	if f.settings[deletedBodiesClearedKey] != "" {
		t.Fatal("a clear that failed was recorded as done")
	}
	f.clearErr = nil
	clearDeletedBodiesOnceIn(f, func() []string { return nil })
	if f.clears != 2 || f.settings[deletedBodiesClearedKey] == "" {
		t.Fatalf("the next launch: cleared %d times, recorded %q", f.clears, f.settings[deletedBodiesClearedKey])
	}
	clearDeletedBodiesOnceIn(f, func() []string { return nil })
	if f.clears != 2 {
		t.Fatalf("a recorded run ran again: %d clears", f.clears)
	}
}

// The one-time clear takes the blobs of the bodies it erases: a blob only the
// erased body named is removed, banned and not served, while a blob a live
// message names stays served. A cached blob that no held message names is not
// served either.
func TestTheOneTimeClearTakesTheErasedBodiesBlobs(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g := guildWith(t, owner, member)
	ch := g.Channels[0].ID
	waitFor(t, 10*time.Second, func() bool {
		v, _ := member.store.GetSetting(deletedBodiesClearedKey)
		return v != ""
	}, "the launch run was not recorded")
	if err := member.store.DeleteSetting(deletedBodiesClearedKey); err != nil {
		t.Fatal(err)
	}

	gone, kept, orphan := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	for _, id := range []string{gone, kept, orphan} {
		if err := member.store.SaveAttachment(id, []byte("ciphertext of "+id[:4])); err != nil {
			t.Fatal(err)
		}
	}
	m, err := domain.NewMessage(ch, owner.id.PublicKey(), "deleted "+testBlobToken(gone)+" "+testBlobToken(kept))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.store.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	live, err := domain.NewMessage(ch, owner.id.PublicKey(), "live "+testBlobToken(kept))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.store.SaveMessage(live); err != nil {
		t.Fatal(err)
	}
	// Deleted with its body kept, as a moderator's device or an older build
	// left it.
	if _, ok, err := member.store.MarkDeleted(m.ID, nil, true); err != nil || !ok {
		t.Fatalf("MarkDeleted: %v %v", ok, err)
	}
	if serves(t, member, orphan) {
		t.Error("a cached blob no held message names was served")
	}

	member.clearDeletedBodiesOnce()

	if body, _ := member.store.MessageContent(m.ID); body != "" {
		t.Fatalf("the deleted body was kept: %q", body)
	}
	if _, held, _ := member.store.GetAttachment(gone); held || !member.store.BlobBanned(gone) || serves(t, member, gone) {
		t.Error("the blob of a body the clear erased was kept")
	}
	if !serves(t, member, kept) || member.store.BlobBanned(kept) {
		t.Error("a blob a live message names went with the erased body")
	}
	if v, _ := member.store.GetSetting(deletedBodiesClearedKey); v == "" {
		t.Fatal("the run was not recorded")
	}
}

// The clear runs once more after an upgrade, under its own record. On a
// device that ran the first build's clear, whose later guild deletes kept
// their bodies on every device, it clears a member's body and its picture and
// keeps the body in a guild where the device may reveal, with its picture
// held. On a device that never ran it, it clears every deleted body,
// moderator or not, with their pictures.
func TestTheClearRunsAgainAfterAnUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	for _, firstClear := range []bool{true, false} {
		t.Run(fmt.Sprintf("firstClear=%v", firstClear), func(t *testing.T) {
			clearAfterUpgrade(t, firstClear)
		})
	}
}

func clearAfterUpgrade(t *testing.T, firstClear bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g := guildWith(t, owner, member)
	ch := g.Channels[0].ID
	waitFor(t, 10*time.Second, func() bool {
		v, _ := member.store.GetSetting(deletedBodiesClearedKey)
		return v != ""
	}, "the launch run was not recorded")
	if err := member.store.DeleteSetting(deletedBodiesClearedKey); err != nil {
		t.Fatal(err)
	}
	if firstClear {
		if err := member.store.SetSetting(deletedBodiesClearedV1Key, "1"); err != nil {
			t.Fatal(err)
		}
	}
	own, err := member.CreateGuild("own") // where the member may reveal
	if err != nil {
		t.Fatal(err)
	}
	deletedWithPicture := func(channelID string, pic byte, text string) (string, string) {
		id := strings.Repeat(string(pic), 64)
		if err := member.store.SaveAttachment(id, []byte("ciphertext of "+text)); err != nil {
			t.Fatal(err)
		}
		m, err := domain.NewMessage(channelID, owner.id.PublicKey(), text+" "+testBlobToken(id))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.store.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := member.store.MarkDeleted(m.ID, nil, true); err != nil || !ok {
			t.Fatalf("MarkDeleted: %v %v", ok, err)
		}
		return m.ID, id
	}
	memberMsg, memberPic := deletedWithPicture(ch, 'd', "a member's")
	modMsg, modPic := deletedWithPicture(own.Channels[0].ID, 'e', "a moderator's")

	member.clearDeletedBodiesOnce()

	if body, _ := member.store.MessageContent(memberMsg); body != "" {
		t.Errorf("a member's deleted body was kept: %q", body)
	}
	if serves(t, member, memberPic) || !member.store.BlobBanned(memberPic) {
		t.Error("the picture of a member's deleted body is still served or not banned")
	}
	body, _ := member.store.MessageContent(modMsg)
	_, held, _ := member.store.GetAttachment(modPic)
	if firstClear {
		if !strings.HasPrefix(body, "a moderator's") || !held {
			t.Errorf("a body the device may reveal, or its picture, was cleared: %q held %v", body, held)
		}
	} else {
		if body != "" || held || !member.store.BlobBanned(modPic) || serves(t, member, modPic) {
			t.Errorf("without the first clear, a moderator's body or picture was kept: %q held %v", body, held)
		}
	}
	if v, _ := member.store.GetSetting(deletedBodiesClearedKey); v == "" {
		t.Fatal("the run was not recorded")
	}
}
