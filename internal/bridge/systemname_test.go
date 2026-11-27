package bridge

import (
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

// A system row is a member's notice. The name it carries is the sender's own
// claim, so the view drops it: the page names the row from the authenticated
// sender (or, for an event reminder, from the guild), and a member cannot post
// a notice that reads as "Concord".
func TestSystemRowsDoNotCarryTheirAssertedName(t *testing.T) {
	v := messageView(domain.Message{ID: "m1", Kind: "system", Name: "Concord", Content: "Your keys changed"})
	if v.SenderName != "" {
		t.Fatalf("system row kept its asserted name %q", v.SenderName)
	}
	v = messageView(domain.Message{ID: "m2", Kind: "guest", Name: "Sam"})
	if v.SenderName != "Sam" {
		t.Fatalf("guest row lost its name: %q", v.SenderName)
	}
	v = messageView(domain.Message{ID: "m3", Name: "Bashir"})
	if v.SenderName != "Bashir" {
		t.Fatalf("plain row lost its name: %q", v.SenderName)
	}
}
