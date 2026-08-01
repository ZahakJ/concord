package app

import (
	"strings"
	"testing"

	"github.com/zahak/concord/internal/domain"
)

// TestEventICSFixture pins the exact bytes of an export: CRLF line endings,
// UTC Z-times, §3.3.11 TEXT escaping, UID derived from the event id, and the
// explicit no-recurrence marker. A byte-level fixture because ICS parsers in
// the wild are unforgiving — "roughly right" output imports as garbage.
func TestEventICSFixture(t *testing.T) {
	ev := domain.Event{
		ID:      "0123456789abcdef0123456789abcdef",
		GuildID: "g1",
		Title:   "Team sync; planning, review",
		Details: "Line one\nLine two with \\ backslash",
		// 2026-01-01T00:00:00Z .. 01:00:00Z, stamped 2025-12-31T00:00:00Z.
		StartUnix: 1767225600,
		EndUnix:   1767229200,
		Location:  "HQ, room 4",
		CreatedBy: "fpr",
		CreatedAt: 1767139200,
		UpdatedAt: 1767139200,
	}
	want := "BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"PRODID:-//Concord//Concord Calendar//EN\r\n" +
		"CALSCALE:GREGORIAN\r\n" +
		"METHOD:PUBLISH\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:0123456789abcdef0123456789abcdef@concord\r\n" +
		"DTSTAMP:20251231T000000Z\r\n" +
		"DTSTART:20260101T000000Z\r\n" +
		"DTEND:20260101T010000Z\r\n" +
		"SUMMARY:Team sync\\; planning\\, review\r\n" +
		"DESCRIPTION:Line one\\nLine two with \\\\ backslash\r\n" +
		"LOCATION:HQ\\, room 4\r\n" +
		"X-CONCORD-RECURRENCE:NONE\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	if got := icsCalendar([]domain.Event{ev}); got != want {
		t.Fatalf("ics bytes drifted:\n got: %q\nwant: %q", got, want)
	}
}

// TestEventICSFoldingAndOmissions: a SUMMARY longer than 75 octets folds with
// CRLF+space at the 75-octet boundary, and an event with no end, details or
// location omits DTEND/DESCRIPTION/LOCATION instead of writing empty values.
func TestEventICSFoldingAndOmissions(t *testing.T) {
	long := strings.Repeat("x", 100)
	ev := domain.Event{
		ID:        "ffffffffffffffffffffffffffffffff",
		GuildID:   "g1",
		Title:     long,
		StartUnix: 1767225600,
		CreatedBy: "fpr",
		CreatedAt: 1767139200,
	}
	got := icsCalendar([]domain.Event{ev})
	// "SUMMARY:" is 8 octets, so 67 x's fit on the first physical line and the
	// remaining 33 continue after the fold, behind the mandated single space.
	wantFold := "SUMMARY:" + strings.Repeat("x", 67) + "\r\n " + strings.Repeat("x", 33) + "\r\n"
	if !strings.Contains(got, wantFold) {
		t.Fatalf("long SUMMARY did not fold at 75 octets:\n%q", got)
	}
	for _, absent := range []string{"DTEND", "DESCRIPTION", "LOCATION"} {
		if strings.Contains(got, absent) {
			t.Fatalf("%s must be omitted when unset, got:\n%q", absent, got)
		}
	}
	// UpdatedAt zero falls back to CreatedAt for DTSTAMP.
	if !strings.Contains(got, "DTSTAMP:20251231T000000Z\r\n") {
		t.Fatalf("DTSTAMP did not fall back to CreatedAt:\n%q", got)
	}
}
