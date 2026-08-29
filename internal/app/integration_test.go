package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// TestThreeNodeGuildE2EE is the Phase 2 acceptance test: three independent
// Services form a guild via the invite handshake and exchange end-to-end
// encrypted messages over gossipsub, with every message persisted. It exercises
// the whole stack — identity, libp2p, MLS group crypto, gossipsub, invite
// streams, and the encrypted store — through the same API the GUI uses.
func TestThreeNodeGuildE2EE(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	c := startService(t, ctx)

	ra, rb, rc := &recorder{}, &recorder{}, &recorder{}
	a.OnMessage(ra.add)
	b.OnMessage(rb.add)
	c.OnMessage(rc.add)

	// A creates a guild; B and C join via A's invite code.
	g, err := a.CreateGuild("test-guild")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}

	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	// Let the A-B gossip mesh warm before the next membership commit is
	// published, so B reliably receives it.
	waitMembers(t, 20*time.Second, 2, a, b)

	if _, err := c.JoinViaInvite(code); err != nil {
		t.Fatalf("C JoinViaInvite: %v", err)
	}

	// All three must converge to a 3-member epoch before messaging.
	waitMembers(t, 30*time.Second, 3, a, b, c)

	// Each peer sends; the other two must receive the plaintext. Sends are
	// retried to absorb gossipsub mesh/epoch timing.
	sendUntilReceived(t, a, channel, "hello-from-A", rb, rc)
	sendUntilReceived(t, b, channel, "hello-from-B", ra, rc)
	sendUntilReceived(t, c, channel, "hello-from-C", ra, rb)

	// History is persisted locally at B (its own + the two it received).
	msgs, err := b.Messages(channel, 0)
	if err != nil {
		t.Fatalf("B Messages: %v", err)
	}
	if len(msgs) < 3 {
		t.Fatalf("B persisted %d messages, want >= 3", len(msgs))
	}
}

// startService boots a Service in an isolated temp dir with LAN discovery off
// (connectivity comes from the invite handshake, keeping the test deterministic).
func startService(t *testing.T, ctx context.Context) *Service {
	t.Helper()
	svc, err := Start(ctx, Config{
		DataDir:     t.TempDir(),
		Passphrase:  "test-pass",
		DisableMDNS: true,
	})
	if err != nil {
		t.Fatalf("Start service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// recorder collects messages delivered to a Service's OnMessage callback.
type recorder struct {
	mu   sync.Mutex
	msgs []domain.Message
}

func (r *recorder) add(m domain.Message) {
	r.mu.Lock()
	r.msgs = append(r.msgs, m)
	r.mu.Unlock()
}

func (r *recorder) has(content string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.msgs {
		if m.Content == content {
			return true
		}
	}
	return false
}

// waitMembers blocks until every service reports want members in guild g, or
// fails after timeout.
// theGuild is the guild a test made.
//
// It used to be spelled `s.Guilds()[0]`, which was true only while an account
// held nothing it had not created. Every account now has a Notes self-DM from
// the moment it exists, and Notes is created before anything a test does, so
// index zero became "your scratchpad" and forty-seven tests spent twenty
// seconds each waiting for a one-member self-group to grow a second member.
//
// A DM is never the guild under test — the ones that exercise DMs reach for
// them by name — so the rule is simply "the first thing that is not one".
func theGuild(t *testing.T, s *Service) domain.Guild {
	t.Helper()
	for _, g := range s.Guilds() {
		if g.Kind == "" {
			return g
		}
	}
	t.Fatalf("this service holds no guild: %+v", s.Guilds())
	return domain.Guild{}
}

func waitMembers(t *testing.T, timeout time.Duration, want int, svcs ...*Service) {
	t.Helper()
	guildID := theGuild(t, svcs[0]).ID
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok := true
		for _, s := range svcs {
			n, _ := s.MemberCount(guildID)
			if n != want {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("group did not converge to %d members within %s", want, timeout)
}

// sendUntilReceived publishes content from sender, retrying until all recorders
// observe it, mirroring how a real client would eventually deliver through a
// warming mesh.
func sendUntilReceived(t *testing.T, sender *Service, channel, content string, receivers ...*recorder) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	first := true
	for time.Now().Before(deadline) {
		if first {
			if _, err := sender.SendMessage(channel, content, "", ""); err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			first = false
		}
		all := true
		for _, r := range receivers {
			if !r.has(content) {
				all = false
				break
			}
		}
		if all {
			return
		}
		// Resend periodically in case an early copy was dropped mid-warmup.
		if _, err := sender.SendMessage(channel, content, "", ""); err != nil {
			t.Fatalf("SendMessage (retry): %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("message %q was not received by all peers within timeout", content)
}
