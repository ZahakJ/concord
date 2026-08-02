package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/domain"
)

// Public booking — "schedule a demo with me" as one URL. The host defines
// office-hours windows here; the rendezvous serves a public page at
// /book/<token> and relays each visitor request over ONE short libp2p stream
// (/concord/booking/1.0.0) to THIS node, which is the only authority on
// anything: it computes free slots, validates a chosen one, spins up a guest
// MEETING for it (the existing knock-to-enter machinery) and writes the
// calendar event into the host's own Notes self-group — MLS-encrypted, so it
// reaches the host's linked devices without the rendezvous ever seeing it.
// The rendezvous stores NOTHING about availability or bookings; when this
// node is offline the page says so, honestly. No email, no third party — the
// visitor keeps the meeting link and an .ics file, and that is deliberate.

const (
	// bookingConfigKey / bookingRecordsKey persist the host's availability and
	// the taken slots in the same encrypted store every other pref lives in.
	bookingConfigKey  = "booking.config"
	bookingRecordsKey = "booking.records"

	maxBookingBlurbRunes = 280
	maxBookingWindows    = 28 // four a day is plenty of office hours
	minBookingSlotMin    = 5
	maxBookingSlotMin    = 240
	// maxBookingHorizonDays stays under maxMeetingLifetime (31 days): a booked
	// slot's meeting expires shortly after the slot ends, and a horizon longer
	// than the longest meeting lifetime would mint rooms our own devices refuse
	// to keep.
	maxBookingHorizonDays = 30
	// bookingMinLead keeps "starts in 90 seconds" off the page: a visitor who
	// books a slot needs the host to actually see it appear first.
	bookingMinLead = 30 * time.Minute
	// maxBookingSlots bounds the slots response frame — the page shows a
	// couple of weeks of office hours, not an infinite scroll.
	maxBookingSlots = 600
	// maxOutstandingBookings caps live bookings. Every booking is a meeting
	// guild plus a calendar event on this node's disk, and the page is on the
	// open web: without a ceiling, a scripted visitor could mint rooms all day.
	maxOutstandingBookings = 200

	maxBookingNameRunes = 40
	maxBookingNoteRunes = 500

	// Host-side rate limits. The gateway already limits per visitor IP, but a
	// full Concord peer can dial /concord/booking/1.0.0 without the gateway —
	// a limit only the gateway enforces is decorative, so the receive side
	// keeps its own budget: a burst, then a trickle.
	bookingSlotsBurst  = 30
	bookingSlotsRefill = 1.0 // requests/second
	bookingBookBurst   = 10
	bookingBookRefill  = 0.1
)

// bookingWindow is one office-hours block: a weekday (0 = Sunday, matching
// both time.Weekday and JavaScript's getDay) plus start/end minutes from that
// day's local midnight. Minutes-from-midnight rather than clock strings so
// there is exactly one parse, at the settings panel.
type bookingWindow struct {
	Weekday  int `json:"weekday"`
	StartMin int `json:"startMin"`
	EndMin   int `json:"endMin"`
}

// bookingConfig is the host's whole published surface: everything a visitor
// can learn from the page is in here (plus the display name), so "what does
// the internet see" has one answer.
type bookingConfig struct {
	Enabled     bool            `json:"enabled"`
	Blurb       string          `json:"blurb"`
	SlotMinutes int             `json:"slotMinutes"`
	HorizonDays int             `json:"horizonDays"`
	Windows     []bookingWindow `json:"windows"`
	// Token is the bearer secret in the /book/<token> URL, minted like a guest
	// link's. The protocol only answers requests that present it, so an
	// unpublished page reveals nothing — not even that bookings exist.
	Token string `json:"token"`
}

// bookingRecord is one taken slot. EventID keys it (unique per booking);
// MeetingGuildID ties it to the disposable meeting room so cancelling can
// kill the visitor's link.
type bookingRecord struct {
	EventID        string `json:"eventId"`
	MeetingGuildID string `json:"meetingGuildId"`
	SlotUnix       int64  `json:"slotUnix"`
	EndUnix        int64  `json:"endUnix"`
	Name           string `json:"name"`
	Note           string `json:"note"`
	CreatedAt      int64  `json:"createdAt"`
}

// BookingView is what the settings panel reads: the config, the public URL
// (when live) and the upcoming bookings, in one round trip.
type BookingView struct {
	Enabled     bool            `json:"enabled"`
	Blurb       string          `json:"blurb"`
	SlotMinutes int             `json:"slotMinutes"`
	HorizonDays int             `json:"horizonDays"`
	Windows     []bookingWindow `json:"windows"`
	URL         string          `json:"url"`
	Bookings    []bookingRecord `json:"bookings"`
}

// BookingConfigInput is the settable half of the config — the token is never
// client-supplied; it is minted here or not at all.
type BookingConfigInput struct {
	Enabled     bool            `json:"enabled"`
	Blurb       string          `json:"blurb"`
	SlotMinutes int             `json:"slotMinutes"`
	HorizonDays int             `json:"horizonDays"`
	Windows     []bookingWindow `json:"windows"`
}

// initBookings restores state and registers the protocol handler. Called from
// Start, beside initGuests.
func (s *Service) initBookings() {
	s.bookingMu.Lock()
	s.bookingCfg = bookingConfig{SlotMinutes: 30, HorizonDays: 14}
	if raw, err := s.store.GetSetting(bookingConfigKey); err == nil && raw != "" {
		var cfg bookingConfig
		if json.Unmarshal([]byte(raw), &cfg) == nil {
			s.bookingCfg = cfg
		}
	}
	if raw, err := s.store.GetSetting(bookingRecordsKey); err == nil && raw != "" {
		var recs []bookingRecord
		if json.Unmarshal([]byte(raw), &recs) == nil {
			s.bookingRecords = recs
		}
	}
	s.bookingSlotsBucket = tokenBucket{tokens: bookingSlotsBurst, last: time.Now()}
	s.bookingBookBucket = tokenBucket{tokens: bookingBookBurst, last: time.Now()}
	s.pruneBookingsLocked(time.Now())
	s.bookingMu.Unlock()
	s.host.HandleBookings(func(from peer.ID, req []byte) []byte {
		return s.handleBookingRequest(req)
	})
}

// tokenBucket is the small shared shape of the host-side limits.
type tokenBucket struct {
	tokens float64
	last   time.Time
}

func (b *tokenBucket) take(refill, burst float64) bool {
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * refill
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// pruneBookingsLocked drops records whose slot is a day gone: they hold a cap
// slot and clutter the panel, and their meeting room has already expired.
// Caller holds bookingMu.
func (s *Service) pruneBookingsLocked(now time.Time) {
	kept := s.bookingRecords[:0]
	for _, r := range s.bookingRecords {
		if time.Unix(r.EndUnix, 0).After(now.Add(-24 * time.Hour)) {
			kept = append(kept, r)
		}
	}
	s.bookingRecords = kept
}

func (s *Service) saveBookingsLocked() {
	if blob, err := json.Marshal(s.bookingCfg); err == nil {
		_ = s.store.SetSetting(bookingConfigKey, string(blob))
	}
	recs := s.bookingRecords
	if recs == nil {
		recs = []bookingRecord{}
	}
	if blob, err := json.Marshal(recs); err == nil {
		_ = s.store.SetSetting(bookingRecordsKey, string(blob))
	}
}

// sanitizeBookingText bounds visitor-typed text before it can render anywhere
// (the host's calendar, the panel, an ICS line): control characters and
// invisible formatting go, length is capped in RUNES. Newlines survive only
// when multiline — a note may have paragraphs, a name may not.
func sanitizeBookingText(v string, maxRunes int, multiline bool) string {
	v = strings.Map(func(r rune) rune {
		if r == '\n' && multiline {
			return r
		}
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cs, r) {
			return -1
		}
		return r
	}, v)
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > maxRunes {
		runes := []rune(v)
		v = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return v
}

// validateBookingInput normalizes and checks a config the panel submitted.
func validateBookingInput(in BookingConfigInput) (BookingConfigInput, error) {
	in.Blurb = sanitizeBookingText(in.Blurb, maxBookingBlurbRunes, false)
	if in.SlotMinutes < minBookingSlotMin || in.SlotMinutes > maxBookingSlotMin {
		return in, fmt.Errorf("app: slot length must be %d–%d minutes", minBookingSlotMin, maxBookingSlotMin)
	}
	if in.HorizonDays < 1 || in.HorizonDays > maxBookingHorizonDays {
		return in, fmt.Errorf("app: bookings can open 1–%d days ahead", maxBookingHorizonDays)
	}
	if len(in.Windows) > maxBookingWindows {
		return in, fmt.Errorf("app: at most %d office-hours windows", maxBookingWindows)
	}
	for _, w := range in.Windows {
		if w.Weekday < 0 || w.Weekday > 6 {
			return in, fmt.Errorf("app: bad weekday")
		}
		if w.StartMin < 0 || w.EndMin > 24*60 || w.StartMin >= w.EndMin {
			return in, fmt.Errorf("app: a window must start before it ends, within one day")
		}
		if w.EndMin-w.StartMin < in.SlotMinutes {
			return in, fmt.Errorf("app: a window must fit at least one slot")
		}
	}
	// Overlapping windows on one weekday would offer two meetings over the
	// same clock time — refuse at save time, where the fix is obvious.
	sorted := append([]bookingWindow(nil), in.Windows...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Weekday != sorted[j].Weekday {
			return sorted[i].Weekday < sorted[j].Weekday
		}
		return sorted[i].StartMin < sorted[j].StartMin
	})
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Weekday == sorted[i-1].Weekday && sorted[i].StartMin < sorted[i-1].EndMin {
			return in, fmt.Errorf("app: two windows on the same day overlap")
		}
	}
	if in.Enabled && len(in.Windows) == 0 {
		return in, fmt.Errorf("app: add at least one office-hours window before publishing")
	}
	return in, nil
}

// BookingSettings returns the panel's view.
func (s *Service) BookingSettings() BookingView {
	s.bookingMu.Lock()
	defer s.bookingMu.Unlock()
	s.pruneBookingsLocked(time.Now())
	return s.bookingViewLocked()
}

func (s *Service) bookingViewLocked() BookingView {
	v := BookingView{
		Enabled:     s.bookingCfg.Enabled,
		Blurb:       s.bookingCfg.Blurb,
		SlotMinutes: s.bookingCfg.SlotMinutes,
		HorizonDays: s.bookingCfg.HorizonDays,
		Windows:     append([]bookingWindow{}, s.bookingCfg.Windows...),
		Bookings:    append([]bookingRecord{}, s.bookingRecords...),
	}
	sort.Slice(v.Bookings, func(i, j int) bool { return v.Bookings[i].SlotUnix < v.Bookings[j].SlotUnix })
	if s.bookingCfg.Enabled && s.bookingCfg.Token != "" {
		if base := s.guestGatewayBase(); base != "" {
			// Token in the PATH (the rendezvous serves /book/<token>), host peer
			// id in the fragment like a guest link's — the gateway only learns it
			// when the page actually asks it to relay.
			v.URL = fmt.Sprintf("%s/book/%s#h=%s", base, s.bookingCfg.Token, s.host.PeerID())
		}
	}
	return v
}

// SetBookingConfig saves the availability + page toggle. Enabling mints the
// URL token on first use (24 random bytes, same coinage as a guest link);
// re-saving never rotates it — the link the host already put on their website
// must keep working.
func (s *Service) SetBookingConfig(in BookingConfigInput) (BookingView, error) {
	in, err := validateBookingInput(in)
	if err != nil {
		return BookingView{}, err
	}
	if in.Enabled && s.guestGatewayBase() == "" {
		return BookingView{}, fmt.Errorf("app: a booking page needs a rendezvous server (Settings → Connection)")
	}
	s.bookingMu.Lock()
	defer s.bookingMu.Unlock()
	if in.Enabled && s.bookingCfg.Token == "" {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			return BookingView{}, err
		}
		s.bookingCfg.Token = base64.RawURLEncoding.EncodeToString(raw)
	}
	s.bookingCfg.Enabled = in.Enabled
	s.bookingCfg.Blurb = in.Blurb
	s.bookingCfg.SlotMinutes = in.SlotMinutes
	s.bookingCfg.HorizonDays = in.HorizonDays
	s.bookingCfg.Windows = in.Windows
	s.saveBookingsLocked()
	return s.bookingViewLocked(), nil
}

// isBookingMeeting reports whether a guild is a room a booking minted. Such
// rooms always KNOCK: the link went to someone the host has never met, over
// the open web, so arrival must be the host's decision even though ordinary
// meeting links auto-admit while the door is unlocked.
func (s *Service) isBookingMeeting(guildID string) bool {
	s.bookingMu.Lock()
	defer s.bookingMu.Unlock()
	for _, r := range s.bookingRecords {
		if r.MeetingGuildID == guildID {
			return true
		}
	}
	return false
}

// CancelBooking frees a slot: the calendar event goes, the meeting room is
// deleted (which is what makes the visitor's link answer "no longer valid"),
// and the slot shows as available again on the public page.
func (s *Service) CancelBooking(eventID string) error {
	s.bookingMu.Lock()
	var rec bookingRecord
	found := false
	for i, r := range s.bookingRecords {
		if r.EventID == eventID {
			rec = r
			s.bookingRecords = append(s.bookingRecords[:i], s.bookingRecords[i+1:]...)
			found = true
			break
		}
	}
	if found {
		s.saveBookingsLocked()
	}
	s.bookingMu.Unlock()
	if !found {
		return fmt.Errorf("app: unknown booking")
	}
	// Best effort from here: the record is gone (the slot is free again), so a
	// half-cleaned room or event must not resurrect it.
	if notes, err := s.NotesDM(); err == nil {
		_ = s.DeleteEvent(notes.ID, rec.EventID)
	}
	if rec.MeetingGuildID != "" {
		_ = s.deleteGuildLocal(rec.MeetingGuildID)
		s.dropGuestTokens(rec.MeetingGuildID)
	}
	s.emitGuildUpdate()
	return nil
}

// dropGuestTokens revokes any guest link into a guild immediately, instead of
// waiting for the deleted-guild check to catch the next visitor.
func (s *Service) dropGuestTokens(guildID string) {
	s.guestMu.Lock()
	for t, tok := range s.guestTokens {
		if tok.GuildID == guildID {
			delete(s.guestTokens, t)
		}
	}
	s.guestMu.Unlock()
	s.saveGuestTokens()
}

// availableSlotsLocked computes bookable start times: the office-hours
// windows unrolled across the horizon, minus anything already booked, minus
// anything too soon to be real. Times are computed in THIS machine's local
// zone (the host's office hours are wall-clock hours here) and shipped as
// Unix seconds, so the visitor's browser renders them in the visitor's zone.
// Caller holds bookingMu.
func (s *Service) availableSlotsLocked(now time.Time) []int64 {
	cfg := s.bookingCfg
	// Saved configs are validated, but a zero SlotMinutes from a corrupt or
	// hand-edited row would turn `min += SlotMinutes` below into a spin loop —
	// refuse rather than trust the disk.
	if cfg.SlotMinutes < minBookingSlotMin || cfg.HorizonDays < 1 || cfg.HorizonDays > maxBookingHorizonDays {
		return nil
	}
	slotDur := time.Duration(cfg.SlotMinutes) * time.Minute
	lead := now.Add(bookingMinLead)
	horizon := now.Add(time.Duration(cfg.HorizonDays) * 24 * time.Hour)
	var out []int64
	loc := time.Local
	y, m, d := now.In(loc).Date()
	for day := 0; day <= cfg.HorizonDays; day++ {
		// time.Date normalizes day overflow, which also carries DST correctly:
		// each day's windows anchor to that day's real local midnight.
		midnight := time.Date(y, m, d+day, 0, 0, 0, 0, loc)
		wd := int(midnight.Weekday())
		for _, w := range cfg.Windows {
			if w.Weekday != wd {
				continue
			}
			for min := w.StartMin; min+cfg.SlotMinutes <= w.EndMin; min += cfg.SlotMinutes {
				st := midnight.Add(time.Duration(min) * time.Minute)
				if !st.After(lead) || st.After(horizon) {
					continue
				}
				if s.slotTakenLocked(st.Unix(), st.Add(slotDur).Unix()) {
					continue
				}
				out = append(out, st.Unix())
				if len(out) >= maxBookingSlots {
					sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
					return out
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// slotTakenLocked reports interval overlap with any booking — not just an
// equal start, because a slot-length change after a booking shifts the grid
// and two half-overlapping meetings is exactly the bug that would allow.
func (s *Service) slotTakenLocked(startUnix, endUnix int64) bool {
	for _, r := range s.bookingRecords {
		if startUnix < r.EndUnix && r.SlotUnix < endUnix {
			return true
		}
	}
	return false
}

// ---- the wire ----

// bookingRequest is a visitor's single line, relayed verbatim by the gateway.
type bookingRequest struct {
	Op    string `json:"op"` // "slots" | "book"
	Token string `json:"token"`
	Slot  int64  `json:"slot,omitempty"`
	Name  string `json:"name,omitempty"`
	Note  string `json:"note,omitempty"`
}

// bookingResponse is the single line back. Exactly one of the shapes is
// filled; Err is a sentence safe to show a stranger.
type bookingResponse struct {
	OK          bool    `json:"ok"`
	Err         string  `json:"error,omitempty"`
	Host        string  `json:"host,omitempty"`
	Blurb       string  `json:"blurb,omitempty"`
	SlotMinutes int     `json:"slotMinutes,omitempty"`
	Slots       []int64 `json:"slots,omitempty"`
	MeetingURL  string  `json:"meetingURL,omitempty"`
	ICS         string  `json:"ics,omitempty"`
	Start       int64   `json:"start,omitempty"`
	End         int64   `json:"end,omitempty"`
}

func bookingErr(msg string) []byte {
	b, _ := json.Marshal(bookingResponse{OK: false, Err: msg})
	return b
}

// handleBookingRequest answers one relayed visitor request. Everything here
// is receive-side law: the token gate, the rate budget, the re-validation of
// the chosen slot — the gateway's checks are convenience, not security,
// because any peer can open this stream without the gateway.
func (s *Service) handleBookingRequest(req []byte) []byte {
	var r bookingRequest
	if json.Unmarshal(req, &r) != nil {
		return bookingErr("Bad request.")
	}
	s.bookingMu.Lock()
	cfg := s.bookingCfg
	okBudget := true
	switch r.Op {
	case "slots":
		okBudget = s.bookingSlotsBucket.take(bookingSlotsRefill, bookingSlotsBurst)
	case "book":
		okBudget = s.bookingBookBucket.take(bookingBookRefill, bookingBookBurst)
	}
	s.bookingMu.Unlock()
	if !okBudget {
		return bookingErr("Too many requests right now — try again in a minute.")
	}
	// One generic refusal for "disabled", "no such token" and "not configured":
	// a probe must not learn which of them is true.
	if !cfg.Enabled || cfg.Token == "" || len(r.Token) != len(cfg.Token) ||
		subtle.ConstantTimeCompare([]byte(r.Token), []byte(cfg.Token)) != 1 {
		return bookingErr("This booking page is no longer available.")
	}
	switch r.Op {
	case "slots":
		s.bookingMu.Lock()
		s.pruneBookingsLocked(time.Now())
		slots := s.availableSlotsLocked(time.Now())
		s.bookingMu.Unlock()
		resp := bookingResponse{
			OK:          true,
			Host:        s.DisplayName(),
			Blurb:       cfg.Blurb,
			SlotMinutes: cfg.SlotMinutes,
			Slots:       slots,
		}
		b, _ := json.Marshal(resp)
		return b
	case "book":
		resp := s.bookSlot(r)
		b, _ := json.Marshal(resp)
		return b
	}
	return bookingErr("Bad request.")
}

// bookSlot takes one slot for one visitor: re-validate it against the live
// availability, mint the meeting room + knock-only guest link, put the event
// on the host's own calendar (the Notes self-group — see the comment on the
// Notes choice below), record the slot as taken, and hand the visitor their
// link and .ics. The visitor keeps those; there is nothing to email.
//
// The event lives in Notes rather than a new "personal calendar" store
// because Notes IS the personal/self view Concord already has: a one-member
// MLS group whose guild-meta lane (which calendar events already ride)
// reaches exactly the host's own linked devices, encrypted, with history sync
// for a device that was off. A separate store would need all of that rebuilt.
func (s *Service) bookSlot(r bookingRequest) bookingResponse {
	name := sanitizeBookingText(r.Name, maxBookingNameRunes, false)
	note := sanitizeBookingText(r.Note, maxBookingNoteRunes, true)
	if name == "" {
		return bookingResponse{Err: "Please tell the host who you are."}
	}
	if r.Slot <= 0 {
		return bookingResponse{Err: "Pick a time first."}
	}

	s.bookingMu.Lock()
	now := time.Now()
	s.pruneBookingsLocked(now)
	live := 0
	for _, rec := range s.bookingRecords {
		if rec.EndUnix > now.Unix() {
			live++
		}
	}
	if live >= maxOutstandingBookings {
		s.bookingMu.Unlock()
		return bookingResponse{Err: "The host's calendar is full right now."}
	}
	// Membership in the freshly computed set is the whole check: in a window,
	// inside the horizon, not too soon, not overlapping anything booked.
	valid := false
	for _, st := range s.availableSlotsLocked(now) {
		if st == r.Slot {
			valid = true
			break
		}
	}
	if !valid {
		s.bookingMu.Unlock()
		return bookingResponse{Err: "That time was just taken — pick another."}
	}
	slotMin := s.bookingCfg.SlotMinutes
	s.bookingMu.Unlock()

	start := time.Unix(r.Slot, 0)
	end := start.Add(time.Duration(slotMin) * time.Minute)

	// The meeting room: same disposable machinery as StartMeeting, named for
	// the calendar, alive until an hour past the slot so a meeting that runs
	// long is not cut off mid-sentence.
	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return bookingResponse{Err: "The host's app couldn't create the meeting — try again."}
	}
	g := domain.NewMeetingGuild("📅 "+name+" — "+start.Format("Jan 2, 15:04"), gid, s.PublicKey())
	if err := s.store.SaveGuild(g); err != nil {
		return bookingResponse{Err: "The host's app couldn't create the meeting — try again."}
	}
	s.trackGuild(&g)
	s.setMeetingExpiry(g.ID, end.Add(time.Hour))

	fail := func() bookingResponse {
		_ = s.deleteGuildLocal(g.ID)
		s.dropGuestTokens(g.ID)
		return bookingResponse{Err: "The host's app couldn't finish the booking — try again."}
	}

	// Lifetime 0: the guest token inherits the absolute expiry set above.
	meetingURL, err := s.CreateGuestLink(g.ID, 0)
	if err != nil {
		return fail()
	}

	notes, err := s.NotesDM()
	if err != nil {
		return fail()
	}
	details := "Booked via your public booking page."
	if note != "" {
		details += "\nNote from " + name + ":\n" + note
	}
	details += "\nMeeting link (they have it too):\n" + meetingURL
	ev, err := s.CreateEvent(notes.ID, "📅 "+name+" — booked demo", details, start.Unix(), end.Unix(), "Concord meeting", "")
	if err != nil {
		return fail()
	}

	rec := bookingRecord{
		EventID:        ev.ID,
		MeetingGuildID: g.ID,
		SlotUnix:       start.Unix(),
		EndUnix:        end.Unix(),
		Name:           name,
		Note:           note,
		CreatedAt:      now.Unix(),
	}
	s.bookingMu.Lock()
	// A second booking may have raced ours between the check and here; the
	// overlap test under the same lock is what makes double-booking impossible
	// rather than merely unlikely.
	if s.slotTakenLocked(rec.SlotUnix, rec.EndUnix) {
		s.bookingMu.Unlock()
		_ = s.DeleteEvent(notes.ID, ev.ID)
		return fail()
	}
	s.bookingRecords = append(s.bookingRecords, rec)
	s.saveBookingsLocked()
	s.bookingMu.Unlock()
	s.emitGuildUpdate()

	// The visitor's .ics is written from THEIR side of the table: same UID as
	// the host's event (the two calendars never meet, and if they ever do,
	// matching UIDs merge instead of duplicating), their note, the join link.
	visitorEv := domain.Event{
		ID:        ev.ID,
		Title:     "Demo with " + s.DisplayName(),
		Details:   "Join from your browser (no install):\n" + meetingURL,
		StartUnix: start.Unix(),
		EndUnix:   end.Unix(),
		Location:  "Concord meeting",
		CreatedAt: now.Unix(),
		UpdatedAt: now.Unix(),
	}
	return bookingResponse{
		OK:         true,
		Host:       s.DisplayName(),
		MeetingURL: meetingURL,
		ICS:        icsCalendar([]domain.Event{visitorEv}),
		Start:      start.Unix(),
		End:        end.Unix(),
	}
}
