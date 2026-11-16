package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/crypto/mls"
)

// metaSpy records the type of every guild-meta frame a service encrypts, per
// group, so a test can see what left the device and what did not.
type metaSpy struct {
	mls.Engine
	mu   sync.Mutex
	seen map[string]map[string]int // group -> meta type -> count
}

func (sp *metaSpy) Encrypt(ctx context.Context, gid mls.GroupID, plaintext []byte) ([]byte, error) {
	var m struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(plaintext, &m) == nil && m.Type != "" {
		sp.mu.Lock()
		if sp.seen == nil {
			sp.seen = map[string]map[string]int{}
		}
		if sp.seen[string(gid)] == nil {
			sp.seen[string(gid)] = map[string]int{}
		}
		sp.seen[string(gid)][m.Type]++
		sp.mu.Unlock()
	}
	return sp.Engine.Encrypt(ctx, gid, plaintext)
}

func (sp *metaSpy) count(gid []byte, typ string) int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.seen[string(gid)][typ]
}

// receiptPair starts A (spied) and B on one rendezvous, with a DM that A
// started, and returns A's and B's channel ids for it.
func receiptPair(t *testing.T, ctx context.Context) (a, b *Service, spy *metaSpy, dmID, aCh, bCh string) {
	t.Helper()
	boot := testRendezvous(t, ctx)
	spy = &metaSpy{}
	a, err := Start(ctx, Config{
		DataDir: t.TempDir(), Passphrase: "test-pass", DisableMDNS: true,
		BootstrapPeers: []string{boot},
		wrapMLS:        func(e mls.Engine) mls.Engine { spy.Engine = e; return spy },
	})
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	b = startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := a.CreateGuild("hub")
	if err != nil {
		t.Fatal(err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B join: %v", err)
	}
	waitMemberCount(t, 90*time.Second, g.ID, 2, a, b)
	dmID, aCh, bCh = dmChannelFor(t, a, b)
	return a, b, spy, dmID, aCh, bCh
}

func tickOf(t *testing.T, s *Service, ch, id string) ReceiptView {
	t.Helper()
	v, err := s.Receipts(ch)
	if err != nil {
		t.Fatal(err)
	}
	return v[id]
}

// ZW-02. A DM copy that CannotRepairHere sends no delivered ack and no read
// receipt: both are encrypted to the tree the copy was restored to, like any
// other meta frame. Once the copy is healthy both go out again.
func TestBarredDMCopySendsNoAcksOrReadReceipts(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b, spy, dm, aCh, bCh := receiptPair(t, ctx)
	group := rhGroup(a, dm)
	for _, s := range []*Service{a, b} {
		if err := s.SetReadReceiptsEnabled(true); err != nil {
			t.Fatal(err)
		}
	}

	latch(t, a, dm)
	sent, err := b.SendMessage(bCh, "to the barred copy", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return hasMessage(a, aCh, "to the barred copy") },
		"setup: the barred copy never received B's message")
	if _, err := a.MarkRead(aCh, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	if n := spy.count(group, "delivered"); n != 0 {
		t.Errorf("the barred copy encrypted %d delivered acks", n)
	}
	if n := spy.count(group, "read_receipt"); n != 0 {
		t.Errorf("the barred copy encrypted %d read receipts", n)
	}
	if v := tickOf(t, b, bCh, sent.ID); v.Delivered || v.Read {
		t.Errorf("B holds receipts from the barred copy: %+v", v)
	}

	a.clearBehind(dm)
	if a.CannotRepairHere(dm) {
		t.Fatal("setup: the copy still reads as barred")
	}
	next, err := b.SendMessage(bCh, "to the healthy copy", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return tickOf(t, b, bCh, next.ID).Delivered },
		"the healthy copy's delivered ack never reached B")
	time.Sleep(10 * time.Millisecond)
	if _, err := a.MarkRead(aCh, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return tickOf(t, b, bCh, next.ID).Read },
		"the healthy copy's read receipt never reached B")
}

// ZW-02, the Notes lanes. The receipts and typing settings, chat pins, hides
// and read markers go to the Notes group through publishMeta, so a Notes copy
// that CannotRepairHere encrypts none of them. A Notes copy in that state
// lasts only as long as the roster cache disagrees with the tree (aloneInGuild
// reads the cache, onlyPossibleCommitter the tree), so the test sets the
// cache before each change.
func TestBarredNotesCopySendsNoOwnDeviceLanes(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b, spy, _, aCh, bCh := receiptPair(t, ctx)
	notes, err := a.ensureNotesDM()
	if err != nil {
		t.Fatal(err)
	}
	ep, err := a.mls.Epoch(ctx, notes.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	a.noteBehind(notes.ID, ep+1)
	bar := func() {
		t.Helper()
		a.memberSetMu.Lock()
		if a.memberSets == nil {
			a.memberSets = map[string]*memberSet{}
		}
		a.memberSets[notes.ID] = &memberSet{creds: [][]byte{a.myCredential, []byte("another leaf")}, at: time.Now()}
		a.memberSetMu.Unlock()
		if !a.CannotRepairHere(notes.ID) {
			t.Fatal("setup: the Notes copy does not read as CannotRepairHere")
		}
	}
	msg, err := b.SendMessage(bCh, "for the lanes", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool { return hasMessage(a, aCh, "for the lanes") },
		"setup: A never received B's message")

	// Each change, then a wait past its beat (hides 0.8 s, read markers 1.2 s),
	// inside the cache's 2 s.
	for _, c := range []struct {
		typ string
		do  func() error
	}{
		{"receipts_pref", func() error { return a.SetReadReceiptsEnabled(true) }},
		{"typing_pref", func() error { return a.SetTypingEnabled(true) }},
		{"chat_prefs", func() error { return a.SetChatPinned(aCh, true) }},
		{"hide_marker", func() error { return a.HideMessage(aCh, msg.ID) }},
		{"read_marker", func() error { _, err := a.MarkRead(aCh, time.Now().UnixMilli()); return err }},
	} {
		bar()
		if err := c.do(); err != nil {
			t.Fatalf("%s: %v", c.typ, err)
		}
		time.Sleep(1500 * time.Millisecond)
		if n := spy.count(notes.GroupID, c.typ); n != 0 {
			t.Errorf("the barred Notes copy encrypted %d %s frames", n, c.typ)
		}
	}
}
