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
	deliver(w.groupA, action(w.chB, "reaction", w.byB.ID, "+2"))

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
	// So do a reaction and a pin carried by the row's own guild and channel.
	deliver(w.groupB, action(w.chB, "reaction", w.byB.ID, "ok"))
	if got := w.row(t, w.byB.ID); len(got.Reactions["ok"]) != 1 {
		t.Errorf("a reaction in the row's own channel must apply, got %v", got.Reactions)
	}
	inA, err := w.a.SendMessage(w.chA, "pinned in a's guild", "", "")
	if err != nil {
		t.Fatalf("a SendMessage: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		_, ok, err := w.b.store.MessageByID(inA.ID)
		return err == nil && ok
	}, "b never received a's message in a's guild")
	deliver(w.groupA, action(w.chA, "pin", inA.ID, "pin"))
	if !w.row(t, inA.ID).Pinned {
		t.Error("a pin in the row's own channel must apply")
	}

	// The sending side refuses an action on a message outside the channel.
	if err := w.a.DeleteMessage(w.chA, w.byA.ID); err == nil {
		t.Error("DeleteMessage accepted a target from another channel")
	}
	// So does an edit: it is sent on the row's own channel.
	if err := w.a.EditMessage(w.chA, w.byA.ID, "edited from a's channel", ""); err == nil {
		t.Error("EditMessage accepted a target from another channel")
	}
	if err := w.a.EditMessage(w.chB, w.byA.ID, "edited on its own channel", ""); err != nil {
		t.Errorf("EditMessage on the row's own channel: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		row, ok, err := w.b.store.MessageByID(w.byA.ID)
		return err == nil && ok && row.Content == "edited on its own channel"
	}, "an edit sent on the row's own channel never reached b")
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

	apply := func(payload syncPayload) (*syncPayload, map[string]bool) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		ct, err := w.a.mls.Encrypt(ctx, w.groupA, raw)
		if err != nil {
			t.Fatalf("a encrypt: %v", err)
		}
		ok, got, failed := w.b.applySyncPayloadFrom(w.ga.ID, w.groupA, ct, w.a.Fingerprint())
		if !ok {
			t.Fatal("b could not read a sync payload a encrypted at the shared epoch")
		}
		return got, failed
	}

	// Rows served under another guild's channel are not stored, and the
	// catch-up position of that channel stays where it was.
	floorB, _ := w.b.store.SyncFloor(w.chB)
	later := domain.Message{ID: domain.NewID(), ChannelID: w.chB, Sender: w.a.PublicKey(), Content: "served under gb", Sent: time.Now().Add(365 * 24 * time.Hour).UTC()}
	later.Sig = w.a.signMessage(later)
	got, failed := apply(syncPayload{Guild: w.ga.Clone(), Messages: map[string][]domain.Message{w.chB: {later}}})
	w.b.advanceSyncCursor(w.a.host.PeerID(), syncCursor{since: map[string]int64{}}, got, failed)
	if _, held, _ := w.b.store.MessageByID(later.ID); held {
		t.Error("a sync payload for one guild stored a row under another guild's channel")
	}
	if f, _ := w.b.store.SyncFloor(w.chB); f != floorB {
		t.Errorf("a sync payload for one guild moved another guild's catch-up position: %d, was %d", f, floorB)
	}

	// The same source still changes its own guild: a new row and a tombstone
	// for a row held in ga's channel both apply.
	own, err := w.a.SendMessage(w.chA, "in a's guild", "", "")
	if err != nil {
		t.Fatalf("a SendMessage: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		_, ok, err := w.b.store.MessageByID(own.ID)
		return err == nil && ok
	}, "b never received a's message in a's guild")
	fresh := domain.Message{ID: domain.NewID(), ChannelID: w.chA, Sender: w.a.PublicKey(), Content: "served under ga", Sent: time.Now().UTC()}
	fresh.Sig = w.a.signMessage(fresh)
	tomb := own
	tomb.Deleted, tomb.Updated = true, time.Now().UTC()
	apply(syncPayload{Guild: w.ga.Clone(), Messages: map[string][]domain.Message{w.chA: {fresh, tomb}}})
	if _, held, _ := w.b.store.MessageByID(fresh.ID); !held {
		t.Error("a sync payload did not store a row in its own guild")
	}
	if !w.row(t, own.ID).Deleted {
		t.Error("a sync payload from the guild's owner did not apply a tombstone in its own guild")
	}
}

// deliver hands b a frame that a encrypted under groupID, as the live lane
// receives it.
func (w twoGuilds) deliver(t *testing.T, ctx context.Context, groupID []byte, m domain.Message) {
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

// signedByA is a message from a, signed as a's sends are.
func (w twoGuilds) signedByA(channelID, kind, target, content string) domain.Message {
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

// A delete, pin or reaction that reaches a device before the row it names is
// applied when the row arrives live, and only then. An edit is not held.
func TestAnActionThatArrivesBeforeItsTargetApplies(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newTwoGuilds(t, ctx)

	row := w.signedByA(w.chB, "", "", "arrives second")
	w.deliver(t, ctx, w.groupB, w.signedByA(w.chB, "reaction", row.ID, "late"))
	w.deliver(t, ctx, w.groupB, w.signedByA(w.chB, "edit", row.ID, "edited early"))
	// The same, aimed at a row that will turn out to be in another guild.
	other := w.signedByA(w.chA, "", "", "in a's guild")
	w.deliver(t, ctx, w.groupB, w.signedByA(w.chB, "reaction", other.ID, "elsewhere"))

	w.deliver(t, ctx, w.groupB, row)
	got := w.row(t, row.ID)
	if len(got.Reactions["late"]) != 1 {
		t.Errorf("a reaction that arrived before its target was lost: %v", got.Reactions)
	}
	if got.Content != "arrives second" {
		t.Errorf("an edit that arrived before its target was held: %q", got.Content)
	}

	w.deliver(t, ctx, w.groupA, other)
	if got := w.row(t, other.ID); len(got.Reactions) != 0 {
		t.Errorf("an early action applied to a row of another guild: %v", got.Reactions)
	}
}

// An unsigned delete is what v0.57 and older send. It is accepted from the
// message's own author and carries no moderator's authority, so even the
// guild's owner removes only their own message with one.
func TestAnUnsignedDeleteActsOnlyForItsAuthor(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newTwoGuilds(t, ctx) // a owns ga

	theirs, err := w.b.SendMessage(w.chA, "b in a's guild", "", "")
	if err != nil {
		t.Fatalf("b SendMessage: %v", err)
	}
	theirs2, err := w.b.SendMessage(w.chA, "b again in a's guild", "", "")
	if err != nil {
		t.Fatalf("b SendMessage: %v", err)
	}
	mine, err := w.a.SendMessage(w.chA, "a in a's guild", "", "")
	if err != nil {
		t.Fatalf("a SendMessage: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		_, ok, err := w.b.store.MessageByID(mine.ID)
		return err == nil && ok
	}, "b never received a's message in a's guild")

	unsigned := func(target string) domain.Message {
		m := w.signedByA(w.chA, "delete", target, "deleted")
		m.Sig = nil
		return m
	}
	w.deliver(t, ctx, w.groupA, unsigned(theirs.ID))
	if w.row(t, theirs.ID).Deleted {
		t.Error("an unsigned delete from the owner removed another member's message")
	}
	w.deliver(t, ctx, w.groupA, unsigned(mine.ID))
	if !w.row(t, mine.ID).Deleted {
		t.Error("an unsigned delete of the sender's own message did not apply")
	}
	// Signed, the owner's delete of another member's message applies.
	w.deliver(t, ctx, w.groupA, w.signedByA(w.chA, "delete", theirs2.ID, "deleted"))
	if !w.row(t, theirs2.ID).Deleted {
		t.Error("a signed delete from the owner did not apply")
	}
}
