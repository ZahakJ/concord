package app

import (
	"context"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/identity"
)

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
