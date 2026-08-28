package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// publishPastTheComposer encrypts and publishes a message WITHOUT any of the
// send-side checks, which is what a client with the restraint edited out looks
// like from the outside. Every gate this batch adds is asserted against this
// rather than against SendMessage, because a rule the sender enforces on itself
// is not a rule in a network where the sender owns the machine.
func publishPastTheComposer(t *testing.T, s *Service, channelID, content string) {
	t.Helper()
	s.mu.RLock()
	guildID, ok := s.channelToGuild[channelID]
	var groupID []byte
	if ok {
		groupID = s.guilds[guildID].GroupID
	}
	s.mu.RUnlock()
	if !ok {
		t.Fatalf("unknown channel %s", channelID)
	}
	msg, err := domain.NewMessage(channelID, s.PublicKey(), content)
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Name = s.DisplayName()
	msg.Sig = s.signMessage(msg)
	payload, _ := json.Marshal(msg)
	ct, err := s.mls.Encrypt(s.ctx, groupID, payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := s.ps.Publish(s.ctx, domain.TopicID(groupID, channelID), ct); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

// TestAnnouncementChannelIsEnforcedOnReceive: the channel type used to be an
// icon. Client-side composer gating is not enforcement in a network with no
// server — the composer runs on the machine of the person being restrained — so
// the assertion is about what the OTHER peer renders.
func TestAnnouncementChannelIsEnforcedOnReceive(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	member := startService(t, ctx)
	seen := &recorder{}
	owner.OnMessage(seen.add)

	g, err := owner.CreateGuild("Dar al-Hikma")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, owner, member)

	announce, err := owner.CreateChannel(g.ID, "announcements", "announcement", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	general := g.Channels[0].ID
	waitUntil(t, 20*time.Second, func() bool {
		for _, c := range member.Guilds() {
			if c.ID != g.ID {
				continue
			}
			for _, ch := range c.Channels {
				if ch.ID == announce.ID {
					return true
				}
			}
		}
		return false
	}, "the announcement channel never reached the member")

	// The owner may post there.
	sendUntilReceived(t, owner, announce.ID, "the hall opens at seven", seen)

	// An ordinary member may not — refused on their own side for honesty, and,
	// which is the part that matters, dropped on the owner's side even when the
	// refusal is bypassed by calling straight past the composer.
	if _, err := member.SendMessage(announce.ID, "hi everyone", "", ""); err == nil {
		t.Fatal("the send side let an ordinary member post in an announcement channel")
	}
	publishPastTheComposer(t, member, announce.ID, "hi everyone")
	time.Sleep(3 * time.Second)
	if seen.has("hi everyone") {
		t.Fatal("an ordinary member's post in an announcement channel was delivered")
	}

	// And the ordinary channel next door is unaffected.
	sendUntilReceived(t, member, general, "still talking here", seen)
}
