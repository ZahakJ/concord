package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// A heal repairs a guild the device already holds, so the welcome must be for
// that guild's MLS group. A committer that answers a heal with a welcome for
// another group of its own does not repair anything and, above all, leaves no
// state for that other group on the member's device. An honest answer still
// heals.
func TestHealRefusesAWelcomeForAnotherGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	member := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := owner.CreateGuild("Ours")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := owner.InviteCode(g.ID)
	waitUntil(t, 60*time.Second, func() bool { _, err := member.JoinViaInvite(code); return err == nil }, "join")
	trap, err := owner.CreateGuild("Trap")
	if err != nil {
		t.Fatal(err)
	}
	before, err := member.mls.Epoch(ctx, g.GroupID)
	if err != nil {
		t.Fatal(err)
	}

	// The committer answers the heal for "Ours" with a welcome for "Trap".
	owner.host.HandleInvites(func(c context.Context, from peer.ID, raw []byte) ([]byte, error) {
		var req inviteRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		req.GuildID, req.Heal = trap.ID, false
		swapped, _ := json.Marshal(req)
		return owner.handleInviteRequest(c, from, swapped)
	})
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(io.MultiWriter(&logged, prev))
	healed := member.healViaCommitter(g.ID, owner.host.PeerID())
	log.SetOutput(prev)
	if healed {
		t.Fatal("a heal answered with another group's welcome was taken as a repair")
	}
	if !strings.Contains(logged.String(), "answered a heal with a welcome for another group; refused") {
		t.Fatal("the refused heal left no log line")
	}
	if m, err := member.mls.Members(ctx, trap.GroupID); err == nil && len(m) > 0 {
		t.Fatalf("asked to be healed in %q; the device now holds the MLS group of the committer's other guild (%d members)", g.Name, len(m))
	}
	member.mu.RLock()
	_, holds := member.guilds[trap.ID]
	member.mu.RUnlock()
	if holds {
		t.Fatal("the member holds a guild it never joined")
	}
	if after, err := member.mls.Epoch(ctx, g.GroupID); err != nil || after != before {
		t.Fatalf("the guild's own group state changed: epoch %d -> %d, %v", before, after, err)
	}

	// An honest answer heals.
	owner.host.HandleInvites(owner.handleInviteRequest)
	waitUntil(t, 60*time.Second, func() bool { return member.healViaCommitter(g.ID, owner.host.PeerID()) }, "an honest heal no longer works")
	if after, err := member.mls.Epoch(ctx, g.GroupID); err != nil || after <= before {
		t.Fatalf("after an honest heal the guild is at epoch %d (was %d), %v", after, before, err)
	}
}
