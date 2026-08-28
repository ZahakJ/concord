package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// These tests pin the guest-opened-event contract: opening mints a knock-gated
// meeting room whose lifetime is bounded by the event, the link is stable
// across edits, revoke kills both link and room, the ICS export carries the
// link, and the guest fields on the event obey their host on the receive side.

// eventForGuests makes a guild + a near-future event and returns both.
func eventForGuests(t *testing.T, s *Service) (domain.Guild, domain.Event) {
	t.Helper()
	g, err := s.CreateGuild("Club")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	start := time.Now().Add(2 * time.Hour).Unix()
	ev, err := s.CreateEvent(g.ID, "Game night", "Bring snacks", start, start+7200, "the lounge", "", "", 0)
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	return g, ev
}

func guestTokenOf(t *testing.T, url string) string {
	t.Helper()
	i := strings.Index(url, "&t=")
	if i < 0 {
		t.Fatalf("guest link has no token: %q", url)
	}
	return url[i+3:]
}

// TestEventGuestOpenKnockSeedAndAutoAdmit drives the whole host-side flow: the
// mint, the room's expiry, the knock default, the seeded context message, the
// stable URL on re-open, and the auto-admit choice.
func TestEventGuestOpenKnockSeedAndAutoAdmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1")
	s := startService(t, ctx)

	g, ev := eventForGuests(t, s)
	opened, err := s.OpenEventGuests(g.ID, ev.ID, false)
	if err != nil {
		t.Fatalf("OpenEventGuests: %v", err)
	}
	if !strings.Contains(opened.GuestURL, "/guest#h=") {
		t.Fatalf("no guest link on the event: %q", opened.GuestURL)
	}
	if opened.GuestHost != s.id.Fingerprint() {
		t.Fatalf("guest host is %q, want this account", opened.GuestHost)
	}
	// The members' door is minted alongside the guest link, and it is a real
	// invite into the meeting room — decoding it must name that room.
	if opened.MemberCode == "" {
		t.Fatal("no member join code on the opened event")
	}
	ic, err := decodeInviteCode(opened.MemberCode)
	if err != nil {
		t.Fatalf("member code does not decode: %v", err)
	}

	s.eventGuestMu.Lock()
	rec, ok := s.eventGuests[ev.ID]
	s.eventGuestMu.Unlock()
	if !ok {
		t.Fatal("no local record of the hosted room")
	}
	s.mu.RLock()
	room, alive := s.guilds[rec.MeetingGuildID]
	s.mu.RUnlock()
	if !alive || room.Kind != "meeting" {
		t.Fatalf("guest room missing or not a meeting: alive=%v kind=%q", alive, room.Kind)
	}
	if ic.GuildID != rec.MeetingGuildID {
		t.Fatalf("member code points at %q, want the meeting room %q", ic.GuildID, rec.MeetingGuildID)
	}
	// The HOST tapping Join must land in the room they already own — no
	// network round-trip, no self-dial, just the guild back.
	joined, err := s.JoinEventRoom(g.ID, ev.ID)
	if err != nil {
		t.Fatalf("host JoinEventRoom: %v", err)
	}
	if joined.ID != rec.MeetingGuildID {
		t.Fatalf("host joined %q, want their own room %q", joined.ID, rec.MeetingGuildID)
	}
	if !strings.Contains(room.Name, "Game night") {
		t.Fatalf("room not named for the event: %q", room.Name)
	}
	// Lifetime: the room persists well past the event — anchored to now, not the
	// scheduled end, so a meeting a while ago is still joinable.
	if got, want := s.meetingExpiry(rec.MeetingGuildID).Unix(), time.Now().Add(eventGuestKeepOpen).Unix(); got < want-120 || got > want+120 {
		t.Fatalf("room expiry %d, want ~now+keepOpen %d", got, want)
	}

	// Default door: a guest KNOCKS, is admitted by the host, and lands on the
	// seeded context message.
	token := guestTokenOf(t, opened.GuestURL)
	channelID := room.Channels[0].ID
	r, c := guestPipe(t, s)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: token, Name: "Rivka"})
	frames := readGuestFrames(t, r, c, "waiting", "welcome", "end")
	if last := frames[len(frames)-1]; last.Type != "waiting" {
		t.Fatalf("event guest got %q on arrival, want a knock (waiting)", last.Type)
	}
	var fpr string
	s.guestMu.Lock()
	for _, sess := range s.guestSessions[channelID] {
		fpr = sess.fpr
	}
	s.guestMu.Unlock()
	if fpr == "" {
		t.Fatal("no knocking session registered")
	}
	if err := s.PublishCallControl(channelID, "admit", fpr, ""); err != nil {
		t.Fatalf("admit: %v", err)
	}
	got := readGuestFrames(t, r, c, "welcome", "end")
	if last := got[len(got)-1]; last.Type != "welcome" {
		t.Fatalf("after admission the guest got %q, want welcome", last.Type)
	}
	// The history replay must include the seeded event context.
	seeded := false
	for deadline := time.Now().Add(5 * time.Second); !seeded && time.Now().Before(deadline); {
		for _, f := range readGuestFrames(t, r, c, "sys", "end") {
			if f.Type == "end" {
				t.Fatalf("session ended before the seed arrived: %s", f.Reason)
			}
			if f.Type == "sys" && strings.Contains(f.Content, "Game night") && strings.Contains(f.Content, "the lounge") {
				seeded = true
			}
		}
	}
	if !seeded {
		t.Fatal("admitted guest never saw the seeded event context")
	}

	// Re-open with auto-admit: SAME link, open door.
	reopened, err := s.OpenEventGuests(g.ID, ev.ID, true)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if reopened.GuestURL != opened.GuestURL {
		t.Fatalf("re-opening rotated the link: %q -> %q", opened.GuestURL, reopened.GuestURL)
	}
	r2, c2 := guestPipe(t, s)
	sendGuestFrame(t, c2, guestFrame{Type: "hello", Token: token, Name: "Yusuf"})
	f2 := readGuestFrames(t, r2, c2, "welcome", "waiting", "end")
	if last := f2[len(f2)-1]; last.Type != "welcome" {
		t.Fatalf("auto-admit guest got %q, want welcome straight away", last.Type)
	}
}

// TestEventGuestLinkStableAcrossEdits: editing the event keeps the link and
// drags the room's lifetime along with the new times.
func TestEventGuestLinkStableAcrossEdits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1")
	s := startService(t, ctx)

	g, ev := eventForGuests(t, s)
	opened, err := s.OpenEventGuests(g.ID, ev.ID, false)
	if err != nil {
		t.Fatalf("OpenEventGuests: %v", err)
	}
	newStart := time.Now().Add(26 * time.Hour).Unix()
	edited, err := s.UpdateEvent(g.ID, ev.ID, "Game night (moved)", "Now with pizza", newStart, newStart+3600, "the lounge", "", "", 0)
	if err != nil {
		t.Fatalf("UpdateEvent: %v", err)
	}
	if edited.GuestURL != opened.GuestURL || edited.GuestHost != opened.GuestHost {
		t.Fatalf("edit changed the guest link: %+q -> %+q", opened.GuestURL, edited.GuestURL)
	}
	if edited.MemberCode != opened.MemberCode {
		t.Fatalf("edit changed the member join code: %+q -> %+q", opened.MemberCode, edited.MemberCode)
	}
	s.eventGuestMu.Lock()
	rec := s.eventGuests[ev.ID]
	s.eventGuestMu.Unlock()
	want := time.Now().Add(eventGuestKeepOpen).Unix()
	if got := s.meetingExpiry(rec.MeetingGuildID).Unix(); got < want-120 || got > want+120 {
		t.Fatalf("room expiry after re-open %d, want ~now+keepOpen %d", got, want)
	}
	// The token's own clock must agree, or the door shuts early (serveGuest
	// checks both).
	token := guestTokenOf(t, opened.GuestURL)
	s.guestMu.Lock()
	tok, ok := s.guestTokens[token]
	s.guestMu.Unlock()
	if !ok || tok.Expires.Unix() != want {
		t.Fatalf("token expiry %v (ok=%v), want %d", tok.Expires.Unix(), ok, want)
	}
}

// TestEventGuestRevokeTearsDown: revoke clears the event, deletes the room,
// and the link answers "no longer valid".
func TestEventGuestRevokeTearsDown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1")
	s := startService(t, ctx)

	g, ev := eventForGuests(t, s)
	opened, err := s.OpenEventGuests(g.ID, ev.ID, true)
	if err != nil {
		t.Fatalf("OpenEventGuests: %v", err)
	}
	s.eventGuestMu.Lock()
	roomID := s.eventGuests[ev.ID].MeetingGuildID
	s.eventGuestMu.Unlock()

	revoked, err := s.RevokeEventGuests(g.ID, ev.ID)
	if err != nil {
		t.Fatalf("RevokeEventGuests: %v", err)
	}
	if revoked.GuestURL != "" || revoked.GuestHost != "" || revoked.MemberCode != "" {
		t.Fatalf("revoke left room fields on the event: %+v", revoked)
	}
	// Join after revoke fails HONESTLY — no room, said plainly.
	if _, err := s.JoinEventRoom(g.ID, ev.ID); err == nil || !strings.Contains(err.Error(), "no room") {
		t.Fatalf("Join after revoke: %v, want a plain 'no room to join'", err)
	}
	s.mu.RLock()
	_, alive := s.guilds[roomID]
	s.mu.RUnlock()
	if alive {
		t.Fatal("revoke left the guest room standing")
	}
	s.eventGuestMu.Lock()
	_, still := s.eventGuests[ev.ID]
	s.eventGuestMu.Unlock()
	if still {
		t.Fatal("revoke left the local record")
	}

	token := guestTokenOf(t, opened.GuestURL)
	r, c := guestPipe(t, s)
	sendGuestFrame(t, c, guestFrame{Type: "hello", Token: token, Name: "Late"})
	frames := readGuestFrames(t, r, c, "end", "welcome", "waiting")
	if last := frames[len(frames)-1]; last.Type != "end" || !strings.Contains(last.Reason, "no longer valid") {
		t.Fatalf("revoked link answered %q (%q), want a clean refusal", last.Type, last.Reason)
	}

	// Deleting an event with a live room also tears it down.
	g2, ev2 := eventForGuests(t, s)
	if _, err := s.OpenEventGuests(g2.ID, ev2.ID, false); err != nil {
		t.Fatalf("OpenEventGuests: %v", err)
	}
	s.eventGuestMu.Lock()
	room2 := s.eventGuests[ev2.ID].MeetingGuildID
	s.eventGuestMu.Unlock()
	if err := s.DeleteEvent(g2.ID, ev2.ID); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
	s.mu.RLock()
	_, alive = s.guilds[room2]
	s.mu.RUnlock()
	if alive {
		t.Fatal("deleting the event left its guest room standing")
	}
}

// TestEventICSCarriesGuestLink: "Add to calendar" must hand the invitee the
// join link, as both the URL property and inside DESCRIPTION.
func TestEventICSCarriesGuestLink(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1")
	s := startService(t, ctx)

	g, ev := eventForGuests(t, s)
	opened, err := s.OpenEventGuests(g.ID, ev.ID, false)
	if err != nil {
		t.Fatalf("OpenEventGuests: %v", err)
	}
	ics, err := s.EventICS(g.ID, ev.ID)
	if err != nil {
		t.Fatalf("EventICS: %v", err)
	}
	// Unfold before matching: RFC 5545 folds long lines, and the link is long.
	flat := strings.ReplaceAll(ics, "\r\n ", "")
	if !strings.Contains(flat, "URL:"+strings.ReplaceAll(opened.GuestURL, ",", `\,`)) {
		t.Fatalf("ICS has no URL property with the guest link:\n%s", ics)
	}
	if !strings.Contains(flat, "Join from your browser") {
		t.Fatalf("ICS description does not mention the join link:\n%s", ics)
	}
	// Without a link the properties stay out.
	if _, err := s.RevokeEventGuests(g.ID, ev.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	ics2, _ := s.EventICS(g.ID, ev.ID)
	if strings.Contains(ics2, "URL:") {
		t.Fatalf("revoked event still exports a URL:\n%s", ics2)
	}
}

// TestEventGuestFieldsObeyTheirHost is the receive side: only the recorded
// guest host's frames may set, change or clear the guest fields — an author's
// or moderator's edit carries them through, and nobody can claim a room in
// someone else's name.
func TestEventGuestFieldsObeyTheirHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked service test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1")
	s := startService(t, ctx)

	g, err := s.CreateGuild("Club")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	// The event arrives authored by ANOTHER member (the create lane binds
	// authorship to the actor), so that member may edit it below — while WE,
	// as guild owner, open it to guests and become the recorded host.
	ev := domain.Event{
		ID: domain.NewID(), Title: "Game night", Details: "Bring snacks",
		StartUnix: time.Now().Add(2 * time.Hour).Unix(),
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	ev.EndUnix = ev.StartUnix + 7200
	s.applyEventUpsert(g.ID, "attacker-fpr", ev)
	stored, found, _ := s.store.EventByID(ev.ID)
	if !found || stored.CreatedBy != "attacker-fpr" {
		t.Fatalf("setup: event not created under the other member: %+v", stored)
	}
	opened, err := s.OpenEventGuests(g.ID, ev.ID, false)
	if err != nil {
		t.Fatalf("OpenEventGuests: %v", err)
	}
	stored, _, _ = s.store.EventByID(ev.ID)

	// 1) The author edits from a stale copy carrying no guest fields: the live
	// link AND the member join code must survive.
	forged := stored
	forged.Title = "Game night (attacker edit)"
	forged.GuestURL, forged.GuestHost, forged.MemberCode = "", "", ""
	forged.UpdatedAt = stored.UpdatedAt + 10
	s.applyEventUpsert(g.ID, "attacker-fpr", forged)
	after, _, _ := s.store.EventByID(ev.ID)
	if after.Title != "Game night (attacker edit)" {
		t.Fatalf("legitimate edit did not apply: %q", after.Title)
	}
	if after.GuestURL != opened.GuestURL || after.GuestHost != opened.GuestHost {
		t.Fatal("a non-host edit killed the guest link")
	}
	if after.MemberCode != opened.MemberCode {
		t.Fatal("a non-host edit killed the member join code")
	}

	// 2) The author tries to re-point guests (and members) at another room.
	forged = after
	forged.GuestURL = "https://evil.example/guest#h=x&t=y"
	forged.GuestHost = "attacker-fpr"
	forged.MemberCode = "CI1evilcode"
	forged.UpdatedAt = after.UpdatedAt + 10
	s.applyEventUpsert(g.ID, "attacker-fpr", forged)
	after, _, _ = s.store.EventByID(ev.ID)
	if after.GuestURL != opened.GuestURL || after.GuestHost != opened.GuestHost {
		t.Fatalf("a non-host re-pointed the guest link: %q", after.GuestURL)
	}
	if after.MemberCode != opened.MemberCode {
		t.Fatalf("a non-host re-pointed the member join code: %q", after.MemberCode)
	}

	// 3) A fresh record cannot arrive claiming somebody else hosts a room —
	// neither door survives the create gate.
	alien := domain.Event{
		ID: domain.NewID(), Title: "Planted", StartUnix: time.Now().Add(time.Hour).Unix(),
		GuestURL: "https://evil.example/guest#h=x&t=y", GuestHost: "innocent-bystander",
		MemberCode: "CI1evilcode",
		CreatedAt:  time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	s.applyEventUpsert(g.ID, "attacker-fpr", alien)
	planted, found, _ := s.store.EventByID(alien.ID)
	if !found {
		t.Fatal("benign part of the create did not apply")
	}
	if planted.GuestURL != "" || planted.GuestHost != "" || planted.MemberCode != "" {
		t.Fatal("a create claimed a room in someone else's name")
	}

	// 4) The host's own frame (e.g. their linked device revoking) clears the
	// fields AND tears the room down here, where it lives.
	s.eventGuestMu.Lock()
	roomID := s.eventGuests[ev.ID].MeetingGuildID
	s.eventGuestMu.Unlock()
	clear := after
	clear.GuestURL, clear.GuestHost, clear.MemberCode = "", "", ""
	clear.UpdatedAt = after.UpdatedAt + 10
	s.applyEventUpsert(g.ID, s.id.Fingerprint(), clear)
	after, _, _ = s.store.EventByID(ev.ID)
	if after.GuestURL != "" || after.GuestHost != "" || after.MemberCode != "" {
		t.Fatal("the host's own clear did not apply")
	}
	s.mu.RLock()
	_, alive := s.guilds[roomID]
	s.mu.RUnlock()
	if alive {
		t.Fatal("the host's clear left the room standing")
	}
}
