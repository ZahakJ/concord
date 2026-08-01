package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEventPropagatesAndRSVP: an event created by one member reaches every
// other; a member's RSVP travels back bound to their identity; and the
// edit/delete permission is enforced on the RECEIVE side as well as the local
// side — a forged upsert or removal pushed as meta by a non-author without
// ManageMessages must change nothing on honest peers.
func TestEventPropagatesAndRSVP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	member := startServiceOn(t, ctx, t.TempDir(), boot)

	g, err := owner.CreateGuild("Events")
	if err != nil {
		t.Fatal(err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("join: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		n, _ := owner.MemberCount(g.ID)
		return n == 2
	}, "the member never joined")

	start := time.Now().Add(24 * time.Hour).Unix()
	ev, err := owner.CreateEvent(g.ID, "Game night", "Bring snacks", start, start+7200, "the lounge")
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		evs, _ := member.Events(g.ID)
		return len(evs) == 1 && evs[0].ID == ev.ID && evs[0].Title == "Game night" &&
			evs[0].CreatedBy == owner.id.Fingerprint()
	}, "the event never reached the member")

	// The member RSVPs; the owner must see the answer under the MEMBER'S
	// fingerprint — the state travels with the event, one entry per account.
	if err := member.RSVP(g.ID, ev.ID, "going"); err != nil {
		t.Fatalf("rsvp: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		evs, _ := owner.Events(g.ID)
		return len(evs) == 1 && evs[0].RSVPs[member.id.Fingerprint()] == "going"
	}, "the RSVP never reached the owner")

	// The member is not the author and holds no ManageMessages: local calls
	// must refuse…
	if _, err := member.UpdateEvent(g.ID, ev.ID, "Hijacked", "", start, start+7200, ""); err == nil {
		t.Fatal("a non-author without ManageMessages edited an event locally")
	}
	if err := member.DeleteEvent(g.ID, ev.ID); err == nil {
		t.Fatal("a non-author without ManageMessages deleted an event locally")
	}
	// …and forged meta applications must be refused by the receive gate.
	forged := ev
	forged.Title = "Forged"
	owner.applyEventUpsert(g.ID, member.id.Fingerprint(), forged)
	owner.applyEventRemove(g.ID, member.id.Fingerprint(), ev.ID)
	evs, err := owner.Events(g.ID)
	if err != nil || len(evs) != 1 {
		t.Fatalf("a forged removal from a non-privileged member was applied: %v %d", err, len(evs))
	}
	if evs[0].Title != "Game night" {
		t.Fatalf("a forged edit from a non-privileged member was applied: %q", evs[0].Title)
	}

	// The author's real edit propagates — and must NOT wipe the RSVP, which
	// travels its own lane.
	if _, err := owner.UpdateEvent(g.ID, ev.ID, "Game night II", "Bring snacks", start, start+7200, "the lounge"); err != nil {
		t.Fatalf("author edit: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		evs, _ := member.Events(g.ID)
		return len(evs) == 1 && evs[0].Title == "Game night II" &&
			evs[0].RSVPs[member.id.Fingerprint()] == "going"
	}, "the author's edit never reached the member (or wiped the RSVP)")

	// The service-level ICS export carries the event under its stable UID.
	ics, err := owner.EventICS(g.ID, ev.ID)
	if err != nil {
		t.Fatalf("event ics: %v", err)
	}
	if !strings.Contains(ics, "UID:"+ev.ID+"@concord\r\n") ||
		!strings.Contains(ics, "SUMMARY:Game night II\r\n") {
		t.Fatalf("ics export missing UID/SUMMARY:\n%q", ics)
	}
}

// TestEventFreshJoinerConverges: a member who joins AFTER an event exists
// never saw the event_upserted gossip, so the only road to convergence is the
// history-sync snapshot — the same one that hands them the channel list.
func TestEventFreshJoinerConverges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)

	g, err := owner.CreateGuild("Later")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(48 * time.Hour).Unix()
	ev, err := owner.CreateEvent(g.ID, "Launch party", "", start, 0, "")
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if err := owner.RSVP(g.ID, ev.ID, "going"); err != nil {
		t.Fatalf("owner rsvp: %v", err)
	}

	joiner := startServiceOn(t, ctx, t.TempDir(), boot)
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := joiner.JoinViaInvite(code); err != nil {
		t.Fatalf("join: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		evs, _ := joiner.Events(g.ID)
		return len(evs) == 1 && evs[0].ID == ev.ID && evs[0].Title == "Launch party" &&
			evs[0].CreatedBy == owner.id.Fingerprint() &&
			evs[0].RSVPs[owner.id.Fingerprint()] == "going"
	}, "the fresh joiner never converged on the pre-existing event (with its RSVP)")
}
