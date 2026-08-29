package app

import (
	"context"
	"testing"
)

// A guild's pinned strip is shared state every member sees, and the permission
// bit has always been documented as "delete anyone's messages and pin". Nothing
// enforced the second half: PinMessage broadcast without asking, and applyPin
// toggled whatever arrived, so any member could pin — or silently UNPIN a
// moderator's pin — for the whole guild.
//
// The receive side is the half that matters, because it is the only one a
// patched client cannot route around: the pin's author is the MLS-authenticated
// sender of the frame, so every device re-decides for itself.
func TestPinningInAGuildNeedsManageMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startServiceInDir(t, ctx, t.TempDir())
	stranger := startServiceInDir(t, ctx, t.TempDir())

	g, err := owner.CreateGuild("Pinboard")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID
	m, err := owner.SendMessage(ch, "worth keeping", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	pinned := func() bool {
		got, ok, err := owner.store.MessageByID(m.ID)
		if err != nil || !ok {
			t.Fatalf("MessageByID: %v (found=%v)", err, ok)
		}
		return got.Pinned
	}

	// A member with no roles holds nothing, so their pin must not land here.
	owner.applyPin(m.ID, stranger.PublicKey(), ch)
	if pinned() {
		t.Fatal("a member without Manage messages pinned a guild message on someone else's device")
	}

	// The owner holds every bit, so theirs does.
	owner.applyPin(m.ID, owner.PublicKey(), ch)
	if !pinned() {
		t.Fatal("the owner's own pin was refused")
	}

	// ...and the same member must not be able to UNPIN it either, which was the
	// worse half: undoing a moderator's decision needs no permission at all.
	owner.applyPin(m.ID, stranger.PublicKey(), ch)
	if !pinned() {
		t.Fatal("a member without Manage messages unpinned a moderator's pin")
	}

	// The send side refuses before it broadcasts, so the UI gets a reason
	// rather than a pin that appears locally and nowhere else.
	if !owner.mayPin(g.ID, owner.Fingerprint()) {
		t.Fatal("the owner may not pin in their own guild")
	}
	if owner.mayPin(g.ID, stranger.Fingerprint()) {
		t.Fatal("mayPin let a roleless member pin in a guild")
	}
	// A conversation with no roles in it — a DM, a group, a meeting — has
	// nobody to grant the bit, so everyone in one may still pin.
	if !owner.mayPin("", stranger.Fingerprint()) {
		t.Fatal("mayPin gated a channel that belongs to no guild")
	}
}
