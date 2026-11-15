package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTypingDefaultsOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	if s.TypingEnabled() {
		t.Fatal("a new install broadcasts typing before the user chose to")
	}
}

// An install from before the opt-in default may hold "1" (explicitly on, or
// written by an older switch) or nothing; both end up off exactly once, and a
// choice made afterwards is never overridden again.
func TestTypingMigrationTurnsEveryoneOffOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	// Fake a legacy database: the pref says on and the migration never ran.
	if err := s.store.SetSetting(typingPrefKey, "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetSetting(typingOptInKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = startServiceInDir(t, ctx, dir)
	if s.TypingEnabled() {
		t.Fatal("an existing install kept broadcasting typing after the opt-in change")
	}
	if err := s.SetTypingEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = startServiceInDir(t, ctx, dir)
	if !s.TypingEnabled() {
		t.Fatal("the migration ran again and undid the user's choice")
	}
}

func TestTypingDroppedBeforeTheUIWhileOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	var mu sync.Mutex
	n := 0
	s.OnTyping(func(_, _ string, _ bool) { mu.Lock(); n++; mu.Unlock() })
	g, err := s.CreateGuild("quiet")
	if err != nil {
		t.Fatal(err)
	}
	// Off: nothing reaches the UI and no hello is solicited from the sender.
	s.receiveTyping(g.ID, g.GroupID, g.Channels[0].ID, s.host.PeerID(), []byte{typingStart})
	s.emitTyping(s.id.Fingerprint(), g.Channels[0].ID, false)
	mu.Lock()
	defer mu.Unlock()
	if n != 0 {
		t.Fatalf("%d typing events reached the UI with the switch off", n)
	}
}

func TestTypingPrefSyncNewestWins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	now := time.Now().UnixMilli()

	s.applyRemoteTypingPref(true, now)
	if !s.TypingEnabled() {
		t.Fatal("a newer remote choice was ignored")
	}
	s.applyRemoteTypingPref(false, now-1000)
	if !s.TypingEnabled() {
		t.Fatal("an older remote choice overrode a newer one")
	}
	// A stamp from far in the future is clamped, so a later local choice
	// still beats it instead of being pinned underneath it.
	s.applyRemoteTypingPref(false, now+int64(6*time.Hour/time.Millisecond))
	if s.TypingEnabled() {
		t.Fatal("a clamped future stamp should still be newer than a choice made earlier")
	}
	if st := s.typingStamp(); st > time.Now().Add(readSkew).UnixMilli() {
		t.Fatalf("stamp %d not clamped", st)
	}
	time.Sleep(5 * time.Millisecond)
	if err := s.SetTypingEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !s.TypingEnabled() {
		t.Fatal("a local choice lost to a clock-skewed remote one")
	}
	s.applyRemoteTypingPref(false, now+1)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = startServiceInDir(t, ctx, dir)
	if !s.TypingEnabled() {
		t.Fatal("synced setting or its stamp did not survive a restart")
	}
}

// Two devices of one account: a choice on one reaches the other over Notes.
func TestTypingPrefSyncsAcrossOwnDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	// The wire may not be meshed yet; republish until it lands.
	waitUntil(t, 40*time.Second, func() bool {
		_ = desk.SetTypingEnabled(true)
		time.Sleep(500 * time.Millisecond)
		return phone.TypingEnabled()
	}, "typing setting never reached the other device")
	waitUntil(t, 40*time.Second, func() bool {
		_ = phone.SetTypingEnabled(false)
		time.Sleep(500 * time.Millisecond)
		return !desk.TypingEnabled()
	}, "the later choice never reached the first device")
}

// No indicator while either side is off; it appears once both are on.
func TestTypingIndicatorNeedsBothSides(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("typing")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)
	ch := g.Channels[0].ID

	var mu sync.Mutex
	seen := 0
	b.OnTyping(func(_, _ string, _ bool) { mu.Lock(); seen++; mu.Unlock() })
	count := func() int { mu.Lock(); defer mu.Unlock(); return seen }
	burst := func() {
		for i := 0; i < 5; i++ {
			_ = a.SendTyping(ch)
			time.Sleep(200 * time.Millisecond)
		}
		time.Sleep(time.Second)
	}
	set := func(x *Service, on bool) {
		if err := x.SetTypingEnabled(on); err != nil {
			t.Fatal(err)
		}
	}

	if a.TypingEnabled() || b.TypingEnabled() {
		t.Fatal("not off by default")
	}
	burst() // both off
	set(a, true)
	burst() // only sender on
	set(a, false)
	set(b, true)
	burst() // only receiver on
	if n := count(); n != 0 {
		t.Fatalf("%d indicators shown while one side was off", n)
	}
	set(a, true)
	deadline := time.Now().Add(15 * time.Second)
	for count() == 0 && time.Now().Before(deadline) {
		_ = a.SendTyping(ch)
		time.Sleep(300 * time.Millisecond)
	}
	if count() == 0 {
		t.Fatal("no indicator with both sides on")
	}
}
