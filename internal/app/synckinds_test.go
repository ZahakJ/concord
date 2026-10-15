package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// plantRow writes a message straight into a service's store, the way it would
// sit there after arriving while the other side was not listening. Nothing is
// published: the only road from here to another peer is history sync.
func plantRow(t *testing.T, s *Service, channelID, kind, content string, sent time.Time) domain.Message {
	t.Helper()
	m, err := domain.NewMessage(channelID, s.PublicKey(), content)
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	m.Kind = kind
	m.Sent = sent
	if _, err := s.store.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	return m
}

// TestSyncCarriesEveryStoredKind holds the two ends of history sync to one list
// of message kinds. The responder used to serve plain chat, "system" and "app"
// while the applier kept plain chat, "system" and "invite": a guest's relayed
// words, an invitation card and a missed-call line never travelled at all, and
// every leftover "app" row was shipped to a peer that threw it away.
func TestSyncCarriesEveryStoredKind(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("kinds")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)
	channel := g.Channels[0].ID

	at := time.Now().Add(time.Second)
	want := map[string]bool{"": true, "system": true, "guest": true, "invite": true, "call-missed": true, "app": false}
	ids := map[string]string{}
	for kind := range want {
		ids[kind] = plantRow(t, a, channel, kind, "kind:"+kind, at).ID
	}

	if err := b.syncGuildFromPeer(g.ID, a.host.PeerID()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	for kind, should := range want {
		_, ok, err := b.store.MessageByID(ids[kind])
		if err != nil {
			t.Fatalf("MessageByID: %v", err)
		}
		if ok != should {
			t.Errorf("kind %q: B holds it = %v after sync, want %v", kind, ok, should)
		}
	}
}
