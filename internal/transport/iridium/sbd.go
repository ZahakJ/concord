package iridium

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// MOLimit is the 9602/9603 mobile-originated buffer.
	MOLimit = 340
	// MTLimit is the mobile-terminated buffer: what one SBDRB can return.
	MTLimit = 270
)

// SBDIX is the result of one SBD session:
// +SBDIX: <MO status>, <MOMSN>, <MT status>, <MTMSN>, <MT length>, <MT queued>
type SBDIX struct {
	MOStatus int
	MOMSN    int
	MTStatus int // 0 no mail, 1 message received, 2 error checking mailbox
	MTMSN    int
	MTLen    int
	MTQueued int
}

// ParseSBDIX parses a +SBDIX line.
func ParseSBDIX(s string) (SBDIX, error) {
	v, ok := strings.CutPrefix(strings.TrimSpace(s), "+SBDIX:")
	if !ok {
		return SBDIX{}, fmt.Errorf("iridium: not an SBDIX line: %q", s)
	}
	parts := strings.Split(v, ",")
	if len(parts) != 6 {
		return SBDIX{}, fmt.Errorf("iridium: SBDIX has %d fields: %q", len(parts), s)
	}
	var n [6]int
	for i, p := range parts {
		x, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || x < 0 || x > 65535 {
			return SBDIX{}, fmt.Errorf("iridium: bad SBDIX field %q", p)
		}
		n[i] = x
	}
	r := SBDIX{n[0], n[1], n[2], n[3], n[4], n[5]}
	if r.MTLen > MTLimit {
		return SBDIX{}, fmt.Errorf("iridium: SBDIX MT length %d", r.MTLen)
	}
	return r, nil
}

// MOOK reports whether the MO message reached the gateway. Statuses 0–4 are
// success (1: MT too big to fetch, 2: location update rejected, 3–4 reserved).
func (r SBDIX) MOOK() bool { return r.MOStatus <= 4 }

// MOReason explains a failed MO status, for the settings page.
func (r SBDIX) MOReason() string {
	switch r.MOStatus {
	case 10:
		return "gateway timed out"
	case 11:
		return "MO queue at gateway full"
	case 13:
		return "session interrupted"
	case 15:
		return "access denied (line rental lapsed?)"
	case 16:
		return "modem locked by the gateway"
	case 17:
		return "gateway not responding"
	case 18:
		return "radio link dropped"
	case 32:
		return "no satellite in view"
	case 33:
		return "antenna fault"
	case 34:
		return "radio disabled"
	case 35:
		return "modem busy"
	case 36, 38:
		return "network asked to try later"
	case 37:
		return "SBD service temporarily disabled"
	}
	return fmt.Sprintf("SBD session failed (status %d)", r.MOStatus)
}

// Credits is what Rock7 bills for a message of n bytes: one credit per 50
// bytes or part thereof (docs.groundcontrol.com, "Iridium contract costs").
func Credits(n int) int {
	if n <= 0 {
		return 0
	}
	return (n + 49) / 50
}
