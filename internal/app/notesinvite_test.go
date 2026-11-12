package app

import (
	"context"
	"testing"
	"time"
)

// TestNotesCannotBeSharedByInvite pins Notes staying private: a shareable code
// is refused for it, and a stranger presenting a raw code for it is refused at
// the door, so the owner keeps their Notes and nobody reads them.
func TestNotesCannotBeSharedByInvite(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	stranger := startService(t, ctx)
	var notesID string
	waitUntil(t, 10*time.Second, func() bool {
		for _, g := range owner.Guilds() {
			if isNotesGuild(g, owner.PublicKey()) {
				notesID = g.ID
				return true
			}
		}
		return false
	}, "the account has no Notes")
	if _, err := owner.ShareableInviteCode(notesID); err == nil {
		t.Fatal("a shareable invite code was minted for Notes")
	}
	raw, err := owner.InviteCode(notesID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := stranger.JoinViaInvite(raw); err == nil {
		t.Fatal("a stranger joined someone's Notes")
	}
	if n, _ := owner.MemberCount(notesID); n != 1 {
		t.Fatalf("Notes has %d members, want 1", n)
	}
}
