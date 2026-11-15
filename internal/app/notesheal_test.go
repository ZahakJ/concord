package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// guardedPair is an owner U and an ordinary member P in one guild, on one
// rendezvous. U is the guild's only possible committer.
func guardedPair(t *testing.T, ctx context.Context, boot string) (u, p *Service, guildID, channelID string) {
	t.Helper()
	u = startServiceOn(t, ctx, t.TempDir(), boot)
	p = startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := u.CreateGuild("guarded")
	if err != nil {
		t.Fatal(err)
	}
	code, err := u.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.JoinViaInvite(code); err != nil {
		t.Fatalf("P join: %v", err)
	}
	waitMemberCount(t, 90*time.Second, g.ID, 2, u, p)
	return u, p, g.ID, g.Channels[0].ID
}

// latch puts the "seen behind" latch on s for a guild one epoch past where it
// stands: what a restored copy holds once it has met a member ahead of it.
func latch(t *testing.T, s *Service, guildID string) {
	t.Helper()
	cur, err := s.mls.Epoch(s.ctx, rhGroup(s, guildID))
	if err != nil {
		t.Fatal(err)
	}
	s.noteBehind(guildID, cur+1)
	if !s.CannotRepairHere(guildID) {
		t.Fatal("setup: the latched sole committer does not read as CannotRepairHere")
	}
}

// devicePubOf is the device key a service's libp2p identity is.
func devicePubOf(t *testing.T, s *Service) []byte {
	t.Helper()
	pub, err := s.host.PeerID().ExtractPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := pub.Raw()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// linkDevice links a new device into s's account and starts it. It joins none
// of the account's guilds: the invites the link hands over are returned.
func linkDevice(t *testing.T, ctx context.Context, s *Service, boot string) (*Service, []string) {
	t.Helper()
	code, err := s.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res, err := RedeemLink(ctx, dir, code, "test-pass", "")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	return startServiceOn(t, ctx, dir, boot), res.GuildInvites
}

// SG-01. A sole committer's latched copy of an ordinary guild stays barred when
// another device of the owner's account connects that is not seated in the
// guild. That device cannot repair the guild (it holds no copy of it), so its
// presence must not lift the guard.
func TestBarredCopyStaysBarredWhenAnUnseatedSiblingConnects(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, _, gid, _ := guardedPair(t, ctx, boot)
	latch(t, u, gid)

	tab, _ := linkDevice(t, ctx, u, boot)
	waitUntil(t, 60*time.Second, func() bool {
		return u.presence(tab.host.PeerID()).Fingerprint == u.Fingerprint() &&
			slices.Contains(u.host.Peers(), tab.host.PeerID())
	}, "the tablet never connected to U as a device of U's account")
	if n := memberCount(u, gid); n != 2 {
		t.Fatalf("setup: the guild has %d leaves, want 2 (the tablet must not be seated)", n)
	}

	if !u.CannotRepairHere(gid) {
		t.Fatal("an unseated device of the owner connected and the guard came off")
	}
	if err := u.mintBarred(gid); !errors.Is(err, ErrBehindOnlyCommitter) {
		t.Fatalf("mintBarred with the tablet connected: %v", err)
	}
	if err := u.ResolveStranding(gid); !errors.Is(err, ErrBehindOnlyCommitter) {
		t.Fatalf("Continue from here with the tablet connected: err=%v", err)
	}
}

// SG-04. A device of this account is asked for a heal only if this device saw
// it strictly ahead in the guild. After a restore, a device unlinked since the
// backup still resolves to the account (its revocation went with the backup)
// and may still hold a stale copy; it must not be the healer.
func TestHealSkipsSiblingNotSeenAhead(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := desk.CreateGuild("Shared")
	if err != nil {
		t.Fatal(err)
	}
	phone, _ := linkDevice(t, ctx, desk, boot)
	tab, _ := linkDevice(t, ctx, desk, boot)
	waitUntil(t, 60*time.Second, func() bool {
		return len(desk.authorizedCommittersOnline(g.ID)) == 2
	}, "the desk never saw both of its devices as the account's")

	if got := desk.healCandidates(g.ID); len(got) != 0 {
		t.Fatalf("siblings nobody saw ahead are heal candidates: %v", got)
	}
	desk.noteAhead(g.ID, phone.host.PeerID())
	got := desk.healCandidates(g.ID)
	if !slices.Equal(got, []peer.ID{phone.host.PeerID()}) {
		t.Fatalf("heal candidates %v, want only the phone %s (the tablet %s was never seen ahead)",
			got, phone.host.PeerID(), tab.host.PeerID())
	}
	// The latch going down (a heal, Continue, eviction, or catching up) takes
	// the observation with it.
	desk.noteBehind(g.ID, 99)
	desk.clearBehind(g.ID)
	if got := desk.healCandidates(g.ID); len(got) != 0 {
		t.Fatalf("an observation outlived the latch: %v", got)
	}
}

// SG-02, and leaves rather than accounts. A guild seating a second device of
// this account is not "alone": that device may hold what this one is missing.
// A leaf of a device this device has unlinked is not counted: it will never
// serve anything, and counting it would keep the stranding from resolving.
func TestAloneCountsLeavesButNotRevokedSiblings(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := desk.CreateGuild("Shared")
	if err != nil {
		t.Fatal(err)
	}
	tab, invites := linkDevice(t, ctx, desk, boot)
	for _, ic := range invites {
		if _, err := tab.JoinViaInvite(ic); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitMemberCount(t, 60*time.Second, g.ID, 2, desk)
	if desk.aloneInGuild(g.ID) {
		t.Fatal("a guild seating another device of this account read as alone")
	}

	_ = tab.Close()
	// The tablet is unlinked but its leaf stays (as where no committer could
	// remove it): this device holds its revocation, nothing more.
	key := hex.EncodeToString(devicePubOf(t, tab))
	desk.deviceMu.Lock()
	desk.revokedDevices[key] = true
	delete(desk.deviceAccounts, key)
	desk.deviceMu.Unlock()
	if n := memberCount(desk, g.ID); n != 2 {
		t.Fatalf("setup: %d leaves, want the revoked tablet's still seated", n)
	}
	if !desk.aloneInGuild(g.ID) {
		t.Fatal("a revoked sibling's leaf kept the guild from reading as alone")
	}
	desk.setOutOfSync(g.ID, true)
	desk.recoverOutOfSync(g.ID)
	if desk.OutOfSync(g.ID) {
		t.Fatal("a guild holding only this device and a revoked sibling did not resolve on its own")
	}
}

// SG-03. A copy that is alone by its own tree but has seen a member ahead is
// not resolved automatically, the manual Continue stays open for it, and the
// view does not call it alone.
func TestLatchedAndAloneDoesNotResolveByItself(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := startService(t, ctx)
	g, err := u.CreateGuild("Only me here")
	if err != nil {
		t.Fatal(err)
	}
	if !u.aloneInGuild(g.ID) || !u.AloneInGuild(g.ID) {
		t.Fatal("setup: a guild of one leaf is not alone")
	}
	cur, err := u.mls.Epoch(u.ctx, rhGroup(u, g.ID))
	if err != nil {
		t.Fatal(err)
	}
	u.noteBehind(g.ID, cur+1)
	u.setOutOfSync(g.ID, true)

	u.recoverOutOfSync(g.ID)
	if !u.OutOfSync(g.ID) || !u.behindLatched(g.ID) {
		t.Fatal("a latched copy, alone by its own tree, resolved its stranding by itself")
	}
	if u.AloneInGuild(g.ID) {
		t.Fatal("the view calls a latched copy alone; the banner would say nobody else is here")
	}
	if u.CannotRepairHere(g.ID) {
		t.Fatal("a copy alone in its tree reads as CannotRepairHere")
	}
	if err := u.ResolveStranding(g.ID); err != nil {
		t.Fatalf("the manual Continue was refused: %v", err)
	}
	if u.OutOfSync(g.ID) || u.behindLatched(g.ID) {
		t.Fatal("the accepted Continue left the flag or the latch")
	}
}

// SG-05. A heal whose Welcome is for another group has repaired nothing: the
// guild stays flagged and latched.
func TestHealRefusesWelcomeForAnotherGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	u, p, g1, _ := guardedPair(t, ctx, boot)
	g2, err := u.CreateGuild("second")
	if err != nil {
		t.Fatal(err)
	}
	code, err := u.InviteCode(g2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.JoinViaInvite(code); err != nil {
		t.Fatalf("P join second: %v", err)
	}
	waitMemberCount(t, 90*time.Second, g2.ID, 2, u, p)

	// The committer answers a heal for g1 with a Welcome into g2.
	p.requestInvite = func(ctx context.Context, to peer.AddrInfo, request []byte) ([]byte, error) {
		var req inviteRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return nil, err
		}
		req.GuildID = g2.ID
		b, _ := json.Marshal(req)
		return p.host.RequestInvite(ctx, to, b)
	}
	cur, err := p.mls.Epoch(p.ctx, rhGroup(p, g1))
	if err != nil {
		t.Fatal(err)
	}
	p.noteBehind(g1, cur+1)
	p.setOutOfSync(g1, true)
	if ok, why := p.healAsk(g1, u.host.PeerID()); ok {
		t.Fatal("a Welcome into another group was taken as the heal")
	} else if why != "" {
		t.Fatalf("setup: the committer refused the heal (%s); the test needs a Welcome", why)
	}
	// The latch, not the flag: a readable answer from U at P's own epoch may
	// lower the flag at any moment, and only a heal or catching up clears the
	// latch.
	if !p.behindLatched(g1) {
		t.Fatal("a heal into another group cleared the guild's latch")
	}
}
