package app

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// serveProfiles has from serve to a sync payload carrying the given roster,
// exactly as a doctored response would arrive.
func serveProfiles(t *testing.T, to, from *Service, guildID string, roster map[string]Profile) {
	t.Helper()
	to.mu.RLock()
	g := to.guilds[guildID].Clone()
	to.mu.RUnlock()
	raw, err := json.Marshal(syncPayload{Guild: domain.Guild{ID: guildID}, Profiles: roster})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ct, err := from.mls.Encrypt(from.ctx, g.GroupID, raw)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !to.applySyncPayload(guildID, g.GroupID, ct, from.Fingerprint()) {
		t.Fatal("the payload was not readable — test setup broken")
	}
}

// TestUntrustedRosterCannotRouteSomeoneElsesMail: an ordinary member serving a
// sync roster could fill in a profile we did not hold yet for a third member,
// MailboxPub and all — and that key decides where the third member's offline
// mail is sealed to. It could also seed profiles for fingerprints that are not
// in the guild at all, and stamp a copy so far in the future that the
// member's own announce, real key included, never replaced it.
func TestUntrustedRosterCannotRouteSomeoneElsesMail(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svcs, guildID, _ := guildOf(t, ctx, 3)
	a, b, c := svcs[0], svcs[1], svcs[2]

	// A has not learned C yet, as if C joined while A was away. Let the join's
	// own announces land first, or one of them re-teaches A in the middle.
	waitUntil(t, 10*time.Second, func() bool { return a.ProfileOf(c.Fingerprint()).Name != "" }, "A never learned C")
	time.Sleep(time.Second)
	a.mu.Lock()
	delete(a.profiles, c.Fingerprint())
	a.mu.Unlock()

	planted := bytes.Repeat([]byte{0xee}, 32)
	serveProfiles(t, a, b, guildID, map[string]Profile{
		c.Fingerprint(): {Name: "C, says B", MailboxPub: planted, UpdatedAt: time.Now().UnixMilli() + (1 << 40)},
		"NOT-A-MEMBER":  {Name: "a stranger"},
	})
	if got := a.ProfileOf("NOT-A-MEMBER").Name; got != "" {
		t.Fatalf("a roster entry for a non-member was adopted: %q", got)
	}
	got := a.ProfileOf(c.Fingerprint())
	if got.Name != "C, says B" {
		t.Fatalf("a first fill for a member was refused outright: %+v", got)
	}
	if len(got.MailboxPub) > 0 {
		t.Fatal("an ordinary member planted a mailbox key for a third member")
	}

	// C's own copy, served by C, replaces it — key included.
	real := bytes.Repeat([]byte{0x11}, 32)
	serveProfiles(t, a, c, guildID, map[string]Profile{
		c.Fingerprint(): {Name: "C", MailboxPub: real, UpdatedAt: time.Now().UnixMilli()},
	})
	got = a.ProfileOf(c.Fingerprint())
	if got.Name != "C" || !bytes.Equal(got.MailboxPub, real) {
		t.Fatalf("the member's own profile did not replace the relayed one: %+v", got)
	}
}
