package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestRemovedAdminCannotSeatAnyone pins the phantom guild: an admin who has been
// kicked still holds the guild's old epoch and, until this, a client that kept
// offering Invite. A newcomer who redeemed that code was welcomed onto the dead
// epoch — a copy of the guild with the real name and member list that only the
// kicked admin could read — and later, when the newcomer's client found the real
// guild unreadable and asked the owner to "re-add" it, the owner's client seated
// them for real. Nobody who could admit them ever had.
//
// Both halves are pinned: the kicked admin's client refuses to mint or serve, and
// a re-add heal from an account no committer ever seated is refused rather than
// served, using an older client's behaviour (serving the phantom anyway) as the
// way in.
func TestRemovedAdminCannotSeatAnyone(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	owner := startService(t, ctx)
	admin := startService(t, ctx)
	bystander := startService(t, ctx)
	newcomer := startService(t, ctx)

	g, err := owner.CreateGuild("Riverside Makers")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	for _, s := range []*Service{admin, bystander} {
		if _, err := s.JoinViaInvite(code); err != nil {
			t.Fatalf("JoinViaInvite: %v", err)
		}
	}
	waitMembers(t, 30*time.Second, 3, owner, admin, bystander)
	adminFpr := admin.id.Fingerprint()
	roleID, err := owner.UpsertRole(g.ID, "", "Admin", "#5865f2", PermManageMembers|PermManageChannels, 5)
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := owner.AssignRole(g.ID, adminFpr, roleID, true); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool { return admin.CanManageMembers(g.ID) }, "the admin role never reached the admin")

	if err := owner.KickMember(g.ID, adminFpr, ""); err != nil {
		t.Fatalf("KickMember: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool { return admin.EvictedFrom(g.ID) != "" }, "the kicked admin never learned it was out")

	// The kicked admin's own client: nothing to invite with, nothing to manage.
	if _, err := admin.InviteCode(g.ID); err == nil {
		t.Fatal("a kicked admin's client still mints invite codes")
	}
	if admin.CanManageMembers(g.ID) {
		t.Fatal("a kicked admin's client still believes it may manage members")
	}
	if p := admin.MemberPermission(g.ID, adminFpr); p != 0 {
		t.Fatalf("a kicked admin's client reports its own permissions as %b, want none", p)
	}
	// And a code pointed at it (an older build would still have handed one out)
	// is refused at its door rather than served onto the dead epoch.
	ai := admin.host.AddrInfo()
	addrs := make([]string, 0, len(ai.Addrs))
	for _, a := range ai.Addrs {
		addrs = append(addrs, a.String())
	}
	staleCode := encodeInviteCode(inviteCode{GuildID: g.ID, GuildName: g.Name, OwnerID: ai.ID.String(), OwnerAddr: addrs})
	if _, err := newcomer.JoinViaInvite(staleCode); err == nil {
		t.Fatal("a kicked admin's client welcomed a newcomer")
	}

	// An older client serves it anyway: the newcomer lands on the phantom.
	kp, err := newcomer.mls.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("KeyPackage: %v", err)
	}
	admin.mu.RLock()
	ag := admin.guilds[g.ID]
	admin.mu.RUnlock()
	welcome, refusal := admin.admit(ag, inviteRequest{GuildID: g.ID, KeyPackage: kp, Credential: newcomer.myCredential}, newcomer.host.PeerID())
	if refusal != "" || len(welcome) == 0 {
		t.Fatalf("could not stage the phantom welcome: %q", refusal)
	}
	if _, err := newcomer.mls.Join(ctx, welcome); err != nil {
		t.Fatalf("newcomer join phantom: %v", err)
	}
	phantom := ag.Clone()
	if err := newcomer.store.SaveGuild(phantom); err != nil {
		t.Fatalf("SaveGuild: %v", err)
	}
	newcomer.trackGuild(&phantom)

	// The newcomer can reach the real owner, finds the guild unreadable, and asks
	// to be re-added — the request that used to fold it in.
	if err := newcomer.host.Connect(ctx, owner.host.AddrInfo()); err != nil {
		t.Fatalf("connect newcomer to owner: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return len(newcomer.authorizedCommittersOnline(g.ID)) > 0
	}, "the newcomer never saw the owner online")
	newcomer.setOutOfSync(g.ID, true)
	newcomer.healOutOfSync(g.ID)

	for i := 0; i < 10; i++ {
		if owner.guildHasMember(g.ID, newcomer.id.Fingerprint()) {
			t.Fatal("the owner seated an account that only a removed member had ever welcomed")
		}
		time.Sleep(300 * time.Millisecond)
	}
	if n, _ := owner.MemberCount(g.ID); n != 2 {
		t.Fatalf("owner's member count is %d, want 2 (owner and bystander)", n)
	}
	if got := newcomer.EvictedFrom(g.ID); got != evictedUnseated {
		t.Fatalf("the newcomer's client says %q about the phantom, want %q", got, evictedUnseated)
	}

	// A first-time join through a real invite is untouched by any of this: the
	// refusal is for heals, not for people holding a code.
	if _, err := newcomer.JoinViaInvite(code); err != nil {
		t.Fatalf("a real invite no longer seats the newcomer: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool { return owner.guildHasMember(g.ID, newcomer.id.Fingerprint()) }, "the real invite never seated the newcomer")
	if newcomer.EvictedFrom(g.ID) != "" {
		t.Fatal("a real welcome did not lift the unseated state")
	}
}

// TestHealRequestFromUnseatedAccountIsRefused is the responder rule on its own:
// a heal names itself, and a committer serves one only to an account its tree
// already holds.
func TestHealRequestFromUnseatedAccountIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	stranger := startService(t, ctx)
	g, err := owner.CreateGuild("Quiet Room")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	kp, err := stranger.mls.KeyPackage(ctx)
	if err != nil {
		t.Fatalf("KeyPackage: %v", err)
	}
	req, _ := json.Marshal(inviteRequest{GuildID: g.ID, KeyPackage: kp, Credential: stranger.myCredential, Heal: true})
	raw, err := owner.handleInviteRequest(ctx, stranger.host.PeerID(), req)
	if err != nil {
		t.Fatalf("handleInviteRequest: %v", err)
	}
	var resp inviteResponse
	_ = json.Unmarshal(raw, &resp)
	if resp.Error != unseatedRefusal || len(resp.Welcome) != 0 {
		t.Fatalf("a heal from a stranger got %q (welcome %d bytes), want the unseated refusal", resp.Error, len(resp.Welcome))
	}
}
