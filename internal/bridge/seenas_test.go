package bridge

import (
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
)

// An invisible member is served exactly like a disconnected one; the word
// itself never reaches the interface.
func TestInvisibleIsShownAsOffline(t *testing.T) {
	p := appsvc.Profile{Name: "Bea", Presence: "invisible", Status: "reading"}
	seen, online := seenAs(p, true, false)
	if online || seen.Presence != "" {
		t.Fatalf("invisible served as online=%v presence=%q", online, seen.Presence)
	}
	off, offOnline := seenAs(appsvc.Profile{Name: "Dov", Status: "reading"}, false, false)
	if offOnline != online || off.Presence != seen.Presence || off.Status != seen.Status {
		t.Fatal("an invisible member must look exactly like an offline one")
	}
	if _, on := seenAs(p, true, true); !on {
		t.Fatal("your own row keeps the truth")
	}
	if s, on := seenAs(appsvc.Profile{Presence: "dnd"}, true, false); !on || s.Presence != "dnd" {
		t.Fatal("other presences pass through")
	}
}
