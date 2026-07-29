package app

import (
	"context"
	"testing"
	"time"
)

// connectHosts dials b from a directly, so two services are reachable to each
// other WITHOUT sharing a guild — the one arrangement every other test gets for
// free via the invite handshake, and the exact arrangement a stranger's DM
// arrives in.
func connectHosts(t *testing.T, ctx context.Context, a, b *Service) {
	t.Helper()
	if err := a.host.Connect(ctx, b.host.AddrInfo()); err != nil {
		t.Fatalf("connect peers: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := a.peerForFingerprint(b.Fingerprint()); ok {
			if _, ok := b.peerForFingerprint(a.Fingerprint()); ok {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("peers did not resolve each other's fingerprints")
}

// waitRequests blocks until s holds want message requests.
func waitRequests(t *testing.T, s *Service, want int) []MessageRequest {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if reqs := s.MessageRequests(); len(reqs) == want {
			return reqs
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("message requests never reached %d (have %d)", want, len(s.MessageRequests()))
	return nil
}

// TestStrangerDMWaitsInRequests is the acceptance test for message requests: a
// peer we have no relationship with opens a DM, and the invite must sit
// un-redeemed in the tray — no group joined (which is what would have handed
// them our profile and mailbox key) — until we accept, at which point the
// conversation opens for real on both sides.
func TestStrangerDMWaitsInRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stranger := startService(t, ctx)
	me := startService(t, ctx)
	if err := stranger.SetProfile(Profile{Name: "hypatia"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	connectHosts(t, ctx, stranger, me)

	dm, err := stranger.StartDM(me.Fingerprint())
	if err != nil {
		t.Fatalf("stranger StartDM: %v", err)
	}

	reqs := waitRequests(t, me, 1)
	if reqs[0].From != stranger.Fingerprint() {
		t.Fatalf("request from %q, want %q", reqs[0].From, stranger.Fingerprint())
	}
	if reqs[0].Code == "" {
		t.Fatal("request carries no invite code — accepting could never work")
	}

	// The whole point: we are NOT in their group, so nothing about us reached
	// them. A tray row that had already joined would be theatre.
	for _, g := range me.Guilds() {
		if g.ID == dm.ID {
			t.Fatal("a stranger's DM joined the group before it was accepted")
		}
	}
	if n, _ := stranger.MemberCount(dm.ID); n != 1 {
		t.Fatalf("stranger's DM has %d members before acceptance, want 1", n)
	}

	// A re-push (which really happens on every reconnect) must refresh the row,
	// not stack up a second one.
	pid, ok := stranger.peerForFingerprint(me.Fingerprint())
	if !ok {
		t.Fatal("stranger lost the peer")
	}
	code, err := stranger.InviteCode(dm.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	stranger.pushDMInvite(pid, code)
	time.Sleep(2 * time.Second)
	if reqs := me.MessageRequests(); len(reqs) != 1 {
		t.Fatalf("re-pushed invite made %d requests, want 1", len(reqs))
	}

	// Accepting redeems the held code — now, and only now, we join.
	joined, err := me.AcceptMessageRequest(stranger.Fingerprint())
	if err != nil {
		t.Fatalf("AcceptMessageRequest: %v", err)
	}
	if joined.ID != dm.ID {
		t.Fatalf("accepted into guild %s, want the stranger's DM %s", joined.ID, dm.ID)
	}
	if len(me.MessageRequests()) != 0 {
		t.Fatal("accepted request stayed in the tray")
	}
	waitMemberCount(t, 15*time.Second, dm.ID, 2, stranger, me)
}

// TestKnownContactDMSkipsRequests pins the other half: someone we already share
// a guild with is not a stranger, so their first DM must land directly. Gating
// people you already talk to is how a request tray becomes a thing users click
// through without reading.
func TestKnownContactDMSkipsRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	friend := startService(t, ctx)
	me := startService(t, ctx)

	shared, err := friend.CreateGuild("shared")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, _ := friend.InviteCode(shared.ID)
	if _, err := me.JoinViaInvite(code); err != nil {
		t.Fatalf("join shared guild: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, friend, me)

	dm, err := friend.StartDM(me.Fingerprint())
	if err != nil {
		t.Fatalf("friend StartDM: %v", err)
	}
	waitMemberCount(t, 20*time.Second, dm.ID, 2, friend, me)
	if reqs := me.MessageRequests(); len(reqs) != 0 {
		t.Fatalf("a guild-mate's DM landed in the requests tray (%d rows)", len(reqs))
	}
}

// waitMemberCount waits for every service to see want members in guildID.
func waitMemberCount(t *testing.T, timeout time.Duration, guildID string, want int, svcs ...*Service) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok := true
		for _, s := range svcs {
			if n, _ := s.MemberCount(guildID); n != want {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("guild %s did not converge to %d members within %s", guildID, want, timeout)
}

// TestMessageRequestsPersistAndExpire covers the tray's bookkeeping without a
// network: it survives a restart, and rows nobody ever answered age out rather
// than piling up forever.
func TestMessageRequestsPersistAndExpire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)
	s.recordMessageRequest("fpr-fresh-000", "code-fresh")
	s.recordMessageRequest("fpr-stale-000", "code-stale")

	// Age one row past the TTL and rewrite the tray as it would be on disk.
	s.reqMu.Lock()
	stale := s.requests["fpr-stale-000"]
	stale.At = time.Now().Add(-messageRequestTTL - time.Hour).UnixMilli()
	s.requests["fpr-stale-000"] = stale
	s.reqMu.Unlock()
	s.persistMessageRequests()
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again := startServiceInDir(t, ctx, dir)
	reqs := again.MessageRequests()
	if len(reqs) != 1 || reqs[0].From != "fpr-fresh-000" || reqs[0].Code != "code-fresh" {
		t.Fatalf("after restart tray = %+v, want only the fresh request", reqs)
	}

	// Declining with block both clears the row and stops them coming back.
	if err := again.DeclineMessageRequest("fpr-fresh-000", true); err != nil {
		t.Fatalf("DeclineMessageRequest: %v", err)
	}
	if len(again.MessageRequests()) != 0 {
		t.Fatal("declined request stayed in the tray")
	}
	if !again.IsBlocked("fpr-fresh-000") {
		t.Fatal("decline-with-block did not block the sender")
	}
	if _, err := again.AcceptMessageRequest("fpr-fresh-000"); err == nil {
		t.Fatal("accepting a request that no longer exists succeeded")
	}
}

// TestKnownContactCoversOurOwnOutreach pins the crossing-invites case: once we
// have opened a conversation aimed at someone, their invite arriving from the
// other direction is not a stranger knocking — we already decided.
func TestKnownContactCoversOurOwnOutreach(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())

	const other = "fpr-other-000"
	if s.knownContact(other) {
		t.Fatal("an account we have never met counted as known")
	}
	if !s.knownContact(s.Fingerprint()) {
		t.Fatal("our own account (a linked device) counted as a stranger")
	}
	s.queueDMInvite("guild-not-real", other)
	if !s.knownContact(other) {
		t.Fatal("someone we queued a DM invite for still counted as a stranger")
	}
}

// TestBlockedStrangerLeavesNoTrace: a blocked account can't even leave a row in
// the tray, so blocking is not a per-message chore.
func TestBlockedStrangerLeavesNoTrace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stranger := startService(t, ctx)
	me := startService(t, ctx)
	if err := me.BlockUser(stranger.Fingerprint()); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	connectHosts(t, ctx, stranger, me)

	if _, err := stranger.StartDM(me.Fingerprint()); err != nil {
		t.Fatalf("stranger StartDM: %v", err)
	}
	time.Sleep(3 * time.Second)
	if reqs := me.MessageRequests(); len(reqs) != 0 {
		t.Fatalf("blocked stranger left %d requests in the tray", len(reqs))
	}
}
