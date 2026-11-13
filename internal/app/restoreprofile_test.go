package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A recovery phrase is the account key and nothing else. These tests restore
// it onto an empty device, with the original device gone, and require the
// account's profile to come back without anybody typing it — from the two
// places that can honestly hand it over: another device of the account, and a
// guild member holding the record the account announced to them.

// restoreOnto restores phrase into a fresh data dir and starts it.
func restoreOnto(t *testing.T, ctx context.Context, phrase, boot string) *Service {
	t.Helper()
	dir := t.TempDir()
	if err := RestoreIdentity(dir, phrase, "test-pass"); err != nil {
		t.Fatalf("RestoreIdentity: %v", err)
	}
	return startServiceOn(t, ctx, dir, boot)
}

// pictureProfile is a profile whose pictures are the size real ones are: past
// the 64 KiB a hello frame may carry.
func pictureProfile(status string) Profile {
	p := sentinelProfile(status)
	p.Avatar = "data:image/png;base64," + strings.Repeat("QUJD", 10<<10)  // 40 KiB
	p.Banner = "data:image/jpeg;base64," + strings.Repeat("REVG", 30<<10) // 120 KiB
	return p
}

// TestRestoredDeviceGetsProfileFromLinkedDevice: the desktop is lost, the
// phone is still on. A device restored from the phrase must come back as the
// account. It used to, for a profile without pictures; with an avatar and a
// banner the phone's hello outgrew its frame and was refused whole — profile,
// guild invites and all.
func TestRestoredDeviceGetsProfileFromLinkedDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	waitDevice(t, desk, phone.PeerID())
	want := pictureProfile("restored from the phone")
	if err := desk.SetProfile(want); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return profileMatches(phone.SelfProfile(), want)
	}, "the phone never had the profile to give back")
	phrase, err := desk.Mnemonic()
	if err != nil {
		t.Fatal(err)
	}
	_ = desk.Close()

	restored := restoreOnto(t, ctx, phrase, boot)
	start := time.Now()
	waitUntil(t, 60*time.Second, func() bool {
		return profileMatches(restored.SelfProfile(), want)
	}, "a device restored from the phrase never got its profile from the linked phone")
	t.Logf("profile back from the linked device after %v", time.Since(start).Round(100*time.Millisecond))
	waitUntil(t, 60*time.Second, func() bool {
		return len(restored.Guilds()) > 0
	}, "the restored device never got the guild hand-over")
}

// TestRestoredDeviceGetsProfileFromGuildMate: the account's only device is
// gone; a guild member is online. The member holds the record the account
// announced, sealed so only the account can check it, and hands it back.
func TestRestoredDeviceGetsProfileFromGuildMate(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	friend := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := owner.CreateGuild("Keepers")
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
	want := pictureProfile("kept by a friend")
	if err := owner.SetProfile(want); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		return profileMatches(friend.ProfileOf(owner.Fingerprint()), want)
	}, "the friend never learned the profile")
	phrase, err := owner.Mnemonic()
	if err != nil {
		t.Fatal(err)
	}
	// Let the announce that carried the record settle before the owner goes.
	time.Sleep(2 * time.Second)
	_ = owner.Close()

	restored := restoreOnto(t, ctx, phrase, boot)
	start := time.Now()
	waitUntil(t, 90*time.Second, func() bool {
		return profileMatches(restored.SelfProfile(), want)
	}, "a device restored from the phrase never got its profile back from a guild member")
	t.Logf("profile back from the guild member after %v", time.Since(start).Round(100*time.Millisecond))
	// What came back is the account's, not the friend's say-so about it: the
	// friend's own profile is untouched and nothing of the friend's leaked in.
	if restored.SelfProfile().Name == friend.SelfProfile().Name && friend.SelfProfile().Name != want.Name {
		t.Fatal("the restored device took the friend's own profile")
	}
}
