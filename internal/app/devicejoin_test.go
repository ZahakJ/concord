package app

import (
	"context"
	"net"
	"testing"
	"time"
)

func holdsGuild(s *Service, id string) bool {
	for _, g := range s.Guilds() {
		if g.ID == id {
			return true
		}
	}
	return false
}

// A guild the account joins on its original device must show up on its
// linked device too, and the other way round. The original device is not the
// guild's owner, so it cannot admit its sibling itself; what it hands over is
// a code addressed to the owner.
func TestGuildJoinedOnOneDeviceReachesTheOther(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	cy := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := cy.SetDisplayName("Cy"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	for _, tc := range []struct {
		name     string
		joiner   *Service
		sibling  *Service
		guildNam string
	}{
		{"joined on the original device", desk, phone, "Cy's Den"},
		{"joined on the linked device", phone, desk, "Cy's Club"},
	} {
		g, err := cy.CreateGuild(tc.guildNam)
		if err != nil {
			t.Fatalf("CreateGuild: %v", err)
		}
		code, err := cy.InviteCode(g.ID)
		if err != nil {
			t.Fatalf("InviteCode: %v", err)
		}
		if _, err := tc.joiner.JoinViaInvite(code); err != nil {
			t.Fatalf("%s: JoinViaInvite: %v", tc.name, err)
		}
		waitUntil(t, 60*time.Second, func() bool { return holdsGuild(tc.sibling, g.ID) },
			tc.name+": the account's other device never got the guild")
	}
}

// The offer reaches the other device once per connection between them, and
// that connection usually outlives whatever made the join fail. Here the
// guild's owner is offline when the linked device gets the offer; once the
// owner is back, the device must get in without either device restarting.
func TestOfferedGuildIsRetriedOnceTheOwnerIsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	phoneDir := t.TempDir()
	desk, phone, _, _ := linkedPair(t, ctx, t.TempDir(), phoneDir, boot)

	// The owner comes back on the port it left from, as a desktop with a
	// pinned port does, so the code the desk hands over still names it.
	cyDir := t.TempDir()
	if err := SaveListenPort(cyDir, freePort(t)); err != nil {
		t.Fatalf("SaveListenPort: %v", err)
	}
	cy := startServiceOn(t, ctx, cyDir, boot)
	if err := cy.SetDisplayName("Cy"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := cy.CreateGuild("Cy's Den")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := cy.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}

	// The phone is away while the desk joins, so the offer it gets is the one
	// its reconnect hello carries, and by then the owner has gone offline.
	_ = phone.Close()
	if _, err := desk.JoinViaInvite(code); err != nil {
		t.Fatalf("desk JoinViaInvite: %v", err)
	}
	_ = cy.Close()
	phone = startServiceOn(t, ctx, phoneDir, boot)
	waitUntil(t, 60*time.Second, func() bool {
		phone.joiningMu.Lock()
		defer phone.joiningMu.Unlock()
		_, failed := phone.offeredRetry[g.ID]
		return failed
	}, "the phone never tried the offered guild, or did not keep it to try again")
	if holdsGuild(phone, g.ID) {
		t.Fatal("the phone joined through an owner that was offline")
	}

	startServiceOn(t, ctx, cyDir, boot)
	waitUntil(t, 90*time.Second, func() bool { return holdsGuild(phone, g.ID) },
		"the phone never got the guild once the owner was back")
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
