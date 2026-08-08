package app

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZahakJ/concord/internal/domain"
)

// ICS export: RFC 5545 text for guild calendar events. This is the FILE
// FORMAT only — the string is handed to whatever calendar the user already
// runs; no vendor, no network call, nothing leaves the machine.
//
// v1 events do not recur, and the export says so explicitly with
// X-CONCORD-RECURRENCE:NONE instead of leaving importers to wonder whether a
// missing RRULE was intent or a bug. When a weekly toggle lands (v2), that
// line becomes an RRULE and old exports remain unambiguous.

// icsTime renders a Unix timestamp as an RFC 5545 UTC DATE-TIME (§3.3.5
// form #2, the trailing-Z form). Always UTC: a floating local time would
// shift the event for every member in another timezone.
func icsTime(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("20060102T150405Z")
}

// icsEscape escapes a TEXT property value per RFC 5545 §3.3.11: backslash,
// semicolon and comma are backslash-escaped and a newline becomes the two
// characters `\n`. A bare CR is dropped — it is not representable in TEXT,
// and passing it through raw would terminate the content line early.
func icsEscape(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case ';':
			b.WriteString(`\;`)
		case ',':
			b.WriteString(`\,`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// icsFold writes one content line folded at 75 octets (RFC 5545 §3.1: CRLF
// followed by a single space, which counts against the next line's budget).
// Breaks land on rune boundaries — the spec permits any octet split, but a
// UTF-8 sequence cut in half renders as garbage in strict importers.
func icsFold(b *strings.Builder, line string) {
	const limit = 75
	octets := 0
	for _, r := range line {
		n := utf8.RuneLen(r)
		if octets+n > limit {
			b.WriteString("\r\n ")
			octets = 1 // the continuation's leading space
		}
		b.WriteRune(r)
		octets += n
	}
	b.WriteString("\r\n")
}

// icsVEvent writes one VEVENT. UID derives from the event id so re-importing
// a newer export updates the entry instead of duplicating it; DTSTAMP is the
// record's last modification, the closest RFC analogue to "when this copy was
// written". A zero EndUnix omits DTEND, which RFC 5545 defines as a
// point-in-time event — not a day-long block.
func icsVEvent(b *strings.Builder, ev domain.Event) {
	icsFold(b, "BEGIN:VEVENT")
	icsFold(b, "UID:"+ev.ID+"@concord")
	stamp := ev.UpdatedAt
	if stamp <= 0 {
		stamp = ev.CreatedAt
	}
	icsFold(b, "DTSTAMP:"+icsTime(stamp))
	icsFold(b, "DTSTART:"+icsTime(ev.StartUnix))
	if ev.EndUnix > 0 {
		icsFold(b, "DTEND:"+icsTime(ev.EndUnix))
	}
	icsFold(b, "SUMMARY:"+icsEscape(ev.Title))
	// A guest-opened event carries its join link both as the URL property and
	// inside DESCRIPTION: URL is the spec's slot for exactly this, but plenty
	// of calendar UIs only ever show the description, and "Add to calendar"
	// must not strand the link behind a field the app hides.
	desc := ev.Details
	if ev.GuestURL != "" {
		if desc != "" {
			desc += "\n\n"
		}
		desc += "Join from your browser (no install):\n" + ev.GuestURL
	}
	if desc != "" {
		icsFold(b, "DESCRIPTION:"+icsEscape(desc))
	}
	if ev.GuestURL != "" {
		icsFold(b, "URL:"+icsEscape(ev.GuestURL))
	}
	if ev.Location != "" {
		icsFold(b, "LOCATION:"+icsEscape(ev.Location))
	}
	icsFold(b, "X-CONCORD-RECURRENCE:NONE")
	icsFold(b, "END:VEVENT")
}

// icsCalendar renders a complete VCALENDAR wrapping the given events, in the
// order handed in (the store already sorts by start time).
func icsCalendar(events []domain.Event) string {
	var b strings.Builder
	icsFold(&b, "BEGIN:VCALENDAR")
	icsFold(&b, "VERSION:2.0")
	icsFold(&b, "PRODID:-//Concord//Concord Calendar//EN")
	icsFold(&b, "CALSCALE:GREGORIAN")
	icsFold(&b, "METHOD:PUBLISH")
	for _, ev := range events {
		icsVEvent(&b, ev)
	}
	icsFold(&b, "END:VCALENDAR")
	return b.String()
}

// EventICS exports one event as a complete RFC 5545 calendar file.
func (s *Service) EventICS(guildID, eventID string) (string, error) {
	ev, found, err := s.store.EventByID(eventID)
	if err != nil {
		return "", err
	}
	if !found || ev.GuildID != guildID {
		return "", fmt.Errorf("app: unknown event %s", eventID)
	}
	return icsCalendar([]domain.Event{ev}), nil
}

// EventsICS exports a guild's whole calendar, ordered by start time.
func (s *Service) EventsICS(guildID string) (string, error) {
	evs, err := s.store.Events(guildID)
	if err != nil {
		return "", err
	}
	return icsCalendar(evs), nil
}
