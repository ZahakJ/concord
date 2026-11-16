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
	// A commit still landing (the guild was just formed) can leave the two a
	// beat apart in epoch, and then the payload is simply unreadable. That is
	// setup, not the guard, so wait for the epochs to agree and try again; a
	// payload that stays unreadable is still a broken setup and fails.
	for try := 0; ; try++ {
		waitUntil(t, 10*time.Second, func() bool {
			a, errA := to.mls.Epoch(to.ctx, g.GroupID)
			b, errB := from.mls.Epoch(from.ctx, g.GroupID)
			return errA == nil && errB == nil && a == b
		}, "the two never reached the same epoch")
		ct, err := from.mls.Encrypt(from.ctx, g.GroupID, raw)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if to.applySyncPayload(guildID, g.GroupID, ct, from.Fingerprint()) {
			return
		}
		if try >= 4 {
			t.Fatal("the payload was not readable — test setup broken")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// maxRosterAttempts bounds the reruns when C's own announce wins the race.
const maxRosterAttempts = 5

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

	// A has not learned C yet, as if C joined while A was away.
	waitUntil(t, 10*time.Second, func() bool { return a.ProfileOf(c.Fingerprint()).Name != "" }, "A never learned C")
	genuine := a.ProfileOf(c.Fingerprint()).MailboxPub

	// Other writers keep running while the test works: C announces on its own
	// schedule (its own copy, real key), and A's background syncs take honest
	// rosters from B and C, which the guard itself fills in name-only (no key,
	// no stamp). Either landing between forgetting C and the doctored roster
	// re-teaches A some copy of C, which rightly makes the roster's copy no
	// first fill at all. That race is the network being honest, not the guard
	// failing, so such an attempt is run again. Every attempt still holds the
	// guard to all of it: no planted key, no doctored stamp, no stranger, ever.
	planted := bytes.Repeat([]byte{0xee}, 32)
	doctoredAt := time.Now().UnixMilli() + (1 << 40)
	var got Profile
	for attempt := 0; ; attempt++ {
		a.mu.Lock()
		delete(a.profiles, c.Fingerprint())
		a.mu.Unlock()
		serveProfiles(t, a, b, guildID, map[string]Profile{
			c.Fingerprint(): {Name: "C, says B", MailboxPub: planted, UpdatedAt: doctoredAt},
			"NOT-A-MEMBER":  {Name: "a stranger"},
		})
		if got := a.ProfileOf("NOT-A-MEMBER").Name; got != "" {
			t.Fatalf("a roster entry for a non-member was adopted: %q", got)
		}
		var held bool
		a.mu.RLock()
		got, held = a.profiles[c.Fingerprint()]
		a.mu.RUnlock()
		if bytes.Equal(got.MailboxPub, planted) {
			t.Fatal("an ordinary member planted a mailbox key for a third member")
		}
		if got.UpdatedAt >= doctoredAt {
			t.Fatalf("an ordinary member's far-future stamp was kept for a third member: %+v", got)
		}
		if got.Name == "C, says B" {
			break
		}
		// Nobody re-taught A in between, and the doctored roster still left C
		// unheld: the first fill itself was refused.
		if !held {
			t.Fatalf("a first fill for a member was refused outright: %+v", got)
		}
		// Some other copy won the race. It may carry C's real key or no key;
		// any other key came from somewhere it must not have.
		if len(got.MailboxPub) > 0 && !bytes.Equal(got.MailboxPub, genuine) {
			t.Fatalf("A holds a mailbox key for C that is neither C's nor absent: %+v", got)
		}
		// Capped: a guard that another copy beats every time is a guard this
		// test never exercised, and that must fail, not pass quietly.
		if attempt >= maxRosterAttempts-1 {
			t.Fatalf("another copy of C won all %d attempts; the first-fill guard went untested", maxRosterAttempts)
		}
		time.Sleep(50 * time.Millisecond) // another copy of C got there first
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
