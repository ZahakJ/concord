package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/domain"
)

// await reads lines until one has the given type and satisfies pred, keeping
// every line it passes over in seen.
func (h *botHarness) await(typ string, timeout time.Duration, seen *[]map[string]any, pred func(map[string]any) bool) map[string]any {
	h.t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case v, ok := <-h.lines:
			if !ok {
				h.t.Fatalf("bot stdout closed waiting for %q", typ)
			}
			*seen = append(*seen, v)
			if v["type"] == typ && (pred == nil || pred(v)) {
				return v
			}
		case <-deadline:
			h.t.Fatalf("no %q line within %s; saw %v", typ, timeout, *seen)
			return nil
		}
	}
}

// TestBotEndToEnd drives bot mode against a real second peer: the bot joins a
// guild by invite through the protocol, hears a person's message, answers it
// as a reply, and never hears itself.
func TestBotEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := func() *app.Service {
		svc, err := app.Start(ctx, app.Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	alice, botSvc := start(), start()
	if err := alice.SetDisplayName("Alice"); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var heard []domain.Message
	alice.OnMessage(func(m domain.Message) {
		mu.Lock()
		heard = append(heard, m)
		mu.Unlock()
	})
	aliceHeard := func(content string) (domain.Message, bool) {
		mu.Lock()
		defer mu.Unlock()
		for _, m := range heard {
			if m.Content == content {
				return m, true
			}
		}
		return domain.Message{}, false
	}

	g, err := alice.CreateGuild("Tavern")
	if err != nil {
		t.Fatal(err)
	}
	general := g.Channels[0]
	code, err := alice.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}

	h := startBot(t, ctx, botSvc)
	var seen []map[string]any
	ready := h.await("ready", 10*time.Second, &seen, nil)
	if ready["fingerprint"] != botSvc.Fingerprint() {
		t.Fatalf("ready = %v", ready)
	}

	h.send(fmt.Sprintf(`{"cmd":"join","invite":%q,"id":"j"}`, code))
	joined := h.await("joined", 60*time.Second, &seen, nil)
	if joined["id"] != "j" || joined["guild"].(map[string]any)["id"] != g.ID {
		t.Fatalf("joined = %v", joined)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		n, _ := alice.MemberCount(g.ID)
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("guild did not reach 2 members (have %d)", n)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// A person speaks; retry while the mesh warms, as a client would.
	var ping map[string]any
	for i := 0; ping == nil; i++ {
		if i == 30 {
			t.Fatalf("bot never heard !ping; saw %v", seen)
		}
		if _, err := alice.SendMessage(general.ID, "!ping", "", ""); err != nil {
			t.Fatal(err)
		}
		wait := time.After(time.Second)
	drain:
		for {
			select {
			case v := <-h.lines:
				seen = append(seen, v)
				if v["type"] == "message" && v["content"] == "!ping" {
					ping = v
					break drain
				}
			case <-wait:
				break drain
			}
		}
	}
	for k, want := range map[string]any{"guildId": g.ID, "guild": "Tavern", "channelId": general.ID,
		"channel": general.Name, "sender": alice.Fingerprint(), "senderName": "Alice"} {
		if ping[k] != want {
			t.Errorf("message[%s] = %v, want %v", k, ping[k], want)
		}
	}

	// The bot answers by name, as a reply; the person receives it threaded.
	pingID := ping["messageId"].(string)
	var reply domain.Message
	for i := 0; ; i++ {
		if i == 10 {
			t.Fatal("alice never received the bot's pong")
		}
		h.send(fmt.Sprintf(`{"cmd":"send","channel":"Tavern/#%s","text":"pong","replyTo":%q,"id":%d}`, general.Name, pingID, i))
		h.await("sent", 10*time.Second, &seen, func(v map[string]any) bool { return v["id"] == float64(i) })
		got := false
		for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if reply, got = aliceHeard("pong"); got {
				break
			}
		}
		if got {
			break
		}
	}
	if reply.ReplyTo != pingID {
		t.Fatalf("pong replyTo = %q, want %q", reply.ReplyTo, pingID)
	}

	h.send(`{"cmd":"members","guild":"Tavern","id":"m"}`)
	mem := h.await("members", 10*time.Second, &seen, nil)
	if n := len(mem["members"].([]any)); n != 2 {
		t.Fatalf("members = %v", mem)
	}

	// Membership: the bot reported its own arrival as a guild, and reports a
	// person arriving after it as a member.
	sawGuild := false
	for _, v := range seen {
		if v["type"] == "guild" && v["action"] == "join" && v["guildId"] == g.ID {
			sawGuild = true
		}
	}
	if !sawGuild {
		h.await("guild", 10*time.Second, &seen, func(v map[string]any) bool { return v["guildId"] == g.ID })
	}
	bob := start()
	if _, err := bob.JoinViaInvite(code); err != nil {
		t.Fatalf("bob join: %v", err)
	}
	h.await("member", 60*time.Second, &seen, func(v map[string]any) bool {
		return v["action"] == "join" && v["fingerprint"] == bob.Fingerprint()
	})

	h.send(`{"cmd":"quit"}`)
	h.await("ok", 10*time.Second, &seen, func(v map[string]any) bool { return v["cmd"] == "quit" })
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("bot did not stop on quit")
	}
	for v := range h.lines {
		seen = append(seen, v)
	}
	for _, v := range seen {
		if v["type"] == "message" && v["sender"] == botSvc.Fingerprint() {
			t.Fatalf("the bot heard its own message: %v", v)
		}
		if v["type"] == "error" {
			t.Fatalf("unexpected error line: %v", v)
		}
	}
}
