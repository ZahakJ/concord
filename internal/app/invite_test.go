package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

func TestJoinSystemLineNamesTheRoom(t *testing.T) {
	if got := joinSystemLine(domain.Guild{}); got != "has joined the guild" {
		t.Fatalf("guild: %q", got)
	}
	if got := joinSystemLine(domain.Guild{Kind: "meeting"}); got != "has joined the meeting" {
		t.Fatalf("meeting: %q", got)
	}
	if got := joinSystemLine(domain.Guild{Kind: "dm"}); got != "" {
		t.Fatalf("dm should stay quiet, got %q", got)
	}
}

func inviteOpIn(msgs []domain.Message, op string) bool {
	for _, m := range msgs {
		if m.Kind != "invite" {
			continue
		}
		var n inviteNote
		if json.Unmarshal([]byte(m.Content), &n) == nil && n.Op == op {
			return true
		}
	}
	return false
}

func TestInviteStaysOffTheRosterUntilAccepted(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	if err := a.SetProfile(Profile{Name: "Alice"}); err != nil {
		t.Fatalf("Alice profile: %v", err)
	}
	if err := b.SetProfile(Profile{Name: "Bob"}); err != nil {
		t.Fatalf("Bob profile: %v", err)
	}
	connectHosts(t, ctx, a, b)
	if err := a.VerifyFingerprint(b.Fingerprint()); err != nil {
		t.Fatalf("Alice verify Bob: %v", err)
	}
	if err := b.VerifyFingerprint(a.Fingerprint()); err != nil {
		t.Fatalf("Bob verify Alice: %v", err)
	}

	g, err := a.CreateGuild("Dar al-Hikma")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	if err := a.AddMember(g.ID, b.Fingerprint()); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if a.guildHasMember(g.ID, b.Fingerprint()) {
		t.Fatal("the invitee was a member before they accepted")
	}
	found := false
	for _, f := range a.PendingMembersFor(g.ID) {
		if f == b.Fingerprint() {
			found = true
		}
	}
	if !found {
		t.Fatal("the invite was not remembered, so it could never be re-pushed")
	}

	waitUntil(t, 20*time.Second, func() bool {
		for _, dg := range a.Guilds() {
			if dg.Kind != "dm" || len(dg.Channels) == 0 {
				continue
			}
			msgs, err := a.Messages(dg.Channels[0].ID, 40)
			if err == nil && inviteOpIn(msgs, "offered") {
				return true
			}
		}
		return false
	}, "the 1:1 never recorded the invite")

	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)
	if left := a.PendingMembersFor(g.ID); len(left) != 0 {
		t.Fatalf("pending still held %v after they joined", left)
	}

	waitUntil(t, 20*time.Second, func() bool {
		msgs, err := a.Messages(g.Channels[0].ID, 40)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if m.Kind == "system" && strings.Contains(m.Content, "has joined the guild") {
				return true
			}
		}
		return false
	}, "the guild chat never said they had joined")

	waitUntil(t, 20*time.Second, func() bool {
		for _, dg := range a.Guilds() {
			if dg.Kind != "dm" || len(dg.Channels) == 0 {
				continue
			}
			msgs, err := a.Messages(dg.Channels[0].ID, 40)
			if err == nil && inviteOpIn(msgs, "joined") {
				return true
			}
		}
		return false
	}, "the 1:1 never recorded that they joined")
}
