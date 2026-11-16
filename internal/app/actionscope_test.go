package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// twoGuilds is a and b sharing two guilds: ga owned by a, gb owned by b. It
// returns b's messages and a's message in gb, all held by b.
type twoGuilds struct {
	a, b           *Service
	ga, gb         domain.Guild
	chA, chB       string
	groupA, groupB []byte
	byB, byB2, byA domain.Message
}

func newTwoGuilds(t *testing.T, ctx context.Context) twoGuilds {
	t.Helper()
	var w twoGuilds
	w.a = startService(t, ctx)
	w.b = startService(t, ctx)
	w.ga = guildWith(t, w.a, w.b)
	w.gb = guildWith(t, w.b, w.a)
	w.chA, w.chB = w.ga.Channels[0].ID, w.gb.Channels[0].ID
	var err error
	if w.byB, err = w.b.SendMessage(w.chB, "written by b", "", ""); err != nil {
		t.Fatalf("b SendMessage: %v", err)
	}
	if w.byB2, err = w.b.SendMessage(w.chB, "also written by b", "", ""); err != nil {
		t.Fatalf("b SendMessage: %v", err)
	}
	if w.byA, err = w.a.SendMessage(w.chB, "written by a", "", ""); err != nil {
		t.Fatalf("a SendMessage: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		row, ok, err := w.b.store.MessageByID(w.byA.ID)
		return err == nil && ok && row.Content == "written by a"
	}, "b never received a's message in b's guild")
	w.b.mu.RLock()
	w.groupA = w.b.guilds[w.ga.ID].GroupID
	w.groupB = w.b.guilds[w.gb.ID].GroupID
	w.b.mu.RUnlock()
	return w
}

// row is b's copy of a message.
func (w twoGuilds) row(t *testing.T, id string) domain.Message {
	t.Helper()
	m, ok, err := w.b.store.MessageByID(id)
	if err != nil || !ok {
		t.Fatalf("MessageByID(%s): %v %v", id, err, ok)
	}
	return m
}

// A message action (delete, pin, reaction, edit) names its target by id, and
// message ids are one key across every guild a device holds. An action is
// applied only inside the guild whose group carried it: the channel it names
// must belong to that guild, and the target row must sit in that channel.
func TestMessageActionsApplyOnlyInTheGroupThatCarriedThem(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newTwoGuilds(t, ctx)

	deliver := func(groupID []byte, m domain.Message) {
		t.Helper()
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		ct, err := w.a.mls.Encrypt(ctx, groupID, raw)
		if err != nil {
			t.Fatalf("a encrypt: %v", err)
		}
		if !w.b.deliverCiphertext(groupID, ct) {
			t.Fatal("b could not decrypt a frame a encrypted at the shared epoch")
		}
	}
	action := func(channelID, kind, target, content string) domain.Message {
		m := domain.Message{
			ID:        domain.NewID(),
			ChannelID: channelID,
			Sender:    w.a.PublicKey(),
			Kind:      kind,
			ReplyTo:   target,
			Content:   content,
			Sent:      time.Now().UTC(),
		}
		m.Sig = w.a.signMessage(m)
		return m
	}

	// Carried by a's guild, naming a's channel, aimed at rows in b's guild.
	deliver(w.groupA, action(w.chA, "delete", w.byB.ID, "deleted"))
	deliver(w.groupA, action(w.chA, "pin", w.byB.ID, "pin"))
	deliver(w.groupA, action(w.chA, "reaction", w.byB.ID, "+1"))
	deliver(w.groupA, action(w.chA, "edit", w.byA.ID, "changed through a's channel"))
	// Carried by a's guild, naming b's channel.
	deliver(w.groupA, action(w.chB, "edit", w.byA.ID, "changed through a's group"))
	deliver(w.groupA, action(w.chB, "delete", w.byA.ID, "deleted"))

	got := w.row(t, w.byB.ID)
	if got.Deleted {
		t.Error("a delete carried by one guild removed a message in another")
	}
	if got.Pinned {
		t.Error("a pin carried by one guild pinned a message in another")
	}
	if len(got.Reactions) != 0 {
		t.Errorf("a reaction carried by one guild landed on a message in another: %v", got.Reactions)
	}
	if mine := w.row(t, w.byA.ID); mine.Deleted || mine.Content != "written by a" {
		t.Fatalf("an action carried by one guild changed a row in another: deleted=%v content=%q", mine.Deleted, mine.Content)
	}

	// The same edit carried by the guild the row belongs to applies.
	deliver(w.groupB, action(w.chB, "edit", w.byA.ID, "edited in place"))
	if mine := w.row(t, w.byA.ID); mine.Content != "edited in place" {
		t.Fatalf("an edit carried by the row's own guild must apply, got %q", mine.Content)
	}
}

// History sync is read under one guild's group, and its source is trusted (or
// not) in that guild. It changes rows of that guild only, and it does not move
// another guild's channel into it.
func TestSyncPayloadChangesOnlyItsOwnGuild(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newTwoGuilds(t, ctx)

	// From a, the owner of ga and so a trusted source there: a tombstone for a
	// row in gb, under ga's channel and under gb's, in a payload whose guild
	// record also lists gb's channel as one of ga's.
	served := w.ga.Clone()
	served.Channels = append(served.Channels, w.gb.Channels[0])
	for _, ch := range []string{w.chA, w.chB} {
		tomb := domain.Message{ID: w.byB2.ID, ChannelID: ch, Sender: w.b.PublicKey(), Deleted: true, Sent: w.byB2.Sent, Updated: time.Now().UTC()}
		payload := syncPayload{Guild: served, Messages: map[string][]domain.Message{ch: {tomb}}}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		ct, err := w.a.mls.Encrypt(ctx, w.groupA, raw)
		if err != nil {
			t.Fatalf("a encrypt: %v", err)
		}
		if !w.b.applySyncPayload(w.ga.ID, w.groupA, ct, w.a.Fingerprint()) {
			t.Fatal("b could not read a sync payload a encrypted at the shared epoch")
		}
		if w.row(t, w.byB2.ID).Deleted {
			t.Errorf("a sync payload for one guild removed a message in another (served under %s's channel)", map[string]string{w.chA: "ga", w.chB: "gb"}[ch])
		}
	}
	if g, ok := w.b.channelGuild(w.chB); !ok || g != w.gb.ID {
		t.Errorf("a sync payload for one guild moved another guild's channel: now in %q", g)
	}
}
