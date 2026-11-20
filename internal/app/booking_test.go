package app

import (
	"context"
	"crypto/ecdh"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// allWeekWindows opens every weekday all day, so the slot computation always
// has something to offer no matter when the test runs.
func allWeekWindows() []bookingWindow {
	var ws []bookingWindow
	for d := 0; d < 7; d++ {
		ws = append(ws, bookingWindow{Weekday: d, StartMin: 0, EndMin: 24 * 60})
	}
	return ws
}

func bookingReq(t *testing.T, s *Service, req bookingRequest) bookingResponse {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var resp bookingResponse
	if err := json.Unmarshal(s.handleBookingRequest(raw), &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	return resp
}

func testEpk(t *testing.T) string {
	t.Helper()
	k, err := guestEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	return b64url.EncodeToString(k.PublicKey().Bytes())
}

// openBooking is the booking page's half of the seal.
func openBooking(esk *ecdh.PrivateKey, resp bookingResponse) (meetingURL, ics string, err error) {
	spk, err := b64url.DecodeString(resp.Spk)
	if err != nil {
		return "", "", err
	}
	pub, err := guestPublicKey(spk)
	if err != nil {
		return "", "", err
	}
	z, err := esk.ECDH(pub)
	if err != nil {
		return "", "", err
	}
	sealed, err := b64url.DecodeString(resp.Sealed)
	if err != nil {
		return "", "", err
	}
	pt, err := bookingOpen(bookingSealKey(z, esk.PublicKey().Bytes(), spk), sealed)
	if err != nil {
		return "", "", err
	}
	var out struct {
		MeetingURL string `json:"meetingURL"`
		ICS        string `json:"ics"`
	}
	if err := json.Unmarshal(pt, &out); err != nil {
		return "", "", err
	}
	return out.MeetingURL, out.ICS, nil
}

// bookSealed books with an ephemeral key, asserts nothing secret came back in
// the clear, and returns the response with the opened fields filled in.
func bookSealed(t *testing.T, s *Service, req bookingRequest) bookingResponse {
	t.Helper()
	esk, err := guestEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	req.Epk = b64url.EncodeToString(esk.PublicKey().Bytes())
	resp := bookingReq(t, s, req)
	if !resp.OK {
		return resp
	}
	if resp.MeetingURL != "" || resp.ICS != "" {
		t.Fatal("the booking answer carried the meeting link in the clear")
	}
	resp.MeetingURL, resp.ICS, err = openBooking(esk, resp)
	if err != nil {
		t.Fatalf("the visitor could not open their own booking: %v", err)
	}
	return resp
}

// TestBookingSealOpensOnlyForVisitor: no key, no booking; the answer opens
// with the visitor's key and not with another, nor after the host's key is
// swapped in transit; the room's link admits two guests.
func TestBookingSealOpensOnlyForVisitor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1")
	boot := testRendezvous(t, ctx)
	host := startServiceOn(t, ctx, t.TempDir(), boot)
	if _, err := host.SetBookingConfig(BookingConfigInput{
		Enabled: true, SlotMinutes: 30, HorizonDays: 7, Windows: allWeekWindows(),
	}); err != nil {
		t.Fatal(err)
	}
	token := host.bookingCfg.Token
	slots := bookingReq(t, host, bookingRequest{Op: "slots", Token: token})
	if len(slots.Slots) < 2 {
		t.Fatalf("not enough slots: %+v", slots)
	}
	noKey := bookingReq(t, host, bookingRequest{Op: "book", Token: token, Slot: slots.Slots[0], Name: "Kim"})
	if noKey.OK || noKey.Err != "Reload the booking page." {
		t.Fatalf("a book with no key got %+v", noKey)
	}
	if n := len(host.BookingSettings().Bookings); n != 0 {
		t.Fatalf("a refused book still took a slot (%d bookings)", n)
	}

	esk, _ := guestEphemeral()
	resp := bookingReq(t, host, bookingRequest{
		Op: "book", Token: token, Slot: slots.Slots[0], Name: "Kim",
		Epk: b64url.EncodeToString(esk.PublicKey().Bytes()),
	})
	if !resp.OK || resp.Spk == "" || resp.Sealed == "" {
		t.Fatalf("sealed booking answer missing: %+v", resp)
	}
	if resp.MeetingURL != "" || resp.ICS != "" {
		t.Fatal("meeting link in the clear")
	}
	url, ics, err := openBooking(esk, resp)
	if err != nil || !strings.Contains(url, "/guest#h=") || !strings.Contains(url, "&k=") || !strings.Contains(ics, "BEGIN:VEVENT") {
		t.Fatalf("visitor's own key: url=%q err=%v", url, err)
	}
	other, _ := guestEphemeral()
	if _, _, err := openBooking(other, resp); err == nil {
		t.Fatal("the booking answer opened under someone else's key")
	}
	swapped := resp
	swapped.Spk = testEpk(t)
	if _, _, err := openBooking(esk, swapped); err == nil {
		t.Fatal("the booking answer opened after the host's key was swapped")
	}
	host.guestMu.Lock()
	tok, ok := host.guestTokens[linkIDOf(t, url)]
	host.guestMu.Unlock()
	if !ok || tok.MaxGuests != maxGuestsPerBookingLink {
		t.Fatalf("booking link admits %d guests (found=%v), want %d", tok.MaxGuests, ok, maxGuestsPerBookingLink)
	}
}

func TestBookingConfigValidation(t *testing.T) {
	base := BookingConfigInput{SlotMinutes: 30, HorizonDays: 14, Windows: allWeekWindows()}
	if _, err := validateBookingInput(base); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	bad := base
	bad.SlotMinutes = 3
	if _, err := validateBookingInput(bad); err == nil {
		t.Fatal("3-minute slots accepted")
	}
	bad = base
	bad.HorizonDays = 90
	if _, err := validateBookingInput(bad); err == nil {
		t.Fatal("90-day horizon accepted (would outlive the longest meeting lifetime)")
	}
	bad = base
	bad.Windows = []bookingWindow{
		{Weekday: 1, StartMin: 9 * 60, EndMin: 12 * 60},
		{Weekday: 1, StartMin: 11 * 60, EndMin: 14 * 60},
	}
	if _, err := validateBookingInput(bad); err == nil {
		t.Fatal("overlapping windows accepted — two visitors could book the same clock time")
	}
	bad = base
	bad.Enabled = true
	bad.Windows = nil
	if _, err := validateBookingInput(bad); err == nil {
		t.Fatal("published a page with no hours at all")
	}
	// Visitor-facing text is sanitized, not refused: a blurb with control
	// characters must not be able to corrupt the page or an ICS line.
	weird := base
	weird.Blurb = "hi\x00there‮!"
	got, err := validateBookingInput(weird)
	if err != nil {
		t.Fatalf("sanitizable blurb refused: %v", err)
	}
	if got.Blurb != "hithere!" {
		t.Fatalf("blurb not sanitized: %q", got.Blurb)
	}
}

// TestBookingEndToEndOnHost drives the whole receive path the gateway relays
// into: token gate, slot offer, booking (meeting + Notes event + record),
// double-booking refusal, knock-flag, cancel. This is the receive side — the
// same bytes a modified client or a stranger's script would send.
func TestBookingEndToEndOnHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Setenv("CONCORD_GUEST_BASE", "http://127.0.0.1:1")

	boot := testRendezvous(t, ctx)
	host := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := host.SetDisplayName("Avicenna"); err != nil {
		t.Fatal(err)
	}

	// Nothing configured: any probe gets the one generic refusal.
	if resp := bookingReq(t, host, bookingRequest{Op: "slots", Token: "guess"}); resp.OK {
		t.Fatal("an unconfigured host answered a probe")
	}

	view, err := host.SetBookingConfig(BookingConfigInput{
		Enabled: true, Blurb: "Half an hour, straight to a demo.",
		SlotMinutes: 30, HorizonDays: 7, Windows: allWeekWindows(),
	})
	if err != nil {
		t.Fatalf("SetBookingConfig: %v", err)
	}
	if view.URL == "" || !strings.Contains(view.URL, "/book/") {
		t.Fatalf("no booking URL minted: %q", view.URL)
	}
	token := host.bookingCfg.Token
	if token == "" || strings.Contains(view.URL, "#h=") == false {
		t.Fatalf("URL missing token or host peer fragment: %q", view.URL)
	}

	// Wrong token: same refusal as unconfigured — a probe learns nothing.
	if resp := bookingReq(t, host, bookingRequest{Op: "slots", Token: token + "x"}); resp.OK {
		t.Fatal("wrong token was answered")
	}

	slots := bookingReq(t, host, bookingRequest{Op: "slots", Token: token})
	if !slots.OK || len(slots.Slots) == 0 {
		t.Fatalf("no slots offered: %+v", slots)
	}
	if slots.Host != "Avicenna" || slots.SlotMinutes != 30 {
		t.Fatalf("published surface wrong: %+v", slots)
	}
	// Nothing too soon to be real, nothing beyond the horizon.
	now := time.Now()
	for _, st := range slots.Slots {
		if time.Unix(st, 0).Before(now.Add(bookingMinLead)) {
			t.Fatalf("slot %d starts inside the lead window", st)
		}
		if time.Unix(st, 0).After(now.Add(8 * 24 * time.Hour)) {
			t.Fatalf("slot %d is past the horizon", st)
		}
	}

	pick := slots.Slots[0]
	booked := bookSealed(t, host, bookingRequest{
		Op: "book", Token: token, Slot: pick,
		Name: "Sam `the*buyer`", Note: "Interested in the on-prem story.\x00",
	})
	if !booked.OK {
		t.Fatalf("booking refused: %+v", booked)
	}
	if booked.MeetingURL == "" || !strings.Contains(booked.MeetingURL, "/guest#h=") {
		t.Fatalf("no guest meeting link: %q", booked.MeetingURL)
	}
	if !strings.Contains(booked.ICS, "BEGIN:VEVENT") || !strings.Contains(booked.ICS, "Demo with Avicenna") {
		t.Fatalf("visitor ICS wrong:\n%s", booked.ICS)
	}

	// The host's calendar: one event in Notes with the visitor's name + note.
	notes, err := host.NotesDM()
	if err != nil {
		t.Fatal(err)
	}
	evs, err := host.Events(notes.ID)
	if err != nil || len(evs) != 1 {
		t.Fatalf("Notes calendar: %v events=%d", err, len(evs))
	}
	if !strings.Contains(evs[0].Title, "Sam") || !strings.Contains(evs[0].Details, "on-prem story") {
		t.Fatalf("event missing visitor name/note: %+v", evs[0])
	}
	if strings.Contains(evs[0].Details, "\x00") {
		t.Fatal("control characters survived into the calendar event")
	}

	// The meeting room exists, is a meeting, and is marked knock-always.
	rec := host.BookingSettings().Bookings[0]
	host.mu.RLock()
	room, ok := host.guilds[rec.MeetingGuildID]
	host.mu.RUnlock()
	if !ok || room.Kind != "meeting" {
		t.Fatalf("no meeting room for the booking: %+v", rec)
	}
	if !host.isBookingMeeting(rec.MeetingGuildID) {
		t.Fatal("booking room would auto-admit instead of knocking")
	}
	// Its lifetime covers the slot, not the legacy 24h default.
	if exp := host.meetingExpiry(rec.MeetingGuildID); exp.Before(time.Unix(rec.EndUnix, 0)) {
		t.Fatalf("meeting expires %v, before the slot ends %v", exp, time.Unix(rec.EndUnix, 0))
	}

	// The taken slot is gone from the public page; booking it again is refused.
	again := bookingReq(t, host, bookingRequest{Op: "slots", Token: token})
	for _, st := range again.Slots {
		if st == pick {
			t.Fatal("booked slot still offered")
		}
	}
	if dup := bookingReq(t, host, bookingRequest{Op: "book", Token: token, Slot: pick, Name: "Eve", Epk: testEpk(t)}); dup.OK {
		t.Fatal("double booking accepted")
	}

	// A booking with no name is not a booking.
	if anon := bookingReq(t, host, bookingRequest{Op: "book", Token: token, Slot: again.Slots[0], Epk: testEpk(t)}); anon.OK {
		t.Fatal("anonymous booking accepted")
	}

	// Cancel: slot opens again, the room and the event are gone, and the
	// visitor's guest token is revoked immediately.
	if err := host.CancelBooking(rec.EventID); err != nil {
		t.Fatalf("CancelBooking: %v", err)
	}
	if evs, _ := host.Events(notes.ID); len(evs) != 0 {
		t.Fatalf("event survived the cancel: %d", len(evs))
	}
	host.mu.RLock()
	_, stillThere := host.guilds[rec.MeetingGuildID]
	host.mu.RUnlock()
	if stillThere {
		t.Fatal("meeting room survived the cancel")
	}
	host.guestMu.Lock()
	for _, tok := range host.guestTokens {
		if tok.GuildID == rec.MeetingGuildID {
			t.Fatal("guest token survived the cancel — the visitor's link would still knock")
		}
	}
	host.guestMu.Unlock()
	reopened := bookingReq(t, host, bookingRequest{Op: "slots", Token: token})
	found := false
	for _, st := range reopened.Slots {
		if st == pick {
			found = true
		}
	}
	if !found {
		t.Fatal("cancelled slot never reopened")
	}

	// Flipping the page off makes the very same token worthless — receive-side,
	// where a modified gateway can't help a visitor.
	if _, err := host.SetBookingConfig(BookingConfigInput{
		Enabled: false, SlotMinutes: 30, HorizonDays: 7, Windows: allWeekWindows(),
	}); err != nil {
		t.Fatal(err)
	}
	if resp := bookingReq(t, host, bookingRequest{Op: "slots", Token: token}); resp.OK {
		t.Fatal("disabled page still answered")
	}
}
