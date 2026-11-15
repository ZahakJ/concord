package app

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// receiptRig is a Service with a real store and identity and one DM holding
// us, bob and (optionally) carol, with no network: enough to drive the receipt
// rules directly.
type receiptRig struct {
	s     *Service
	me    *identity.Identity
	bob   *identity.Identity
	carol *identity.Identity
	ch    string
	guild string
}

func newReceiptRig(t *testing.T) *receiptRig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "concord.db"), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r := &receiptRig{me: mustID(t), bob: mustID(t), carol: mustID(t)}
	g := domain.NewGuild("bob", []byte{1, 2, 3}, r.me.PublicKey())
	g.Kind = "dm"
	r.guild, r.ch = g.ID, "dm-channel"
	r.s = &Service{
		ctx:            context.Background(),
		store:          st,
		id:             r.me,
		guilds:         map[string]*domain.Guild{g.ID: &g},
		channelToGuild: map[string]string{r.ch: g.ID},
		memberSets: map[string]*memberSet{g.ID: {
			fprs: map[string]bool{r.me.Fingerprint(): true, r.bob.Fingerprint(): true},
			list: []string{r.me.Fingerprint(), r.bob.Fingerprint()},
			at:   time.Now().Add(time.Hour),
		}},
	}
	return r
}

func (r *receiptRig) send(t *testing.T, from *identity.Identity, at time.Time) domain.Message {
	t.Helper()
	m, err := domain.NewMessage(r.ch, from.PublicKey(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	m.Sent = at
	if _, err := r.s.store.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDeliveredIsAlwaysOnAndNeverForOurOwnMessages(t *testing.T) {
	r := newReceiptRig(t)
	if r.s.ReadReceiptsEnabled() {
		t.Fatal("read receipts must default to off")
	}
	theirs := r.send(t, r.bob, time.Now())
	mine := r.send(t, r.me, time.Now())
	r.s.noteDelivered(theirs)
	r.s.noteDelivered(mine)
	if got := r.s.pendingAcks[r.ch]; len(got) != 1 || got[0] != theirs.ID {
		t.Fatalf("pending acks = %v, want only their message", got)
	}
	// System rows and non-DM channels are not acked.
	sys := theirs
	sys.ID, sys.Kind = "sys", "system"
	r.s.noteDelivered(sys)
	r.s.guilds[r.guild].Kind = ""
	other := theirs
	other.ID = "other"
	r.s.noteDelivered(other)
	if got := r.s.pendingAcks[r.ch]; len(got) != 1 {
		t.Fatalf("pending acks = %v after system/non-DM", got)
	}
}

func TestReadReceiptsAreMutual(t *testing.T) {
	r := newReceiptRig(t)
	base := time.Now().Add(-time.Hour)
	m1 := r.send(t, r.me, base)
	read := guildMeta{Type: "read_receipt", ChannelID: r.ch, Since: 0, At: base.Add(time.Minute).UnixMilli()}

	// Ours off: a receipt that arrives is dropped, not stored.
	r.s.receiveReceipt(r.guild, r.bob.Fingerprint(), read)
	if rs, _ := r.s.store.ReceiptsFor(r.ch); len(rs) != 0 {
		t.Fatalf("receipt stored while ours is off: %v", rs)
	}
	// Ours off: nothing is queued to send either.
	r.s.queueReadReceipt(r.ch, 0, base.UnixMilli())
	if len(r.s.pendingReads) != 0 {
		t.Fatalf("read receipt queued while off: %v", r.s.pendingReads)
	}

	// Both on: stored and shown.
	if err := r.s.SetReadReceiptsEnabled(true); err != nil {
		t.Fatal(err)
	}
	r.s.receiveReceipt(r.guild, r.bob.Fingerprint(), read)
	v := r.s.mustReceipts(t, r.ch)[m1.ID]
	if !v.Read || !v.Delivered || v.Of != 1 {
		t.Fatalf("both on: %+v", v)
	}
	r.s.queueReadReceipt(r.ch, 0, 5)
	if len(r.s.pendingReads) != 1 {
		t.Fatal("read receipt not queued while on")
	}

	// Switching off hides and forgets what we were told, and stops sending.
	if err := r.s.SetReadReceiptsEnabled(false); err != nil {
		t.Fatal(err)
	}
	if len(r.s.pendingReads) != 0 {
		t.Fatal("pending read receipts survived switching off")
	}
	v = r.s.mustReceipts(t, r.ch)[m1.ID]
	if v.Read || !v.Delivered {
		t.Fatalf("after off: %+v (delivered must stay, read must go)", v)
	}
	if rs, _ := r.s.store.ReceiptsFor(r.ch); rs[m1.ID][0].Read != 0 {
		t.Fatal("read receipt kept in the store after switching off")
	}
}

func (s *Service) mustReceipts(t *testing.T, ch string) map[string]ReceiptView {
	t.Helper()
	v, err := s.Receipts(ch)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTurningReadReceiptsOnDoesNotBackfill(t *testing.T) {
	r := newReceiptRig(t)
	// Read through 100 while off, then through 200 after switching on.
	if _, err := r.s.MarkRead(r.ch, time.Now().Add(-time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if len(r.s.pendingReads) != 0 {
		t.Fatal("receipt queued while off")
	}
	prior, _ := r.s.store.ReadMark(r.ch)
	if err := r.s.SetReadReceiptsEnabled(true); err != nil {
		t.Fatal(err)
	}
	if len(r.s.pendingReads) != 0 {
		t.Fatal("switching on queued a receipt for what was already read")
	}
	next := time.Now().Add(-time.Minute).UnixMilli()
	if _, err := r.s.MarkRead(r.ch, next); err != nil {
		t.Fatal(err)
	}
	span := r.s.pendingReads[r.ch]
	if span[0] != prior || span[1] != next {
		t.Fatalf("span = %v, want [%d %d]: only what was read after switching on", span, prior, next)
	}
}

func TestReceiptsFromStrangersAndForOthersMessagesAreIgnored(t *testing.T) {
	r := newReceiptRig(t)
	mine := r.send(t, r.me, time.Now().Add(-time.Hour))
	theirs := r.send(t, r.bob, time.Now().Add(-time.Hour))
	_ = r.s.SetReadReceiptsEnabled(true)
	del := func(actor string, ids ...string) {
		r.s.receiveReceipt(r.guild, actor, guildMeta{Type: "delivered", ChannelID: r.ch, MsgIDs: ids})
	}
	del(r.carol.Fingerprint(), mine.ID) // not a member
	del(r.me.Fingerprint(), mine.ID)    // ourselves
	del("", mine.ID)
	del(r.bob.Fingerprint(), theirs.ID, "nope") // not ours
	r.s.receiveReceipt("another-guild", r.bob.Fingerprint(), guildMeta{Type: "delivered", ChannelID: r.ch, MsgIDs: []string{mine.ID}})
	r.s.receiveReceipt(r.guild, r.bob.Fingerprint(), guildMeta{Type: "delivered", ChannelID: "elsewhere", MsgIDs: []string{mine.ID}})
	if rs, _ := r.s.store.ReceiptsFor(r.ch); len(rs) != 0 {
		t.Fatalf("ignored receipts were stored: %v", rs)
	}
	del(r.bob.Fingerprint(), mine.ID)
	if rs, _ := r.s.store.ReceiptsFor(r.ch); len(rs) != 1 {
		t.Fatalf("a member's delivered ack was not stored: %v", rs)
	}
}

func TestGroupDMNeedsEveryMember(t *testing.T) {
	r := newReceiptRig(t)
	set := r.s.memberSets[r.guild]
	set.fprs[r.carol.Fingerprint()] = true
	set.list = append(set.list, r.carol.Fingerprint())
	mine := r.send(t, r.me, time.Now())
	r.s.receiveReceipt(r.guild, r.bob.Fingerprint(), guildMeta{Type: "delivered", ChannelID: r.ch, MsgIDs: []string{mine.ID}})
	v := r.s.mustReceipts(t, r.ch)[mine.ID]
	if v.Delivered || v.DeliveredBy != 1 || v.Of != 2 {
		t.Fatalf("one of two: %+v", v)
	}
	r.s.receiveReceipt(r.guild, r.carol.Fingerprint(), guildMeta{Type: "delivered", ChannelID: r.ch, MsgIDs: []string{mine.ID}})
	if v = r.s.mustReceipts(t, r.ch)[mine.ID]; !v.Delivered {
		t.Fatalf("two of two: %+v", v)
	}
}

func TestReceiptsPrefNewestWinsAndSurvivesRestart(t *testing.T) {
	r := newReceiptRig(t)
	if err := r.s.applyReceiptsPref(receiptsPref{On: true, At: 2000}, false); err != nil {
		t.Fatal(err)
	}
	_ = r.s.applyReceiptsPref(receiptsPref{On: false, At: 1000}, false) // older: ignored
	if !r.s.ReadReceiptsEnabled() {
		t.Fatal("an older stamp overrode a newer one")
	}
	r.s.loadReceiptsPref()
	if !r.s.ReadReceiptsEnabled() {
		t.Fatal("setting not persisted")
	}
}
