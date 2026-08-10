package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// The user's profile — name, status line, presence, bio, avatar, colors,
// banner, frame/effect/style, games — is ACCOUNT state, not device state.
// These tests pin every leg of its travel:
//
//   - device A edits → device B of the same account shows it: live, after a
//     restart of B, and when B was OFFLINE at the time of the edit;
//   - a freshly linked device inherits the account's current profile;
//   - a guild friend sees the edit live, and a friend who was offline for it
//     converges when they come back;
//   - and none of it is spoofable: a profile only ever binds to the peer that
//     MLS/certificate authentication says authored it.

// sentinelProfile is a full profile touching every user-facing field.
func sentinelProfile(status string) Profile {
	return Profile{
		Name:     "Avicenna Prime",
		Status:   status,
		Emoji:    "\U0001F319", // 🌙
		Color:    "#aabbcc",
		Color2:   "#112233",
		Avatar:   "data:image/png;base64,AAAA",
		Banner:   "preset:dusk",
		Presence: "idle",
		Bio:      "polymath, occasional physician",
		Frame:    "gold",
		Effect:   "sparkle",
		Style:    &Style{Speed: "slow", Dir: "cw"},
	}
}

// profileMatches compares the fields a human actually set (not MailboxPub or
// Activity, which are device-derived).
func profileMatches(got, want Profile) bool {
	return got.Name == want.Name && got.Status == want.Status &&
		got.Emoji == want.Emoji && got.Color == want.Color &&
		got.Color2 == want.Color2 && got.Avatar == want.Avatar &&
		got.Banner == want.Banner && got.Presence == want.Presence &&
		got.Bio == want.Bio && got.Frame == want.Frame &&
		got.Effect == want.Effect
}

func hasGame(games []Game, name string) bool {
	for _, g := range games {
		if g.Name == name {
			return true
		}
	}
	return false
}

// TestProfileEditReachesLinkedDeviceLive: change everything on the desktop
// while the phone is online — the phone's OWN profile must become the new one
// (it presents as the same person), and it must still be there after the phone
// restarts.
func TestProfileEditReachesLinkedDeviceLive(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	phoneDir := t.TempDir()
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), phoneDir, boot)

	// Make sure the two devices have actually met (the hello ran) before the
	// edit, so this is the LIVE leg, not the reconnect leg.
	waitDevice(t, desk, phone.PeerID())

	want := sentinelProfile("on the road")
	if err := desk.SetProfile(want); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := desk.SetGames([]Game{{Name: "Disco Elysium"}}); err != nil {
		t.Fatalf("SetGames: %v", err)
	}

	waitUntil(t, 60*time.Second, func() bool {
		return profileMatches(phone.SelfProfile(), want)
	}, "the phone never adopted the profile edited on the desktop")
	waitUntil(t, 30*time.Second, func() bool {
		return hasGame(phone.SelfProfile().Games, "Disco Elysium")
	}, "the game collection never reached the phone")

	// Restart the phone: the adopted profile must be its own persisted state,
	// not something it re-learns from luck.
	_ = phone.Close()
	phone2 := startServiceOn(t, ctx, phoneDir, boot)
	if got := phone2.SelfProfile(); !profileMatches(got, want) {
		t.Fatalf("after restart the phone's profile regressed: got %+v", got)
	}
}

// TestProfileEditReachesOfflineLinkedDeviceOnReconnect: the phone is OFF when
// the desktop edits. It must converge when it comes back — via the hello /
// sync catch-up, since the gossip announce it missed is never replayed.
func TestProfileEditReachesOfflineLinkedDeviceOnReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	phoneDir := t.TempDir()
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), phoneDir, boot)
	waitDevice(t, desk, phone.PeerID())
	_ = phone.Close()

	want := sentinelProfile("changed while you were away")
	if err := desk.SetProfile(want); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}

	phone2 := startServiceOn(t, ctx, phoneDir, boot)
	waitUntil(t, 60*time.Second, func() bool {
		return profileMatches(phone2.SelfProfile(), want)
	}, "a phone that was offline for the edit never converged on reconnect")
}

// TestFreshlyLinkedDeviceInheritsAccountProfile: the link handover must carry
// the account's CURRENT profile (all of it, games included), and the new
// device must end up presenting it — without waiting for the user to edit
// something first.
func TestFreshlyLinkedDeviceInheritsAccountProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk := startServiceOn(t, ctx, t.TempDir(), boot)
	want := sentinelProfile("linked and looking sharp")
	if err := desk.SetProfile(want); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if err := desk.SetGames([]Game{{Name: "Outer Wilds"}}); err != nil {
		t.Fatalf("SetGames: %v", err)
	}
	if _, err := desk.CreateGuild("Shared"); err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	code, err := desk.LinkOffer()
	if err != nil {
		t.Fatalf("LinkOffer: %v", err)
	}
	phoneDir := t.TempDir()
	res, err := RedeemLink(ctx, phoneDir, code, "test-pass")
	if err != nil {
		t.Fatalf("RedeemLink: %v", err)
	}
	// The handover snapshot itself must be complete…
	if !profileMatches(res.Profile, want) {
		t.Fatalf("link handover profile is incomplete: got %+v", res.Profile)
	}
	if !hasGame(res.Profile.Games, "Outer Wilds") {
		t.Fatal("link handover dropped the game collection")
	}

	// …and the device must converge even if the caller applies nothing by
	// hand: the hello exchange with its own account is the safety net.
	phone := startServiceOn(t, ctx, phoneDir, boot)
	for _, ic := range res.GuildInvites {
		if _, err := phone.JoinViaInvite(ic); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitUntil(t, 60*time.Second, func() bool {
		return profileMatches(phone.SelfProfile(), want)
	}, "a freshly linked device started blank instead of inheriting the profile")
}

// TestProfileEditReachesFriendWhoWasOffline: the friend legs. Live gossip
// covers a connected friend; a friend who was OFFLINE for the edit must pick
// it up when they return (re-announce on connect + sync roster).
func TestProfileEditReachesFriendWhoWasOffline(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	friendDir := t.TempDir()
	friend := startServiceOn(t, ctx, friendDir, boot)

	g, err := owner.CreateGuild("Meadow")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 2
	}, "the friend never joined")

	// Live leg: the connected friend sees the edit.
	live := sentinelProfile("hello friends")
	if err := owner.SetProfile(live); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return profileMatches(friend.ProfileOf(owner.Fingerprint()), live)
	}, "a connected friend never saw the profile edit")

	// Offline leg: the friend goes away, the profile changes, the friend
	// returns and must converge.
	_ = friend.Close()
	later := sentinelProfile("moved to Hamadan")
	if err := owner.SetProfile(later); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	friend2 := startServiceOn(t, ctx, friendDir, boot)
	waitUntil(t, 60*time.Second, func() bool {
		return profileMatches(friend2.ProfileOf(owner.Fingerprint()), later)
	}, "a friend who was offline for the edit never converged after returning")
}

// TestProfileForgeriesAreRefused pins the receive-side gates on every lane a
// profile can arrive by. Each is checked on the RECEIVING peer against
// MLS/certificate-authenticated identity, so a patched client convinces
// nobody but itself.
func TestProfileForgeriesAreRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	friend := startServiceOn(t, ctx, t.TempDir(), boot)

	mine := sentinelProfile("mine, not yours")
	if err := owner.SetProfile(mine); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	g, err := owner.CreateGuild("Trusting")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 2
	}, "the friend never joined")

	evil := sentinelProfile("pwned")
	evil.Name = "Definitely The Owner"
	// A stamp from the far future: if any gate below falls through to the
	// last-writer-wins comparison, the forgery wins and the test catches it.
	evil.UpdatedAt = time.Now().UnixMilli() + (1 << 40)

	// Lane 1, gossip announce: a member may only speak for its OWN
	// fingerprint. The frame names a third party; the authenticated actor is
	// the friend — it must be dropped, not bound to the named target.
	target := "THIRD-PARTY-FPR"
	owner.applyProfileMeta(g.ID, friend.Fingerprint(), guildMeta{
		Type: "profile", Fingerprint: target, Name: "evil", UpdatedAt: evil.UpdatedAt,
	})
	if got := owner.ProfileOf(target).Name; got != "" {
		t.Fatalf("a member bound a profile to someone else's fingerprint: %q", got)
	}

	// Lane 2, device hello: only a peer PROVEN to be a device of this very
	// account may move this device's own profile. The friend is authenticated,
	// connected, a guild member — and still refused.
	owner.adoptOfferedProfile(friend.host.PeerID(), &evil)
	if got := owner.SelfProfile(); got.Status == "pwned" || got.Name == evil.Name {
		t.Fatalf("a friend's hello rewrote our own profile: %+v", got)
	}

	// Lane 3, sync roster: a served backfill containing OUR OWN fingerprint
	// must not move our profile unless the server is one of our own devices.
	payload := syncPayload{
		Guild:    domain.Guild{ID: g.ID},
		Profiles: map[string]Profile{owner.Fingerprint(): evil},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	ct, err := friend.mls.Encrypt(friend.ctx, g.GroupID, raw)
	if err != nil {
		t.Fatalf("encrypt forged payload: %v", err)
	}
	if !owner.applySyncPayload(g.ID, g.GroupID, ct, friend.Fingerprint()) {
		t.Fatal("the forged payload was not even readable — test setup broken")
	}
	if got := owner.SelfProfile(); got.Status == "pwned" || got.Name == evil.Name {
		t.Fatalf("a guild member's sync backfill rewrote our own profile: %+v", got)
	}

	// Lane 4, stale relay: once a stamped profile is learned, an OLDER stamped
	// copy (a peer that slept through the edit re-serving its cache) must not
	// roll it back.
	relayFpr := "SOME-MUTUAL-FRIEND"
	owner.learnProfile(relayFpr, Profile{Name: "Mutual", Status: "new status", UpdatedAt: 200})
	owner.learnProfile(relayFpr, Profile{Name: "Mutual", Status: "old status", UpdatedAt: 100})
	if got := owner.ProfileOf(relayFpr).Status; got != "new status" {
		t.Fatalf("a stale relayed profile rolled back a newer one: %q", got)
	}
}
