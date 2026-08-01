package app

import (
	"context"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// These tests pin "typing from a linked device shows an encoded name".
//
// The typing topic carries no payload — attribution is entirely
// s.presence(from), which maps a DEVICE PeerID to its account via
// s.deviceAccounts and otherwise falls back to the fingerprint of the raw
// device key, a fingerprint belonging to no member. The bridge resolves the
// emitted fingerprint to a display name via ProfileName, and anything that
// resolves to nothing renders in the composer as a truncated raw key
// ("TQVB GV2Y is typing…"). receiveTyping is the fix: attribute to a member
// account (re-reading the roster once on a miss), drop what cannot be
// attributed, and suppress one's own devices.

// typingCapture wires an OnTyping listener and returns getters for the last
// fingerprint seen on the wanted channel and the number of events.
func typingCapture(svc *Service, channelID string) (last func() (string, bool), count func() int) {
	var mu sync.Mutex
	var got string
	n := 0
	svc.OnTyping(func(from, ch string) {
		if ch != channelID {
			return
		}
		mu.Lock()
		got = from
		n++
		mu.Unlock()
	})
	last = func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		return got, n > 0
	}
	count = func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
	return last, count
}

// pumpTyping publishes typing hints until stop() reports true or the deadline
// passes; typing is fire-and-forget gossip and the mesh may still be forming.
func pumpTyping(t *testing.T, from *Service, channelID string, stop func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := from.SendTyping(channelID); err != nil {
			t.Fatalf("SendTyping: %v", err)
		}
		if stop() || time.Now().After(deadline) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestTypingFromOwnPhoneIsSuppressedButNamesTheAccountToOthers: you type on
// your phone. Your desktop must NOT show "you are typing" (it is your own
// action, and before this fix it rendered as an encoded stranger, because
// ProfileName holds no entry for one's own fingerprint). A friend in the same
// guild, meanwhile, must see it attributed to your ACCOUNT — not to the
// phone's device key.
func TestTypingFromOwnPhoneIsSuppressedButNamesTheAccountToOthers(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)
	desk, phone, textCh, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	guildID := desk.Guilds()[0].ID

	// The friend joins the shared guild, so the same typing gossip reaches a
	// second account. The friend seeing the event proves delivery worked — which
	// is what makes the desktop's silence mean "suppressed", not "lost".
	friend := startServiceOn(t, ctx, t.TempDir(), boot)
	code, err := desk.InviteCode(guildID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatalf("friend JoinViaInvite: %v", err)
	}
	waitUntil(t, 60*time.Second, func() bool {
		n, _ := friend.MemberCount(guildID)
		return n == 3
	}, "the friend never converged on the 3-leaf roster")

	deskLast, deskCount := typingCapture(desk, textCh)
	_ = deskLast
	friendLast, _ := typingCapture(friend, textCh)

	pumpTyping(t, phone, textCh, func() bool { _, ok := friendLast(); return ok })
	got, ok := friendLast()
	if !ok {
		t.Fatal("the friend never saw the phone typing at all")
	}
	if got != desk.Fingerprint() {
		t.Fatalf("friend saw typing attributed to %q; want the account %q — the phone's device key leaked through", got, desk.Fingerprint())
	}

	// The same signals demonstrably reached this mesh; the desktop must have
	// surfaced none of them.
	time.Sleep(2 * time.Second) // grace for anything in flight
	if n := deskCount(); n != 0 {
		f, _ := deskLast()
		t.Fatalf("the desktop surfaced %d typing event(s) for its own account's phone (as %q); your own typing is not news", n, f)
	}
}

// TestTypingFromFriendsPhoneNamesTheFriend: a friend types on THEIR phone in a
// shared guild; we must see the friend's account, not their phone's raw key.
func TestTypingFromFriendsPhoneNamesTheFriend(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boot := testRendezvous(t, ctx)

	// The friend: a desktop with a linked phone (their own guild comes along;
	// we ignore it).
	fDesk, fPhone, _, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)
	if err := fDesk.SetDisplayName("Friend"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}

	// Us: a separate account with a guild both friend devices join.
	me := startServiceOn(t, ctx, t.TempDir(), boot)
	if err := me.SetDisplayName("Me"); err != nil {
		t.Fatalf("SetDisplayName: %v", err)
	}
	g, err := me.CreateGuild("Party")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	textCh := g.Channels[0].ID
	code, err := me.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := fDesk.JoinViaInvite(code); err != nil {
		t.Fatalf("friend desktop JoinViaInvite: %v", err)
	}
	// The friend's phone follows via the own-device guild handover.
	waitUntil(t, 60*time.Second, func() bool {
		for _, fg := range fPhone.Guilds() {
			if fg.ID == g.ID {
				return true
			}
		}
		return false
	}, "the friend's phone never received the shared guild")
	waitUntil(t, 60*time.Second, func() bool {
		n, _ := me.MemberCount(g.ID)
		return n == 3
	}, "our view never reached 3 leaves (us, friend desktop, friend phone)")

	last, _ := typingCapture(me, textCh)
	pumpTyping(t, fPhone, textCh, func() bool { _, ok := last(); return ok })
	got, ok := last()
	if !ok {
		t.Fatal("we never saw the friend's phone typing at all")
	}
	if got != fDesk.Fingerprint() {
		t.Fatalf("typing attributed to %q; want the friend account %q — their phone's device key leaked through", got, fDesk.Fingerprint())
	}

	// The failure mode the fallback used to hide: the device→account map has
	// not been populated (the map is memory-only, and the phone's add-commit
	// races its first keystroke). receiveTyping must recover the mapping from
	// the roster in hand rather than surface the device key's fingerprint.
	me.deviceMu.Lock()
	me.deviceAccounts = map[string]string{}
	me.deviceMu.Unlock()
	last2, _ := typingCapture(me, textCh)
	me.receiveTyping(g.ID, g.GroupID, textCh, fPhone.host.AddrInfo().ID)
	got2, ok2 := last2()
	if !ok2 {
		t.Fatal("typing from a member device was dropped even though its cert is in the roster")
	}
	if got2 != fDesk.Fingerprint() {
		t.Fatalf("with the device map cold, typing attributed to %q; want %q via the roster", got2, fDesk.Fingerprint())
	}

	// And a signal that cannot be attributed at all — the typing topic is
	// plaintext gossip, so any key can publish to it — must surface NOTHING,
	// not an encoded stranger.
	strangerKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate stranger key: %v", err)
	}
	strangerID, err := peer.IDFromPrivateKey(strangerKey)
	if err != nil {
		t.Fatalf("stranger peer id: %v", err)
	}
	_, count3 := typingCapture(me, textCh)
	me.receiveTyping(g.ID, g.GroupID, textCh, strangerID)
	if n := count3(); n != 0 {
		t.Fatalf("an outsider's typing signal was surfaced %d time(s); it belongs to no member and must be dropped", n)
	}
}
