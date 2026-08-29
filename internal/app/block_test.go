package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// The unread badge is a promise that there is something in the channel to read.
// The count is done in SQL for speed, which means it never sees the block list
// — so a blocked account's messages went on inflating it, and the reader could
// not clear the badge by reading, because the rows behind it are ones the feed
// will never draw. The catch-up card reads these numbers with no decrypt pass
// behind it, so it announced messages that did not exist.
func TestUnreadCountIgnoresBlockedSenders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())

	them, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate their identity: %v", err)
	}
	friend, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate a friend: %v", err)
	}

	const ch = "channel-under-test"
	at := time.Now().Add(-time.Hour)
	write := func(id string, who *identity.Identity) {
		t.Helper()
		at = at.Add(time.Minute)
		if _, err := s.store.SaveMessage(domain.Message{
			ID: id, ChannelID: ch, Sender: who.PublicKey(), Content: "hello", Sent: at,
		}); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	write("m1", friend)
	write("m2", them)
	write("m3", them)
	write("m4", friend)

	since := map[string]int64{ch: 0}
	if got, err := s.UnreadCounts(since); err != nil || got[ch] != 4 {
		t.Fatalf("before blocking: count = %d (err %v), want 4", got[ch], err)
	}

	if err := s.BlockUser(them.Fingerprint()); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	got, err := s.UnreadCounts(since)
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if got[ch] != 2 {
		t.Fatalf("REGRESSION: badge says %d unread, but only 2 of the 4 rows can be rendered", got[ch])
	}

	// Blocking everyone who spoke must leave no badge at all, rather than a
	// count that can never be cleared by reading.
	if err := s.BlockUser(friend.Fingerprint()); err != nil {
		t.Fatalf("BlockUser friend: %v", err)
	}
	if got, _ := s.UnreadCounts(since); got[ch] != 0 {
		t.Fatalf("every sender blocked, yet the badge still claims %d unread", got[ch])
	}

	// And unblocking restores the count — nothing was deleted, so nothing has
	// to come back over the wire.
	if err := s.UnblockUser(them.Fingerprint()); err != nil {
		t.Fatalf("UnblockUser: %v", err)
	}
	if err := s.UnblockUser(friend.Fingerprint()); err != nil {
		t.Fatalf("UnblockUser friend: %v", err)
	}
	if got, _ := s.UnreadCounts(since); got[ch] != 4 {
		t.Fatalf("after unblocking the count is %d, want all 4 back", got[ch])
	}
}

// Hiding a blocked person's messages leaves their DM row sitting in the list,
// sorted by activity — so the one thing they can still do to you is bump an
// empty conversation to the top of it whenever they feel like it. Blocking has
// to close the conversation, in the core, where every shell goes through it.
func TestBlockingClosesTheOneToOneDM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())

	them, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate their identity: %v", err)
	}
	other, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate someone else: %v", err)
	}
	s.recordDMPeer("dm-with-them", them.Fingerprint())
	s.recordDMPeer("dm-with-other", other.Fingerprint())

	s.mu.RLock()
	openAlready := s.hiddenDMs["dm-with-them"] || s.hiddenDMs["dm-with-other"]
	s.mu.RUnlock()
	if openAlready {
		t.Fatal("a fresh DM started out closed")
	}

	if err := s.BlockUser(them.Fingerprint()); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	s.mu.RLock()
	hidTheirs, hidOthers := s.hiddenDMs["dm-with-them"], s.hiddenDMs["dm-with-other"]
	s.mu.RUnlock()
	if !hidTheirs {
		t.Fatal("REGRESSION: blocking left their DM open — they can still bump it to the top of the list")
	}
	if hidOthers {
		t.Fatal("blocking one person closed a conversation with someone else")
	}

	// A hide, not a delete: the conversation is still there to be reopened, so
	// unblocking and one message from them brings its history straight back.
	if !s.unhideDM("dm-with-them") {
		t.Fatal("the closed DM could not be reopened — blocking deleted it instead of hiding it")
	}

	// …but a message ARRIVING from them must not do the reopening. A closed DM
	// normally surfaces on new activity, which would have handed the blocked
	// account a switch to flip whenever it liked.
	if err := s.BlockUser(them.Fingerprint()); err != nil {
		t.Fatalf("re-block: %v", err)
	}
	if !s.dmReopenBlocked("dm-with-them") {
		t.Fatal("REGRESSION: an arriving message would reopen a blocked person's DM")
	}
	if s.dmReopenBlocked("dm-with-other") || s.dmReopenBlocked("") {
		t.Fatal("the reopen guard fired on a conversation with nobody blocked in it")
	}
	if err := s.UnblockUser(them.Fingerprint()); err != nil {
		t.Fatalf("UnblockUser: %v", err)
	}
	if s.dmReopenBlocked("dm-with-them") {
		t.Fatal("unblocking left the DM permanently unable to reopen")
	}
}

// SenderBlocked is what every view asks before drawing a message, so it has to
// answer for the credential shapes that actually turn up in the store: a bare
// account key from a single-device peer, and a device certificate from someone
// who has linked a phone. Blocking a person means blocking the person — if the
// certificate path were missed, blocking someone's laptop would leave their
// phone talking, which is not a partial fix but a broken feature.
func TestBlockingHidesEveryDeviceOfTheAccount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())

	// Somebody else's account, with a phone linked to it.
	them, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate their identity: %v", err)
	}
	phone, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate their phone: %v", err)
	}
	phoneCert := them.IssueDeviceCertFor(phone.PublicKey(), "their phone", time.Now().UnixMilli()).Marshal()

	if s.SenderBlocked(them.PublicKey()) || s.SenderBlocked(phoneCert) {
		t.Fatal("a stranger was hidden before anybody blocked them")
	}

	if err := s.BlockUser(them.Fingerprint()); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	if !s.SenderBlocked(them.PublicKey()) {
		t.Fatal("blocked account's own messages are still visible")
	}
	if !s.SenderBlocked(phoneCert) {
		t.Fatal("blocking the account left its linked device visible — the block is bypassable by linking a phone")
	}

	// An unrelated account is untouched: blocking is not a mute-everyone switch.
	other, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate a third identity: %v", err)
	}
	if s.SenderBlocked(other.PublicKey()) {
		t.Fatal("blocking one account hid a different one")
	}

	// A row with no sender is a system message, not somebody's speech. It
	// belongs to nobody and so can never be blocked away — without this guard
	// an empty credential hashes to a fingerprint like any other and a single
	// unlucky block list entry would silently blank the channel's join and
	// rename notices.
	if s.SenderBlocked(nil) || s.SenderBlocked([]byte{}) {
		t.Fatal("a senderless system message was hidden by the block filter")
	}

	// Unblocking restores them — there is nothing to re-download, because
	// nothing was deleted.
	if err := s.UnblockUser(them.Fingerprint()); err != nil {
		t.Fatalf("UnblockUser: %v", err)
	}
	if s.SenderBlocked(them.PublicKey()) || s.SenderBlocked(phoneCert) {
		t.Fatal("unblocking did not bring the account's messages back")
	}
}

// A forged certificate — one claiming an account that never signed it — must
// not be able to borrow that account's block status. It resolves to itself, so
// naming a blocked account in the accountPub field buys nothing, and naming an
// unblocked one does not launder a blocked sender past the filter.
func TestForgedDeviceCertDoesNotInheritBlockState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	s := startServiceInDir(t, ctx, dir)

	victim, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate victim: %v", err)
	}
	attacker, err := identity.Generate()
	if err != nil {
		t.Fatalf("generate attacker: %v", err)
	}

	// The attacker signs a cert with their OWN key, then overwrites the
	// accountPub field with the victim's. The signature no longer verifies.
	forged := attacker.IssueDeviceCertFor(attacker.PublicKey(), "not really theirs", time.Now().UnixMilli())
	forged.AccountPub = victim.PublicKey()
	cred := forged.Marshal()

	if err := s.BlockUser(attacker.Fingerprint()); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	if s.SenderBlocked(cred) {
		t.Fatal("a forged cert resolved to the account it merely claims")
	}
	if !s.SenderBlocked(attacker.PublicKey()) {
		t.Fatal("blocking the attacker's real key did not take effect")
	}

	// And the block list survives a restart, so a hidden member does not walk
	// back into the feed the next time the app opens.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	again := startServiceInDir(t, ctx, dir)
	if !again.SenderBlocked(attacker.PublicKey()) {
		t.Fatal("the block was forgotten across a restart")
	}
}
