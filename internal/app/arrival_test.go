package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// TestHistoryArrivesAsHistory pins the bug the phone showed: a device that
// joins a guild — or comes back after a week — takes its catch-up as news.
// Every synced row was emitted exactly like a live message, so the native
// notifier posted one tray line per historical DM, and with no read marks the
// inbox called every old mention unread and every channel counted its whole
// backlog.
//
// Three things must hold on the device that joined, and each fails without
// its half of the fix:
//   - every historical row reaches the UI with Backfill set (sync.go);
//   - a mention of the joiner written BEFORE the join is not unread in the
//     inbox (arrival.go's floor in inboxUnread);
//   - the channel's unread count for the backlog is zero (the same floor
//     under UnreadCounts).
//
// A message written AFTER the join is still unread by both measures, which
// is what keeps the floor from being a mute.
func TestHistoryArrivesAsHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	if err := b.SetProfile(Profile{Name: "Bilal"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	g, err := a.CreateGuild("hamadan")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch := g.Channels[0].ID

	// The backlog, written before anybody else exists: three rows, one of them
	// naming the member who has not joined yet.
	const backlog = 3
	for i, body := range []string{"first", "@Bilal come look at this", "third"} {
		if _, err := a.SendMessage(ch, body, "", ""); err != nil {
			t.Fatalf("SendMessage %d: %v", i, err)
		}
	}

	// B listens before joining, so no emit can slip past the recorder.
	var mu sync.Mutex
	seen := map[string]bool{} // id -> backfill
	b.OnMessage(func(m domain.Message) {
		mu.Lock()
		defer mu.Unlock()
		if m.ChannelID == ch && m.Kind == "" {
			seen[m.ID] = m.Backfill
		}
	})

	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)
	waitUntil(t, 60*time.Second, func() bool {
		msgs, err := b.Messages(ch, 50)
		if err != nil {
			return false
		}
		n := 0
		for _, m := range msgs {
			if m.Kind == "" {
				n++
			}
		}
		return n >= backlog
	}, "the backlog never reached the joiner")

	// Something said AFTER the join, delivered live, which must still be news.
	if _, err := a.SendMessage(ch, "@Bilal and this one is new", "", ""); err != nil {
		t.Fatalf("SendMessage live: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		msgs, err := b.Messages(ch, 50)
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if strings.Contains(m.Content, "is new") {
				return true
			}
		}
		return false
	}, "the live message never arrived")

	// 1. The backlog was emitted as backfill; the live row was not.
	msgs, err := b.Messages(ch, 50)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	mu.Lock()
	for _, m := range msgs {
		if m.Kind != "" {
			continue
		}
		bf, emitted := seen[m.ID]
		live := strings.Contains(m.Content, "is new")
		switch {
		case !emitted:
			t.Errorf("%q was stored on the joiner without an emit", m.Content)
		case live && bf:
			t.Errorf("the live message %q was emitted as backfill", m.Content)
		case !live && !bf:
			t.Errorf("historical %q was emitted as if just written (Backfill=false)", m.Content)
		}
	}
	mu.Unlock()

	// 2. The inbox: the old mention is there, read; the new one is unread.
	page, err := b.Inbox(nil, 0, 50, false)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	var oldSeen, newSeen bool
	for _, e := range page.Entries {
		if e.ChannelID != ch {
			continue
		}
		if strings.Contains(e.Snippet, "come look") {
			oldSeen = true
			if e.Unread {
				t.Errorf("a mention written before the join is unread in the inbox: %+v", e)
			}
		}
		if strings.Contains(e.Snippet, "is new") {
			newSeen = true
			if !e.Unread {
				t.Errorf("a mention written after the join is not unread: %+v", e)
			}
		}
	}
	if !oldSeen || !newSeen {
		t.Fatalf("inbox did not list both mentions (old=%v new=%v): %+v", oldSeen, newSeen, page.Entries)
	}

	// 3. The badge: with no read mark at all, only the live row counts.
	counts, err := b.UnreadCounts(map[string]int64{ch: 0})
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if counts[ch] != 1 {
		t.Errorf("unread count after joining = %d, want 1 (the live row only)", counts[ch])
	}

	// The floor is this device's alone: B's stamp governs B's inbox and badge,
	// and nothing about it travelled to A (it is a setting, not a read mark).
}

// TestArrivalStampIsNotWrittenOnRestart is the upgrade path: a guild that was
// already on disk when the process started must NOT be stamped at startup,
// or the first run after the upgrade would retire everything the owner had
// not read yet.
func TestArrivalStampIsNotWrittenOnRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	a, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	g, err := a.CreateGuild("hamadan")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	if a.guildArrivedNano(g.ID) == 0 {
		t.Fatal("a created guild carries no arrival stamp")
	}
	// Forge the pre-upgrade state: no stamp on disk.
	if err := a.store.SetSetting(guildArrivedPrefix+g.ID, ""); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = a.Close()
	b, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer b.Close()
	if got := b.guildArrivedNano(g.ID); got != 0 {
		t.Fatalf("restart stamped an existing guild with an arrival (%d); history older than the upgrade would be retired", got)
	}
}

// TestArrivalOfADMIsItsCreation: a direct message is addressed to its members
// from the moment it exists, so its floor is the conversation's creation, not
// the moment this device got round to tracking it — the other party's first
// message is usually written before the invite reaches a phone, and a floor
// at the tracking moment would have retired it unread.
func TestArrivalOfADMIsItsCreation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := Start(ctx, Config{DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Close()
	created := time.Now().Add(-26 * time.Hour)
	dm := domain.Guild{ID: "dm-from-elsewhere", Kind: "dm", Created: created}
	a.noteGuildArrival(&dm)
	if got := a.guildArrivedNano(dm.ID); got != created.UnixNano() {
		t.Fatalf("a DM's floor = %d, want its creation %d", got, created.UnixNano())
	}
	// A guild joined by invite is floored at the join: its history is not ours.
	g := domain.Guild{ID: "guild-from-elsewhere", Created: created}
	before := time.Now().UnixNano()
	a.noteGuildArrival(&g)
	if got := a.guildArrivedNano(g.ID); got < before {
		t.Fatalf("a joined guild's floor = %d, want the join (>= %d)", got, before)
	}
}
